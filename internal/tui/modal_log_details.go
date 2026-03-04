package tui

import (
	"strings"

	"github.com/tinytelemetry/lotus/internal/model"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderSplitModalView renders log details modal with viewport scrolling.
func (m *DashboardModel) renderSplitModalView(vp *viewport.Model, entry *model.LogRecord, width, height int) string {
	// Calculate dimensions
	modalWidth := width - 8   // 4 chars margin on each side
	modalHeight := height - 6 // 3 lines margin top and bottom

	// Account for borders and headers
	contentWidth := modalWidth - 4   // Modal borders
	contentHeight := modalHeight - 4 // Header + status

	// Update viewport sizes
	vp.Width = contentWidth
	vp.Height = contentHeight

	// Update content with proper text wrapping
	if entry != nil {
		contentAreaWidth := contentWidth - 2
		if contentAreaWidth < 10 {
			contentAreaWidth = 10
		}
		infoContent := m.formatLogDetails(*entry, contentAreaWidth)
		wrappedInfoContent := m.wrapTextToWidth(infoContent, contentAreaWidth)
		vp.SetContent(wrappedInfoContent)
	}

	// Create content pane
	contentPane := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBlue).
		Render(vp.View())

	// Header
	header := lipgloss.NewStyle().
		Width(contentWidth).
		Foreground(ColorBlue).
		Bold(true).
		Render("Log Details")

	// Status bar
	statusItems := []string{"up/down/Wheel: Scroll", "PgUp/PgDn: Page", "ESC: Close"}

	statusBar := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render(strings.Join(statusItems, " | "))

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
