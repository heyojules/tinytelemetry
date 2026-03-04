package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tinytelemetry/lotus/internal/model"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// searchDebounceMsg is sent after a debounce delay to trigger a search query.
type searchDebounceMsg struct {
	query string
	seqNo int
}

// searchResultsMsg carries search results back from the async query.
type searchResultsMsg struct {
	query   string
	results []model.LogRecord
	err     error
}

// SearchModal is a floating search overlay that queries DuckDB for matching logs.
type SearchModal struct {
	dashboard *DashboardModel
	input     textinput.Model
	results   []model.LogRecord
	cursor    int
	seqNo     int
	lastQuery string
	err       error
}

// NewSearchModal creates a new search modal.
func NewSearchModal(m *DashboardModel) *SearchModal {
	ti := textinput.New()
	ti.Placeholder = "search logs..."
	ti.Prompt = "🔍 "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColorBlue)
	ti.TextStyle = lipgloss.NewStyle().Foreground(ColorWhite)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorGray)
	ti.CharLimit = 200
	ti.Focus()

	return &SearchModal{
		dashboard: m,
		input:     ti,
	}
}

func (s *SearchModal) ID() string { return "search" }

func (s *SearchModal) Update(msg tea.Msg) (pop bool, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "escape"))):
			return true, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
			if s.cursor > 0 {
				s.cursor--
			}
			return false, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
			if s.cursor < len(s.results)-1 {
				s.cursor++
			}
			return false, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if s.cursor >= 0 && s.cursor < len(s.results) {
				s.jumpToResult(s.results[s.cursor])
				return true, nil
			}
			return false, nil

		default:
			// Forward to text input
			var inputCmd tea.Cmd
			s.input, inputCmd = s.input.Update(msg)

			// Debounce: increment seqNo and schedule a check
			s.seqNo++
			currentSeq := s.seqNo
			currentQuery := s.input.Value()

			debounceCmd := tea.Tick(250*time.Millisecond, func(_ time.Time) tea.Msg {
				return searchDebounceMsg{query: currentQuery, seqNo: currentSeq}
			})

			return false, tea.Batch(inputCmd, debounceCmd)
		}

	case searchDebounceMsg:
		// Ignore stale debounce messages
		if msg.seqNo != s.seqNo {
			return false, nil
		}
		query := msg.query
		if len(query) < 3 {
			s.results = nil
			s.cursor = 0
			s.lastQuery = ""
			s.err = nil
			return false, nil
		}
		if query == s.lastQuery {
			return false, nil
		}
		s.lastQuery = query

		store := s.dashboard.store
		opts := s.dashboard.queryOpts()
		return false, func() tea.Msg {
			results, err := store.SearchLogs(query, 50, opts)
			return searchResultsMsg{query: query, results: results, err: err}
		}

	case searchResultsMsg:
		// Only apply if this matches the last query we sent
		if msg.query == s.lastQuery {
			s.results = msg.results
			s.err = msg.err
			s.cursor = 0
		}
		return false, nil
	}

	return false, nil
}

// jumpToResult finds the matching log entry in the dashboard's log list and jumps to it.
func (s *SearchModal) jumpToResult(result model.LogRecord) {
	m := s.dashboard
	for i, entry := range m.logEntries {
		if entry.Timestamp.Equal(result.Timestamp) && entry.Message == result.Message {
			m.selectedLogIndex = i
			m.logAutoScroll = false
			// Switch to list view if in decks section
			m.activeSection = SectionLogs
			return
		}
	}
	// If not found in current view, still set to logs section
	m.activeSection = SectionLogs
}

func (s *SearchModal) View(width, height int) string {
	// Dynamic width: fixed at min(width-8, 64), clamped to minimum 40
	modalWidth := min(width-8, 64)
	if modalWidth < 40 {
		modalWidth = 40
	}

	innerWidth := modalWidth - 4 // account for border + padding

	// Configure input width
	s.input.Width = innerWidth - 6 // account for prompt icon + spacing

	query := s.input.Value()

	// Build sections top-to-bottom
	var sections []string

	// 1. Input line
	sections = append(sections, s.input.View())

	// 2. Content area depends on state
	if len(query) < 3 {
		// Hint only
		hint := lipgloss.NewStyle().
			Foreground(ColorGray).
			Italic(true).
			Render("type 3+ characters to search...")
		sections = append(sections, hint)
	} else if s.err != nil {
		// Separator + error
		sections = append(sections, renderThinSeparator(innerWidth))
		errMsg := lipgloss.NewStyle().
			Foreground(ColorRed).
			Render(fmt.Sprintf("error: %s", s.err.Error()))
		sections = append(sections, errMsg)
	} else if len(s.results) == 0 && s.lastQuery != "" {
		// Separator + no results
		sections = append(sections, renderThinSeparator(innerWidth))
		noResults := lipgloss.NewStyle().
			Foreground(ColorGray).
			Render("no results found")
		sections = append(sections, noResults)
	} else if len(s.results) > 0 {
		// Separator + result lines + status bar
		sections = append(sections, renderThinSeparator(innerWidth))

		// Max visible results: min(12, height-10) clamped to minimum 3
		maxVisible := min(12, height-10)
		if maxVisible < 3 {
			maxVisible = 3
		}
		visible := min(len(s.results), maxVisible)

		// Compute scroll offset from cursor position
		scrollOffset := 0
		if s.cursor >= visible {
			scrollOffset = s.cursor - visible + 1
		}

		for i := scrollOffset; i < scrollOffset+visible && i < len(s.results); i++ {
			line := s.formatResultLine(s.results[i], innerWidth, i == s.cursor, query)
			sections = append(sections, line)
		}

		// Status bar
		status := lipgloss.NewStyle().
			Foreground(ColorGray).
			Render(fmt.Sprintf("%d results  │  ↑/↓  Enter: jump  Esc: close", len(s.results)))
		sections = append(sections, status)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Wrap in outer frame with rounded border
	frame := lipgloss.NewStyle().
		Width(modalWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Padding(1, 1).
		Render(content)

	return frame
}

// renderThinSeparator returns a thin horizontal line separator.
func renderThinSeparator(width int) string {
	return lipgloss.NewStyle().
		Foreground(ColorGray).
		Render(strings.Repeat("─", width))
}

// formatResultLine formats a single search result for display.
func (s *SearchModal) formatResultLine(entry model.LogRecord, maxWidth int, isSelected bool, query string) string {
	ts := s.dashboard.getDisplayTimestamp(entry).Format("15:04:05")
	severity := fmt.Sprintf("%-5s", entry.Level)

	// Calculate message space: accent(2) + ts(8) + 2 spaces + severity(5) + space = 18
	msgWidth := maxWidth - 18
	if msgWidth < 10 {
		msgWidth = 10
	}

	message := entry.Message
	if len(message) > msgWidth {
		message = message[:msgWidth-3] + "..."
	}

	if isSelected {
		accent := lipgloss.NewStyle().Foreground(ColorBlue).Render("│")
		styledTS := lipgloss.NewStyle().Foreground(ColorWhite).Render(ts)
		severityColor := GetSeverityColor(entry.Level)
		styledSev := lipgloss.NewStyle().Foreground(severityColor).Bold(true).Render(severity)

		line := fmt.Sprintf("%s %s  %s %s", accent, styledTS, styledSev, message)
		return lipgloss.NewStyle().
			Background(lipgloss.Color("#1a3a5c")).
			Foreground(ColorWhite).
			Width(maxWidth).
			Render(line)
	}

	// Non-selected: 2-space indent, gray timestamp, severity-colored level, highlighted message
	styledTS := lipgloss.NewStyle().Foreground(ColorGray).Render(ts)
	severityColor := GetSeverityColor(entry.Level)
	styledSev := lipgloss.NewStyle().Foreground(severityColor).Bold(true).Render(severity)

	// Highlight search term in message
	if query != "" {
		message = s.dashboard.highlightText(message, query)
	}

	return fmt.Sprintf("  %s  %s %s", styledTS, styledSev, message)
}
