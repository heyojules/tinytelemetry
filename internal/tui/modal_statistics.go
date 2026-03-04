package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderStatsModalWithViewport renders the statistics modal using the provided viewport.
func (m *DashboardModel) renderStatsModalWithViewport(vp *viewport.Model, stats *StatsModal, width, height int) string {
	// Calculate dimensions
	modalWidth := width - 8   // Leave 4 chars margin on each side
	modalHeight := height - 4 // Leave 2 lines margin top and bottom

	// Account for borders and headers
	contentWidth := modalWidth - 4   // Modal borders
	contentHeight := modalHeight - 4 // Header + status

	// Update viewport
	vp.Width = contentWidth
	vp.Height = contentHeight

	// Get statistics content and set it to viewport
	statsContent := m.renderStatsContent(contentWidth, stats)
	vp.SetContent(statsContent)

	// Create content pane
	contentPane := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorGray).
		Render(vp.View())

	// Header with title
	header := lipgloss.NewStyle().
		Width(contentWidth).
		Foreground(ColorBlue).
		Bold(true).
		Render("Log Statistics")

	// Status bar
	statusBar := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render("up/down/Wheel: Scroll | PgUp/PgDn: Page | i: Toggle Stats | ESC: Close")

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
