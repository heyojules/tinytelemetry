package socketrpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/tinytelemetry/lotus/internal/model"
)

// Client implements model.LogQuerier over a Unix domain socket using JSON-RPC 2.0.
type Client struct {
	conn    net.Conn
	mu      sync.Mutex
	nextID  int
	scanner *bufio.Scanner
	encoder *json.Encoder
}

// Dial connects to the socket RPC server at the given path.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("socketrpc: dial: %w", err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	return &Client{
		conn:    conn,
		scanner: scanner,
		encoder: json.NewEncoder(conn),
	}, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// call performs a JSON-RPC call and unmarshals the result into dest.
func (c *Client) call(method string, params interface{}, dest interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	id := c.nextID

	paramsData, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("socketrpc: marshal params: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsData,
	}

	c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	if err := c.encoder.Encode(req); err != nil {
		return fmt.Errorf("socketrpc: send: %w", err)
	}

	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return fmt.Errorf("socketrpc: read: %w", err)
		}
		return fmt.Errorf("socketrpc: connection closed")
	}

	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("socketrpc: unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if dest != nil {
		if err := json.Unmarshal(resp.Result, dest); err != nil {
			return fmt.Errorf("socketrpc: unmarshal result: %w", err)
		}
	}
	return nil
}

func (c *Client) TotalLogCount(opts model.QueryOpts) (int64, error) {
	var result int64
	err := c.call("TotalLogCount", map[string]interface{}{"Opts": opts}, &result)
	return result, err
}

func (c *Client) TotalLogBytes(opts model.QueryOpts) (int64, error) {
	var result int64
	err := c.call("TotalLogBytes", map[string]interface{}{"Opts": opts}, &result)
	return result, err
}

func (c *Client) TopWords(limit int, opts model.QueryOpts) ([]model.WordCount, error) {
	var result []model.WordCount
	err := c.call("TopWords", map[string]interface{}{"Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) TopAttributes(limit int, opts model.QueryOpts) ([]model.AttributeStat, error) {
	var result []model.AttributeStat
	err := c.call("TopAttributes", map[string]interface{}{"Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) TopAttributeKeys(limit int, opts model.QueryOpts) ([]model.AttributeKeyStat, error) {
	var result []model.AttributeKeyStat
	err := c.call("TopAttributeKeys", map[string]interface{}{"Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) AttributeKeyValues(key string, limit int) (map[string]int64, error) {
	var result map[string]int64
	err := c.call("AttributeKeyValues", map[string]interface{}{"Key": key, "Limit": limit}, &result)
	return result, err
}

func (c *Client) SeverityCounts(opts model.QueryOpts) (map[string]int64, error) {
	var result map[string]int64
	err := c.call("SeverityCounts", map[string]interface{}{"Opts": opts}, &result)
	return result, err
}

func (c *Client) SeverityCountsByMinute(opts model.QueryOpts) ([]model.MinuteCounts, error) {
	var result []model.MinuteCounts
	err := c.call("SeverityCountsByMinute", map[string]interface{}{"Opts": opts}, &result)
	return result, err
}

func (c *Client) TopHosts(limit int, opts model.QueryOpts) ([]model.DimensionCount, error) {
	var result []model.DimensionCount
	err := c.call("TopHosts", map[string]interface{}{"Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) TopServices(limit int, opts model.QueryOpts) ([]model.DimensionCount, error) {
	var result []model.DimensionCount
	err := c.call("TopServices", map[string]interface{}{"Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) TopServicesBySeverity(severity string, limit int, opts model.QueryOpts) ([]model.DimensionCount, error) {
	var result []model.DimensionCount
	err := c.call("TopServicesBySeverity", map[string]interface{}{"Severity": severity, "Limit": limit, "Opts": opts}, &result)
	return result, err
}

func (c *Client) ListApps() ([]string, error) {
	var result []string
	err := c.call("ListApps", map[string]interface{}{}, &result)
	return result, err
}

func (c *Client) RecentLogsFiltered(limit int, app string, severityLevels []string, messagePattern string) ([]model.LogRecord, error) {
	var result []model.LogRecord
	err := c.call("RecentLogsFiltered", map[string]interface{}{
		"Limit":          limit,
		"App":            app,
		"SeverityLevels": severityLevels,
		"MessagePattern": messagePattern,
	}, &result)
	return result, err
}

func (c *Client) SearchLogs(term string, limit int, opts model.QueryOpts) ([]model.LogRecord, error) {
	var result []model.LogRecord
	err := c.call("SearchLogs", map[string]interface{}{
		"Term":  term,
		"Limit": limit,
		"Opts":  opts,
	}, &result)
	return result, err
}
