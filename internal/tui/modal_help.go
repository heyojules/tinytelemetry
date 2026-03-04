package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderHelpModalWithViewport renders the help modal using the provided viewport.
func (m *DashboardModel) renderHelpModalWithViewport(vp *viewport.Model, width, height int) string {
	// Calculate dimensions
	modalWidth := width - 8   // Leave 4 chars margin on each side
	modalHeight := height - 4 // Leave 2 lines margin top and bottom

	// Account for borders and headers
	contentWidth := modalWidth - 4   // Modal borders
	contentHeight := modalHeight - 4 // Header + status

	// Update viewport
	vp.Width = contentWidth
	vp.Height = contentHeight

	// Get help content and wrap it properly
	helpContent := m.renderHelpModalContent()
	wrappedContent := m.wrapTextToWidth(helpContent, contentWidth)
	vp.SetContent(wrappedContent)

	// Create content pane
	contentPane := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorGray).
		Render(vp.View())

	// Header
	header := lipgloss.NewStyle().
		Width(contentWidth).
		Foreground(ColorBlue).
		Bold(true).
		Render("Help & Documentation")

	// Status bar
	statusBar := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render("up/down/Wheel: Scroll | PgUp/PgDn: Page | ?/h: Toggle Help | ESC: Close")

	// Combine all parts
	modal := lipgloss.JoinVertical(lipgloss.Left, header, contentPane, statusBar)

	// Add outer border and center
	finalModal := lipgloss.NewStyle().
		Width(modalWidth).
		Height(modalHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Render(modal)

	return finalModal
}

// renderHelpModalContent returns the help modal content without positioning
func (m *DashboardModel) renderHelpModalContent() string {
	helpContent := `Log Analyzer Dashboard Help

NAVIGATION:
  Tab/Shift+Tab  - Navigate between sections
  Mouse Click    - Click on any section to switch to it
  up/down or k/j - Move selection within section
  Mouse Wheel    - Scroll up/down to navigate selections
  Enter          - Show details for selected item
  Escape         - Close modal/exit filter mode

ACTIONS:
  /              - Activate filter (regex supported)
  s              - Search and highlight text in logs
  [ / ]          - Switch view (deck sets)
  G              - Search and jump to log entries
  Ctrl+f         - Open severity filter modal
  f              - Open fullscreen log viewer modal
  Space          - Pause/unpause UI updates (manual)
  c              - Toggle Host/Service columns in log view
  T              - Toggle timestamp mode (Log Time / Receive Time)
  r              - Reset pattern extraction state
  u/U            - Cycle update intervals (forward/backward)
  i              - Show comprehensive statistics modal
  ? or h         - Toggle this help
  q/Ctrl+C       - Quit

LOG VIEWER NAVIGATION:
  Home           - Jump to top of log buffer (stops auto-scroll)
  End            - Jump to latest logs (resumes auto-scroll)
  PgUp/PgDn      - Navigate by pages (10 entries at a time)
  up/down or k/j - Navigate individual entries with smart auto-scroll

SECTIONS:
  Views (left)   - Sidebar view navigation (Base/Patterns/Attributes)
  Apps (left)    - Instant app list and app-level filtering
  Words          - Most frequent words in logs
  Attributes     - Log attributes by unique value count
  Log Patterns   - Common log message patterns (Drain3)
  Counts         - Log counts over time
  Logs           - Navigate and inspect individual log entries
                 - Live updates auto-pause while Logs is focused

FILTER & SEARCH:
  Filter (/): Type regex patterns to filter logs (searches message & attributes)
  Search (s): Type text to highlight in displayed logs
  Severity (Ctrl+f): Filter by log severity levels
  Examples: "error", "k8s.*pod", "service.name", "host.name.*prod"

`

	return lipgloss.NewStyle().
		Width(65).
		Render(helpContent)
}
