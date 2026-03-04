package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tinytelemetry/lotus/internal/model"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

func renderSeverityModalView(vp *viewport.Model, sm *SeverityModal, width, height int) string {
	modalWidth := width - 4
	modalHeight := height - 2

	contentWidth := modalWidth - 4
	contentHeight := modalHeight - 6

	vp.Width = contentWidth
	vp.Height = contentHeight

	content := renderSeverityModalContent(sm, contentWidth, contentHeight)
	vp.SetContent(content)

	tabBar := renderTimeRangeTabs(sm.rangeLabels, sm.activeRange, contentWidth)

	contentPane := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorGray).
		Render(vp.View())

	header := lipgloss.NewStyle().
		Width(contentWidth).
		Foreground(ColorBlue).
		Bold(true).
		Render("Severity Timeline")

	statusBar := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render("Tab/←→: Switch range | 1/2/3: Jump to range | ↑↓/Wheel: Scroll | ESC: Close")

	modal := lipgloss.JoinVertical(lipgloss.Left, header, tabBar, contentPane, statusBar)

	finalModal := lipgloss.NewStyle().
		Width(modalWidth).
		Height(modalHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Render(modal)

	return finalModal
}

func renderTimeRangeTabs(labels []string, active int, width int) string {
	var tabs []string
	for i, label := range labels {
		style := lipgloss.NewStyle().Padding(0, 2)
		if i == active {
			style = style.
				Foreground(ColorBlue).
				Bold(true).
				Underline(true)
		} else {
			style = style.Foreground(ColorGray)
		}
		tabs = append(tabs, style.Render(label))
	}
	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	return lipgloss.NewStyle().Width(width).Render(tabLine)
}

func renderSeverityModalContent(sm *SeverityModal, width, height int) string {
	if len(sm.data) == 0 {
		return helpStyle.Render("No data available")
	}

	// Time span based on active range
	var totalSpan time.Duration
	switch sm.activeRange {
	case 0:
		totalSpan = 24 * time.Hour
	case 1:
		totalSpan = 7 * 24 * time.Hour
	case 2:
		totalSpan = 30 * 24 * time.Hour
	}

	return renderFullChart(sm.data, width, height, totalSpan)
}

func renderFullChart(data []model.MinuteCounts, width, height int, totalSpan time.Duration) string {
	barWidth := 2
	showLegend := true
	legendWidth := 18
	legendGap := 3

	if width < 80 {
		barWidth = 1
		legendWidth = 14
		legendGap = 2
	}
	if width < 50 {
		showLegend = false
		legendWidth = 0
		legendGap = 0
	}

	yAxisWidthEstimate := 7
	borderChar := 1

	chartAreaWidth := width - yAxisWidthEstimate - borderChar - legendGap - legendWidth
	if chartAreaWidth < 10 {
		chartAreaWidth = 10
	}

	chartHeight := height - 8
	if chartHeight < 6 {
		chartHeight = 6
	}

	stride := barWidth + 1
	maxBars := chartAreaWidth / stride
	if maxBars < 1 {
		maxBars = 1
	}

	// Fixed timeline: now-span → now
	now := time.Now().Truncate(time.Minute)
	timelineStart := now.Add(-totalSpan)
	bucketDuration := totalSpan / time.Duration(maxBars)
	if bucketDuration < time.Minute {
		bucketDuration = time.Minute
	}
	numBars := maxBars

	// Index source data by Unix timestamp (avoids timezone mismatch)
	dataIndex := make(map[int64]*model.MinuteCounts, len(data))
	for i := range data {
		dataIndex[data[i].Minute.Truncate(time.Minute).Unix()] = &data[i]
	}

	// Aggregate into buckets
	type bucket struct {
		model.MinuteCounts
	}
	buckets := make([]bucket, numBars)
	for i := 0; i < numBars; i++ {
		bStart := timelineStart.Add(time.Duration(i) * bucketDuration)
		bEnd := bStart.Add(bucketDuration)
		for t := bStart.Truncate(time.Minute); t.Before(bEnd); t = t.Add(time.Minute) {
			if mc, ok := dataIndex[t.Unix()]; ok {
				buckets[i].Trace += mc.Trace
				buckets[i].Debug += mc.Debug
				buckets[i].Info += mc.Info
				buckets[i].Warn += mc.Warn
				buckets[i].Error += mc.Error
				buckets[i].Fatal += mc.Fatal
				buckets[i].Total += mc.Total
			}
		}
	}

	// Compute Y-axis
	rawMax := int64(0)
	for _, b := range buckets {
		if b.Total > rawMax {
			rawMax = b.Total
		}
	}
	yCfg := computeYAxis(rawMax, 5)
	yAxisWidth := yCfg.LabelWidth

	barStyles := make(map[string]lipgloss.Style, len(chartSeverities))
	for _, sev := range chartSeverities {
		barStyles[sev.name] = lipgloss.NewStyle().Foreground(lipgloss.Color(sev.color))
	}

	// Render rows
	rows := make([]string, chartHeight)
	for row := 0; row < chartHeight; row++ {
		rowTopVal := yCfg.Max - (yCfg.Max*int64(row))/int64(chartHeight)
		rowBotVal := yCfg.Max - (yCfg.Max*int64(row+1))/int64(chartHeight)

		yLabel := renderYLabel(yCfg, row, chartHeight)

		var barArea strings.Builder
		for i, b := range buckets {
			segments := stackedSegments(b.MinuteCounts)
			cellStr := renderBarCell(segments, b.Total, yCfg.Max, rowBotVal, rowTopVal, barWidth, barStyles)
			barArea.WriteString(cellStr)
			if i < numBars-1 {
				barArea.WriteString(" ")
			}
		}

		rows[row] = yLabel + "│" + barArea.String()
	}

	// X-axis line
	xAxisLine := strings.Repeat(" ", yAxisWidth) + "└"
	for i := 0; i < numBars; i++ {
		xAxisLine += strings.Repeat("─", barWidth)
		if i < numBars-1 {
			xAxisLine += "┴"
		}
	}

	// X-axis labels — evenly spaced across the timeline
	xLabels := buildAdaptiveTimeLabels(timelineStart, now, numBars, yAxisWidth+1, stride, chartAreaWidth)

	// Legend — totals across all buckets
	var legendLines []string
	if showLegend {
		var totals model.MinuteCounts
		for _, b := range buckets {
			totals.Trace += b.Trace
			totals.Debug += b.Debug
			totals.Info += b.Info
			totals.Warn += b.Warn
			totals.Error += b.Error
			totals.Fatal += b.Fatal
			totals.Total += b.Total
		}
		legendLines = buildLegendLines(totals, chartHeight+2)
	}

	// Combine
	var outputLines []string
	for i, row := range rows {
		line := row
		if showLegend && i < len(legendLines) {
			line += strings.Repeat(" ", legendGap) + legendLines[i]
		}
		outputLines = append(outputLines, line)
	}

	xAxisWithLegend := xAxisLine
	if showLegend && len(rows) < len(legendLines) {
		xAxisWithLegend += strings.Repeat(" ", legendGap) + legendLines[len(rows)]
	}
	outputLines = append(outputLines, xAxisWithLegend)

	xLabelsWithLegend := xLabels
	if showLegend && len(rows)+1 < len(legendLines) {
		xLabelsWithLegend += strings.Repeat(" ", legendGap) + legendLines[len(rows)+1]
	}
	outputLines = append(outputLines, xLabelsWithLegend)

	// Summary
	outputLines = append(outputLines, "")
	first := timelineStart.Format("2006-01-02 15:04")
	last := now.Format("2006-01-02 15:04")
	summaryStyle := lipgloss.NewStyle().Foreground(ColorGray)
	outputLines = append(outputLines, summaryStyle.Render(fmt.Sprintf("Range: %s → %s", first, last)))

	return strings.Join(outputLines, "\n")
}
