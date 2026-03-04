package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderSingleModalView renders a simple scrollable modal with the given content.
func (m *DashboardModel) renderSingleModalView(vp *viewport.Model, content string, width, height int) string {
	// Calculate dimensions
	modalWidth := width - 8   // 4 chars margin on each side
	modalHeight := height - 6 // 3 lines margin top and bottom

	// Account for borders and headers
	contentWidth := modalWidth - 4   // Modal borders
	contentHeight := modalHeight - 4 // Header + status

	// Update viewport
	vp.Width = contentWidth
	vp.Height = contentHeight
	vp.SetContent(content)

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
		Render("Top Values")

	// Status bar
	statusBar := renderModalStatusBar()

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

// renderModalStatusBar renders the status bar for modals
func renderModalStatusBar() string {
	statusItems := []string{"up/down/Wheel: Scroll", "PgUp/PgDn: Page", "ESC: Close"}

	statusStyle := lipgloss.NewStyle().
		Foreground(ColorGray)

	return statusStyle.Render(strings.Join(statusItems, " | "))
}

// getSeverityColor returns the appropriate color for a severity level
func getSeverityColor(severity string) lipgloss.Color {
	switch severity {
	case "FATAL", "CRITICAL":
		return ColorRed
	case "ERROR":
		return ColorRed
	case "WARN":
		return ColorOrange
	case "INFO":
		return ColorBlue
	case "DEBUG", "TRACE":
		return ColorGray
	default:
		return ColorWhite
	}
}
