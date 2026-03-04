package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// contentWidth returns the width available for main content, accounting for sidebar.
func (m *DashboardModel) contentWidth() int {
	if m.sidebarVisible {
		w := m.width - sidebarWidth
		if w < 40 {
			w = 40
		}
		return w
	}
	return m.width
}

// layoutHeights computes the three main vertical layout sections so that both
// renderDashboard and visibleLogLines share a single source of truth.
func (m *DashboardModel) layoutHeights() (decksHeight, filterHeight, logsHeight int) {
	statusLineHeight := 1
	usableHeight := m.height - statusLineHeight

	filterHeight = 0
	if m.hasFilterOrSearch() {
		filterHeight = 1
	}

	if len(m.decks) == 0 {
		// No decks → placeholder page (no content areas needed).
		return 0, filterHeight, 0
	}
	// Has decks → full-height decks grid.
	return usableHeight - filterHeight, filterHeight, 0
}

// hasFilterOrSearch returns true if a filter or search is active or applied
func (m *DashboardModel) hasFilterOrSearch() bool {
	return m.filterActive || m.searchActive ||
		m.filterRegex != nil || m.filterInput.Value() != "" ||
		m.searchTerm != "" || m.searchInput.Value() != ""
}

// View renders the dashboard
func (m *DashboardModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "Initializing dashboard..."
	}

	base := m.renderDashboard()

	// If a modal is on the stack, overlay it on the dashboard.
	if modal := m.TopModal(); modal != nil {
		fg := modal.View(m.width, m.height)
		x := (m.width - lipgloss.Width(fg)) / 2
		y := (m.height - lipgloss.Height(fg)) / 3
		return placeOverlay(x, y, fg, base)
	}

	return base
}

// renderDashboard renders the main dashboard layout
func (m *DashboardModel) renderDashboard() string {
	// Ensure minimum dimensions
	if m.height < 20 || m.width < 60 {
		return "Terminal too small. Resize to at least 60x20."
	}

	// Determine content width (accounting for sidebar)
	contentWidth := m.contentWidth()
	showSidebar := m.sidebarVisible

	statusLineHeight := 1

	// No decks: show placeholder (Metrics, Analytics, etc.).
	if len(m.decks) == 0 {
		placeholderHeight := m.height - statusLineHeight
		placeholder := renderEmptyPagePlaceholder(m.currentPageTitle(), contentWidth, placeholderHeight)

		statusLine := m.renderStatusLine()
		contentArea := lipgloss.JoinVertical(lipgloss.Left, placeholder, statusLine)

		var result string
		if showSidebar {
			sidebar := m.renderSidebar(m.height - 2)
			result = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, contentArea)
		} else {
			result = contentArea
		}
		return m.viewStyle.Render(result)
	}

	decksHeight, _, _ := m.layoutHeights()

	var sections []string

	// Decks grid.
	if decksHeight > 0 {
		topSection := m.renderDecksGrid(contentWidth, decksHeight)

		// View flash overlay: show title briefly after view switch.
		if m.viewFlashTitle != "" && time.Now().Before(m.viewFlashExpiry) {
			flashStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBlue).
				Align(lipgloss.Center)
			flashText := flashStyle.Render("[ " + m.viewFlashTitle + " ]")
			topSection = lipgloss.Place(contentWidth, decksHeight, lipgloss.Center, lipgloss.Center, flashText,
				lipgloss.WithWhitespaceChars(" "),
				lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
			)
		}

		sections = append(sections, topSection)
	}

	// Filter bar (shown when active).
	if m.hasFilterOrSearch() {
		filterSection := m.renderFilter()
		sections = append(sections, filterSection)
	}

	// Combine sections with strict height constraints
	mainContent := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Add status line at the very bottom
	statusLine := m.renderStatusLine()

	// Combine main content with status line
	contentArea := lipgloss.JoinVertical(
		lipgloss.Left,
		mainContent,
		statusLine,
	)

	var result string
	if showSidebar {
		sidebar := m.renderSidebar(m.height - 2)
		result = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, contentArea)
	} else {
		result = contentArea
	}

	// Apply cached height/width constraint to entire dashboard
	return m.viewStyle.Render(result)
}

// renderEmptyPagePlaceholder renders a centered placeholder for pages with no decks.
func renderEmptyPagePlaceholder(title string, width, height int) string {
	heading := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("7")).
		Render(title)

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Render("Coming soon")

	block := lipgloss.JoinVertical(lipgloss.Center, heading, subtitle)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// placeOverlay places fg on top of bg at position (x, y), replacing background
// characters with the foreground content.
func placeOverlay(x, y int, fg, bg string) string {
	fgLines := strings.Split(fg, "\n")
	bgLines := strings.Split(bg, "\n")

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	for i, fgLine := range fgLines {
		bgIdx := y + i
		if bgIdx >= len(bgLines) {
			break
		}

		bgLine := bgLines[bgIdx]

		// Build: bgLine[:x] + fgLine + bgLine[x+fgWidth:]
		bgRunes := []rune(bgLine)
		fgWidth := runewidth.StringWidth(lipgloss.NewStyle().Render(fgLine))

		var left string
		if x <= len(bgRunes) {
			left = string(bgRunes[:x])
		} else {
			left = string(bgRunes) + strings.Repeat(" ", x-len(bgRunes))
		}

		var right string
		rightStart := x + fgWidth
		if rightStart < len(bgRunes) {
			right = string(bgRunes[rightStart:])
		}

		bgLines[bgIdx] = left + fgLine + right
	}

	return strings.Join(bgLines, "\n")
}
