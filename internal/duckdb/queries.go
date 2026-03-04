package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// ErrTooManyConcurrentQueries is returned when the query concurrency gate is full.
var ErrTooManyConcurrentQueries = errors.New("too many concurrent queries")

// dangerousKeywordPattern matches dangerous SQL keywords at word boundaries.
// This avoids false positives like "RESET" matching "SET".
// Used as defense-in-depth after comment stripping and semicolon rejection.
var dangerousKeywordPattern = regexp.MustCompile(
	`(?i)\b(INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|TRUNCATE|COPY|ATTACH|LOAD|EXPORT|IMPORT|INSTALL|CALL|EXECUTE|PRAGMA|SET)\b`,
)

// blockCommentPattern matches C-style block comments (/* ... */).
var blockCommentPattern = regexp.MustCompile(`/\*[\s\S]*?\*/`)

// stripSQLComments removes -- line comments and /* */ block comments from a query.
func stripSQLComments(query string) string {
	// Remove block comments first.
	cleaned := blockCommentPattern.ReplaceAllString(query, " ")
	// Remove line comments (-- to end of line).
	var result strings.Builder
	for _, line := range strings.Split(cleaned, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

// queryCtx returns a context with the store's configured query timeout.
func (s *Store) queryCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), s.QueryTimeout)

	// Fast-fail when read concurrency is saturated.
	// This avoids piling up waiting readers that could delay writes under load.
	if s.querySlots == nil {
		return ctx, cancel
	}
	select {
	case s.querySlots <- struct{}{}:
		return ctx, func() {
			<-s.querySlots
			cancel()
		}
	default:
		cancel()
		deniedCtx, deniedCancel := context.WithCancel(context.Background())
		deniedCancel()
		return deniedCtx, func() {}
	}
}

// appFilter returns a WHERE clause and args when opts.App is non-empty.
func appFilter(opts QueryOpts) (clause string, args []interface{}) {
	if opts.App != "" {
		return "WHERE app = ?", []interface{}{opts.App}
	}
	return "", nil
}

// appAnd returns an "AND app = ?" fragment and args when opts.App is non-empty.
// Use this when there is already a WHERE clause.
func appAnd(opts QueryOpts) (clause string, args []interface{}) {
	if opts.App != "" {
		return " AND app = ?", []interface{}{opts.App}
	}
	return "", nil
}

// TopWords returns the most frequent words.
func (s *Store) TopWords(limit int, opts QueryOpts) ([]WordCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		WITH words AS (
			SELECT regexp_replace(
				unnest(string_split(lower(message), ' ')),
				'^[^a-z0-9_]+|[^a-z0-9_]+$',
				''
			) AS word
			FROM logs %s
		)
		SELECT word, COUNT(*) as count
		FROM words
		WHERE word != '' AND length(word) >= 3 AND length(word) <= 50
		GROUP BY word
		ORDER BY count DESC, word ASC
		LIMIT ?`, where)

	args := append(wArgs, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []WordCount
	for rows.Next() {
		var wc WordCount
		if err := rows.Scan(&wc.Word, &wc.Count); err != nil {
			log.Printf("duckdb scan error (TopWords): %v", err)
			continue
		}
		results = append(results, wc)
	}
	return results, rows.Err()
}

// TopAttributes returns the most frequent attribute key-value pairs.
func (s *Store) TopAttributes(limit int, opts QueryOpts) ([]AttributeStat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		WITH attrs AS (
			SELECT
				unnest(map_keys(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_key,
				unnest(map_values(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_value
			FROM logs %s
		)
		SELECT attr_key, attr_value, COUNT(*) AS count
		FROM attrs
		WHERE attr_key IS NOT NULL AND attr_value IS NOT NULL
		GROUP BY attr_key, attr_value
		ORDER BY count DESC, attr_key ASC, attr_value ASC
		LIMIT ?`, where)

	args := append(wArgs, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AttributeStat
	for rows.Next() {
		var as AttributeStat
		if err := rows.Scan(&as.Key, &as.Value, &as.Count); err != nil {
			log.Printf("duckdb scan error (TopAttributes): %v", err)
			continue
		}
		results = append(results, as)
	}
	return results, rows.Err()
}

// TopAttributeKeys returns attribute keys sorted by number of unique values.
func (s *Store) TopAttributeKeys(limit int, opts QueryOpts) ([]AttributeKeyStat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		WITH attrs AS (
			SELECT
				unnest(map_keys(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_key,
				unnest(map_values(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_value
			FROM logs %s
		)
		SELECT attr_key, COUNT(DISTINCT attr_value) AS unique_values, COUNT(*) AS total_count
		FROM attrs
		WHERE attr_key IS NOT NULL
		GROUP BY attr_key
		ORDER BY unique_values DESC
		LIMIT ?`, where)

	args := append(wArgs, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AttributeKeyStat
	for rows.Next() {
		var aks AttributeKeyStat
		if err := rows.Scan(&aks.Key, &aks.UniqueValues, &aks.TotalCount); err != nil {
			log.Printf("duckdb scan error (TopAttributeKeys): %v", err)
			continue
		}
		results = append(results, aks)
	}
	return results, rows.Err()
}

// AttributeKeyValues returns value counts for a specific attribute key.
func (s *Store) AttributeKeyValues(key string, limit int) (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		WITH attrs AS (
			SELECT
				unnest(map_keys(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_key,
				unnest(map_values(CAST(attributes AS MAP(VARCHAR, VARCHAR)))) AS attr_value
			FROM logs
		)
		SELECT attr_value, COUNT(*) AS count
		FROM attrs
		WHERE attr_key = ?
		GROUP BY attr_value
		ORDER BY count DESC, attr_value ASC
		LIMIT ?`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var value string
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			log.Printf("duckdb scan error (AttributeKeyValues): %v", err)
			continue
		}
		result[value] = count
	}
	return result, rows.Err()
}

// SeverityCounts returns the total count per severity level.
func (s *Store) SeverityCounts(opts QueryOpts) (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`SELECT level, COUNT(*) FROM logs %s GROUP BY level`, where)

	rows, err := s.db.QueryContext(ctx, query, wArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var level string
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			log.Printf("duckdb scan error (SeverityCounts): %v", err)
			continue
		}
		result[level] = count
	}
	return result, rows.Err()
}

// SeverityCountsByMinute returns per-minute severity breakdowns for all logs.
func (s *Store) SeverityCountsByMinute(opts QueryOpts) ([]MinuteCounts, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		SELECT date_trunc('minute', timestamp) as minute,
			SUM(CASE WHEN level='TRACE' THEN 1 ELSE 0 END) as trace,
			SUM(CASE WHEN level='DEBUG' THEN 1 ELSE 0 END) as debug,
			SUM(CASE WHEN level='INFO' THEN 1 ELSE 0 END) as info,
			SUM(CASE WHEN level='WARN' THEN 1 ELSE 0 END) as warn,
			SUM(CASE WHEN level='ERROR' THEN 1 ELSE 0 END) as error,
			SUM(CASE WHEN level='FATAL' THEN 1 ELSE 0 END) as fatal,
			COUNT(*) as total
		FROM logs %s
		GROUP BY minute ORDER BY minute`, where)

	rows, err := s.db.QueryContext(ctx, query, wArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MinuteCounts
	for rows.Next() {
		var mc MinuteCounts
		if err := rows.Scan(&mc.Minute, &mc.Trace, &mc.Debug, &mc.Info, &mc.Warn, &mc.Error, &mc.Fatal, &mc.Total); err != nil {
			log.Printf("duckdb scan error (SeverityCountsByMinute): %v", err)
			continue
		}
		results = append(results, mc)
	}
	return results, rows.Err()
}

// TotalLogCount returns the total number of logs in the database.
func (s *Store) TotalLogCount(opts QueryOpts) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM logs %s`, where)

	var count int64
	err := s.db.QueryRowContext(ctx, query, wArgs...).Scan(&count)
	return count, err
}

// TotalLogBytes returns the total raw-line bytes persisted in logs.
func (s *Store) TotalLogBytes(opts QueryOpts) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`SELECT COALESCE(SUM(length(raw_line)), 0) FROM logs %s`, where)

	var total int64
	err := s.db.QueryRowContext(ctx, query, wArgs...).Scan(&total)
	return total, err
}

// TopHosts returns hostnames by descending log count.
func (s *Store) TopHosts(limit int, opts QueryOpts) ([]DimensionCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(hostname, ''), 'unknown') AS host, COUNT(*) AS count
		FROM logs %s
		GROUP BY host
		ORDER BY count DESC, host ASC
		LIMIT ?`, where)

	args := append(wArgs, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DimensionCount
	for rows.Next() {
		var item DimensionCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			log.Printf("duckdb scan error (TopHosts): %v", err)
			continue
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

// TopServices returns services by descending log count.
func (s *Store) TopServices(limit int, opts QueryOpts) ([]DimensionCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	where, wArgs := appFilter(opts)
	query := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(service, ''), 'unknown') AS service, COUNT(*) AS count
		FROM logs %s
		GROUP BY service
		ORDER BY count DESC, service ASC
		LIMIT ?`, where)

	args := append(wArgs, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DimensionCount
	for rows.Next() {
		var item DimensionCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			log.Printf("duckdb scan error (TopServices): %v", err)
			continue
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

// TopServicesBySeverity returns the top services for a given severity level.
func (s *Store) TopServicesBySeverity(severity string, limit int, opts QueryOpts) ([]DimensionCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	andApp, aArgs := appAnd(opts)
	query := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(service, ''), 'unknown') AS svc, COUNT(*) AS count
		FROM logs
		WHERE level = ?%s
		GROUP BY svc
		ORDER BY count DESC, svc ASC
		LIMIT ?`, andApp)

	args := append([]interface{}{severity}, aArgs...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DimensionCount
	for rows.Next() {
		var item DimensionCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			log.Printf("duckdb scan error (TopServicesBySeverity): %v", err)
			continue
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

// ListApps returns all distinct app names from the logs table.
func (s *Store) ListApps() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT app FROM logs ORDER BY app`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []string
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			log.Printf("duckdb scan error (ListApps): %v", err)
			continue
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

// ExecuteQuery runs a read-only SQL query and returns results as maps.
// Only SELECT/WITH read queries are allowed; DDL/DML is rejected.
func (s *Store) ExecuteQuery(query string) ([]map[string]interface{}, error) {
	trimmed := strings.TrimSpace(query)

	// Reject semicolons to prevent statement chaining.
	if strings.Contains(trimmed, ";") {
		return nil, fmt.Errorf("query must not contain semicolons")
	}

	// Strip SQL comments so keywords hidden in comments are still caught.
	stripped := strings.TrimSpace(stripSQLComments(trimmed))
	upper := strings.ToUpper(stripped)

	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return nil, fmt.Errorf("only SELECT/WITH queries are allowed")
	}

	// Defense-in-depth: reject dangerous keywords after comment stripping.
	if match := dangerousKeywordPattern.FindString(stripped); match != "" {
		return nil, fmt.Errorf("query contains disallowed keyword: %s", strings.ToUpper(match))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()
	rows, err := s.db.QueryContext(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	maxRows := 1000

	for rows.Next() && len(results) < maxRows {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			log.Printf("duckdb scan error (ExecuteQuery): %v", err)
			continue
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

// GetSchemaDescription returns a human-readable schema description for AI prompts.
func (s *Store) GetSchemaDescription() string {
	return `Table 'logs': id (BIGINT), timestamp (TIMESTAMP), orig_timestamp (TIMESTAMP), ` +
		`level (VARCHAR: TRACE/DEBUG/INFO/WARN/ERROR/FATAL), level_num (INTEGER), ` +
		`message (VARCHAR), raw_line (VARCHAR), service (VARCHAR), hostname (VARCHAR), ` +
		`pid (INTEGER), attributes (JSON), source (VARCHAR: tcp/stdin/file), app (VARCHAR), ` +
		`event_id (VARCHAR, replay-stable id for dedupe).`
}

// TableRowCounts returns the row count for each known table using a hardcoded allowlist.
func (s *Store) TableRowCounts() (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	allowedTables := []string{"logs"}
	counts := make(map[string]int64, len(allowedTables))

	for _, table := range allowedTables {
		var count int64
		// Table names are hardcoded constants, not user input.
		err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			continue
		}
		counts[table] = count
	}
	return counts, nil
}

// RecentLogsFiltered returns recent log records with optional filtering by app,
// severity levels, and message pattern (regex).
func (s *Store) RecentLogsFiltered(limit int, app string, severityLevels []string, messagePattern string) ([]LogRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	var conditions []string
	var args []interface{}

	if app != "" {
		conditions = append(conditions, "app = ?")
		args = append(args, app)
	}

	if len(severityLevels) > 0 {
		placeholders := make([]string, len(severityLevels))
		for i, lvl := range severityLevels {
			placeholders[i] = "?"
			args = append(args, lvl)
		}
		conditions = append(conditions, "level IN ("+strings.Join(placeholders, ", ")+")")
	}

	if messagePattern != "" {
		conditions = append(conditions, "regexp_matches(message, ?)")
		args = append(args, messagePattern)
	}

	innerQuery := "SELECT timestamp, orig_timestamp, level, level_num, message, raw_line, service, hostname, pid, CAST(attributes AS VARCHAR) AS attributes, source, app FROM logs"
	if len(conditions) > 0 {
		innerQuery += " WHERE " + strings.Join(conditions, " AND ")
	}
	innerQuery += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	// Wrap so final results come back in chronological (ASC) order.
	query := "SELECT * FROM (" + innerQuery + ") ORDER BY timestamp ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []LogRecord
	for rows.Next() {
		var r LogRecord
		var origTS sql.NullTime
		var attrsJSON string
		if err := rows.Scan(&r.Timestamp, &origTS, &r.Level, &r.LevelNum, &r.Message, &r.RawLine, &r.Service, &r.Hostname, &r.PID, &attrsJSON, &r.Source, &r.App); err != nil {
			log.Printf("duckdb scan error (RecentLogsFiltered): %v", err)
			continue
		}
		if origTS.Valid {
			r.OrigTimestamp = origTS.Time
		}
		// Parse attributes JSON back to map; always initialize to non-nil.
		r.Attributes = make(map[string]string)
		if attrsJSON != "" && attrsJSON != "{}" {
			parseJSONMap(attrsJSON, r.Attributes)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// SearchLogs performs a case-insensitive substring search on log messages.
func (s *Store) SearchLogs(term string, limit int, opts QueryOpts) ([]LogRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := s.queryCtx()
	defer cancel()

	andApp, aArgs := appAnd(opts)
	query := fmt.Sprintf(`SELECT timestamp, orig_timestamp, level, level_num, message, raw_line, service, hostname, pid, CAST(attributes AS VARCHAR) AS attributes, source, app
		FROM logs
		WHERE contains(lower(message), lower(?))%s
		ORDER BY timestamp DESC
		LIMIT ?`, andApp)

	args := append([]interface{}{term}, aArgs...)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []LogRecord
	for rows.Next() {
		var r LogRecord
		var origTS sql.NullTime
		var attrsJSON string
		if err := rows.Scan(&r.Timestamp, &origTS, &r.Level, &r.LevelNum, &r.Message, &r.RawLine, &r.Service, &r.Hostname, &r.PID, &attrsJSON, &r.Source, &r.App); err != nil {
			log.Printf("duckdb scan error (SearchLogs): %v", err)
			continue
		}
		if origTS.Valid {
			r.OrigTimestamp = origTS.Time
		}
		r.Attributes = make(map[string]string)
		if attrsJSON != "" && attrsJSON != "{}" {
			parseJSONMap(attrsJSON, r.Attributes)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// parseJSONMap parses a JSON string into a map[string]string.
func parseJSONMap(jsonStr string, dest map[string]string) error {
	// Simple JSON map parser for {"key":"value",...} format
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return err
	}
	for k, v := range raw {
		dest[k] = fmt.Sprintf("%v", v)
	}
	return nil
}
