package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tinytelemetry/lotus/internal/model"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// renderCountsModalWithViewport renders the log counts modal using the provided viewport and data.
func (m *DashboardModel) renderCountsModalWithViewport(vp *viewport.Model, cm *CountsModal, width, height int) string {
	// Calculate dimensions
	modalWidth := width - 8   // Leave 4 chars margin on each side
	modalHeight := height - 4 // Leave 2 lines margin top and bottom

	// Account for borders and headers
	contentWidth := modalWidth - 4   // Modal borders
	contentHeight := modalHeight - 4 // Header + status

	// Update viewport
	vp.Width = contentWidth
	vp.Height = contentHeight

	// Get counts modal content and set it to viewport
	countsContent := m.renderCountsModalContent(contentWidth, cm)
	vp.SetContent(countsContent)

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
		Render("Log Counts Analysis")

	// Status bar
	statusBar := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render("up/down/Wheel: Scroll | PgUp/PgDn: Page | ESC: Close")

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

// renderCountsModalContent renders the content for the counts modal
func (m *DashboardModel) renderCountsModalContent(contentWidth int, cm *CountsModal) string {
	var sections []string

	// Title section
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorBlue).
		Bold(true).
		Align(lipgloss.Center).
		Width(contentWidth)

	sections = append(sections, titleStyle.Render("Log Activity Analysis"))
	sections = append(sections, "")

	// Heatmap section - full width
	heatmapSection := m.renderHeatmapSection(contentWidth, cm.countsHeatmapData)
	sections = append(sections, heatmapSection)
	sections = append(sections, "")

	// Calculate width for side-by-side sections
	halfWidth := (contentWidth - 3) / 2 // -3 for spacing between columns

	// Side-by-side sections: Patterns by Severity | Services by Severity
	patternsSection := m.renderPatternsBySeveritySection(halfWidth)
	servicesSection := m.renderServicesBySeveritySection(halfWidth, cm.countsServicesData)

	sideBySide := lipgloss.JoinHorizontal(lipgloss.Top, patternsSection, servicesSection)
	sections = append(sections, sideBySide)

	return strings.Join(sections, "\n")
}

// renderHeatmapSection renders the severity heatmap chart using provided data
func (m *DashboardModel) renderHeatmapSection(width int, minuteData []model.MinuteCounts) string {
	// Use deckTitleStyle for consistent title formatting
	titleContent := deckTitleStyle.Render("Severity Activity Heatmap (Last 60 Minutes)")

	var contentLines []string

	now := time.Now()

	// Build time axis header
	timeHeader := "Time (mins ago):"
	dataHeader := ""
	for i := 60; i >= 0; i-- {
		if i%5 == 0 {
			if i >= 10 {
				if i%10 == 0 {
					dataHeader += fmt.Sprintf("%2d", i)
					if i > 0 {
						i--
					}
				} else {
					dataHeader += " "
				}
			} else {
				dataHeader += fmt.Sprintf("%d", i)
			}
		} else {
			dataHeader += " "
		}
	}

	timeHeader += dataHeader
	contentLines = append(contentLines, timeHeader)
	contentLines = append(contentLines, strings.Repeat("─", len(timeHeader)))

	// Index minute data by truncated timestamp for O(1) lookup
	minuteIndex := make(map[time.Time]*model.MinuteCounts, len(minuteData))
	for i := range minuteData {
		minuteIndex[minuteData[i].Minute.Truncate(time.Minute)] = &minuteData[i]
	}

	// Get severity order and colors
	severities := []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG", "TRACE"}
	colors := map[string]lipgloss.Color{
		"FATAL": ColorRed, "ERROR": ColorRed, "WARN": ColorOrange,
		"INFO": ColorBlue, "DEBUG": ColorGray, "TRACE": ColorGray,
	}

	// Calculate max count per severity for individual scaling
	maxCounts := make(map[string]int64)
	severityTotals := make(map[string]int64)
	for _, severity := range severities {
		maxCounts[severity] = 1
	}

	for _, mc := range minuteData {
		for _, severity := range severities {
			count := countForSeverity(&mc, severity)
			severityTotals[severity] += count
			if count > maxCounts[severity] {
				maxCounts[severity] = count
			}
		}
	}

	// Render each severity level row
	for _, severity := range severities {
		severityWithCount := fmt.Sprintf("%s (%d)", severity, severityTotals[severity])
		coloredLabel := lipgloss.NewStyle().Foreground(getSeverityColor(severity)).Bold(true).Render(fmt.Sprintf("%-12s", severityWithCount))

		line := coloredLabel + "    "

		for i := 60; i >= 0; i-- {
			minuteTime := now.Add(time.Duration(-i) * time.Minute).Truncate(time.Minute)

			var minuteActivity int64
			found := false
			if mc, ok := minuteIndex[minuteTime]; ok {
				found = true
				minuteActivity = countForSeverity(mc, severity)
			}

			var symbol string
			if !found || minuteActivity == 0 {
				symbol = "."
			} else {
				intensity := float64(minuteActivity) / float64(maxCounts[severity])
				if intensity > 0.7 {
					symbol = "█"
				} else if intensity > 0.4 {
					symbol = "▓"
				} else if intensity > 0.1 {
					symbol = "▒"
				} else {
					symbol = "░"
				}
			}

			if found && minuteActivity > 0 {
				styledSymbol := lipgloss.NewStyle().Foreground(colors[severity]).Render(symbol)
				line += styledSymbol
			} else {
				line += symbol
			}
		}

		contentLines = append(contentLines, line)
	}

	contentLines = append(contentLines, "")
	contentLines = append(contentLines, "Legend: █ High Activity  ▓ Medium Activity  ▒ Low Activity  . No Activity")

	content := strings.Join(contentLines, "\n")

	sectionContent := lipgloss.JoinVertical(lipgloss.Left, titleContent, content)

	return sectionStyle.
		Width(width).
		Render(sectionContent)
}

// countForSeverity extracts the count for a specific severity from MinuteCounts.
func countForSeverity(mc *model.MinuteCounts, severity string) int64 {
	switch severity {
	case "FATAL":
		return mc.Fatal
	case "ERROR":
		return mc.Error
	case "WARN":
		return mc.Warn
	case "INFO":
		return mc.Info
	case "DEBUG":
		return mc.Debug
	case "TRACE":
		return mc.Trace
	}
	return 0
}

// renderPatternsBySeveritySection renders patterns grouped by severity using drain3 data
func (m *DashboardModel) renderPatternsBySeveritySection(width int) string {
	// Use deckTitleStyle for consistent title formatting
	titleContent := deckTitleStyle.Render("Top Patterns by Severity")

	var contentLines []string

	// Get patterns from severity-specific drain3 instances
	severities := []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG", "TRACE"}

	hasAnyData := false
	for _, severity := range severities {
		if drain3Instance, exists := m.drain3BySeverity[severity]; exists && drain3Instance != nil {
			patterns := drain3Instance.GetTopPatterns(3) // Get top 3 patterns for this severity
			if len(patterns) > 0 {
				hasAnyData = true

				// Severity header
				severityStyle := lipgloss.NewStyle().Foreground(getSeverityColor(severity)).Bold(true)
				contentLines = append(contentLines, severityStyle.Render(severity+":"))

				// Show patterns for this severity
				for i, pattern := range patterns {
					line := fmt.Sprintf("  %d. %s (%d)", i+1, pattern.Template, pattern.Count)
					contentLines = append(contentLines, line)
				}
				contentLines = append(contentLines, "")
			}
		}
	}

	if !hasAnyData {
		contentLines = append(contentLines, helpStyle.Render("No patterns detected yet..."))
	}

	content := strings.Join(contentLines, "\n")

	// Use sectionStyle for consistent section formatting with borders
	sectionContent := lipgloss.JoinVertical(lipgloss.Left, titleContent, content)

	return sectionStyle.
		Width(width).
		Render(sectionContent)
}

// renderServicesBySeveritySection renders services grouped by severity using provided data
func (m *DashboardModel) renderServicesBySeveritySection(width int, servicesData map[string][]model.DimensionCount) string {
	// Use deckTitleStyle for consistent title formatting
	titleContent := deckTitleStyle.Render("Top Services by Severity")

	var contentLines []string

	severities := []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG", "TRACE"}

	hasAnyData := false
	for _, severity := range severities {
		services := servicesData[severity]

		if len(services) > 0 {
			hasAnyData = true

			// Severity header
			severityStyle := lipgloss.NewStyle().Foreground(getSeverityColor(severity)).Bold(true)
			contentLines = append(contentLines, severityStyle.Render(severity+":"))

			// Show top 3 services for this severity
			for i, service := range services {
				line := fmt.Sprintf("  %d. %s (%d)", i+1, service.Value, service.Count)
				contentLines = append(contentLines, line)
			}
			contentLines = append(contentLines, "")
		}
	}

	if !hasAnyData {
		contentLines = append(contentLines, helpStyle.Render("No service data available yet..."))
	}

	content := strings.Join(contentLines, "\n")

	// Use sectionStyle for consistent section formatting with borders
	sectionContent := lipgloss.JoinVertical(lipgloss.Left, titleContent, content)

	return sectionStyle.
		Width(width).
		Render(sectionContent)
}
