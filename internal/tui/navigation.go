package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyPress dispatches key events: modal stack first, then inline
// handlers (filter/search), then global dashboard shortcuts.
func (m *DashboardModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}

	// Modal on stack gets the event first.
	if modal := m.TopModal(); modal != nil {
		pop, cmd := modal.Update(msg)
		if pop {
			m.PopModal()
		}
		return m, cmd
	}

	// Inline handlers (filter/search input).
	for _, entry := range m.inlineHandlers {
		if entry.isActive(m) {
			handled, cmd := entry.handler.HandleKey(m, msg)
			if handled {
				return m, cmd
			}
			break
		}
	}

	return m.handleGlobalKeys(msg)
}

// handleGlobalKeys handles dashboard-level shortcuts.
// Only reached when no modal is on the stack and no inline handler is active.
func (m *DashboardModel) handleGlobalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys

	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit

	case key.Matches(msg, k.Escape):
		// Clear applied filter/search even when not in input mode
		if m.filterRegex != nil || m.filterInput.Value() != "" || m.searchTerm != "" || m.searchInput.Value() != "" {
			m.filterActive = false
			m.searchActive = false
			m.filterInput.Blur()
			m.searchInput.Blur()
			m.filterInput.SetValue("")
			m.searchInput.SetValue("")
			m.filterRegex = nil
			m.searchTerm = ""
			if m.activeSection == SectionFilter {
				m.activeSection = SectionDecks
				if m.activeDeckIdx >= len(m.decks) {
					m.activeDeckIdx = max(0, len(m.decks)-1)
				}
			}
			return m, nil
		}

	case key.Matches(msg, k.Help):
		m.PushModal(NewHelpModal(m))
		return m, nil

	case key.Matches(msg, k.Filter):
		if m.filterRegex != nil || m.filterInput.Value() != "" {
			m.activeSection = SectionFilter
			m.filterActive = true
			m.filterInput.Focus()
		} else {
			m.activeSection = SectionFilter
			m.filterActive = true
			m.filterInput.SetValue("")
			m.filterRegex = nil
			m.filterInput.Focus()
		}
		return m, nil

	case key.Matches(msg, k.Search):
		if m.searchTerm != "" || m.searchInput.Value() != "" {
			m.activeSection = SectionFilter
			m.searchActive = true
			m.searchInput.Focus()
		} else {
			m.activeSection = SectionFilter
			m.searchActive = true
			m.searchInput.SetValue("")
			m.searchTerm = ""
			m.searchInput.Focus()
		}
		return m, nil

	case key.Matches(msg, k.ResetPatterns):
		m.drain3LastProcessed = 0
		return m, func() tea.Msg { return ManualResetMsg{} }

	case key.Matches(msg, k.ToggleSidebar):
		m.sidebarVisible = !m.sidebarVisible
		if m.sidebarVisible && m.activeSection != SectionSidebar {
			if m.store != nil {
				if apps, err := m.store.ListApps(); err == nil {
					m.appList = apps
				}
			}
			m.clampSidebarCursor()
		}
		return m, nil

	case key.Matches(msg, k.NextView):
		cmd := m.nextView()
		return m, cmd

	case key.Matches(msg, k.PrevView):
		cmd := m.prevView()
		return m, cmd

	case key.Matches(msg, k.ToggleColumns):
		m.showColumns = !m.showColumns
		return m, nil

	case key.Matches(msg, k.ToggleTimestamp):
		m.useLogTime = !m.useLogTime
		return m, nil

	case key.Matches(msg, k.Inspect):
		if m.activeSection == SectionLogs {
			if m.selectedLogIndex >= 0 && m.selectedLogIndex < len(m.logEntries) {
				entry := m.logEntries[m.selectedLogIndex]
				m.PushModal(NewDetailModal(m, &entry))
			}
		} else {
			m.PushModal(NewStatsModal(m))
		}
		return m, nil

	case key.Matches(msg, k.LogViewer):
		if len(m.logEntries) > 0 {
			m.selectedLogIndex = len(m.logEntries) - 1
		} else {
			m.selectedLogIndex = 0
		}
		m.PushModal(NewLogViewerModal(m))
		return m, nil

	case key.Matches(msg, k.SeverityFilter):
		m.PushModal(NewSeverityFilterModal(m))
		return m, nil

	case key.Matches(msg, k.SearchModal):
		m.PushModal(NewSearchModal(m))
		return m, nil

	case key.Matches(msg, k.DeckPause):
		// Per-deck pause: toggle pause on focused deck's TypeID
		if m.activeSection == SectionDecks && m.activeDeckIdx < len(m.decks) {
			if tp, ok := m.decks[m.activeDeckIdx].(TickableDeck); ok {
				tid := tp.TypeID()
				if state, exists := m.deckStates[tid]; exists {
					state.Paused = !state.Paused
				}
			}
		}
		return m, nil

	case key.Matches(msg, k.Pause):
		m.viewPaused = !m.viewPaused
		return m, nil

	case key.Matches(msg, k.IntervalUp):
		m.currentIntervalIdx = (m.currentIntervalIdx + 1) % len(m.availableIntervals)
		newInterval := m.availableIntervals[m.currentIntervalIdx]
		m.updateInterval = newInterval
		intervalStr := m.formatDuration(newInterval)
		content := fmt.Sprintf("Update Interval Changed\n\nNew interval: %s\n\nPress 'u' for next, 'U' for previous interval.\nThis controls how often the dashboard refreshes.", intervalStr)
		m.PushModal(NewDetailModalWithContent(m, content))
		return m, func() tea.Msg { return UpdateIntervalMsg(newInterval) }

	case key.Matches(msg, k.IntervalDown):
		m.currentIntervalIdx = (m.currentIntervalIdx - 1 + len(m.availableIntervals)) % len(m.availableIntervals)
		newInterval := m.availableIntervals[m.currentIntervalIdx]
		m.updateInterval = newInterval
		intervalStr := m.formatDuration(newInterval)
		content := fmt.Sprintf("Update Interval Changed\n\nNew interval: %s\n\nPress 'u' for next, 'U' for previous interval.\nThis controls how often the dashboard refreshes.", intervalStr)
		m.PushModal(NewDetailModalWithContent(m, content))
		return m, func() tea.Msg { return UpdateIntervalMsg(newInterval) }
	}

	// Sidebar navigation
	if m.activeSection == SectionSidebar && m.sidebarVisible {
		switch {
		case key.Matches(msg, k.Up):
			cmd := m.moveSidebarCursor(-1)
			return m, cmd
		case key.Matches(msg, k.Down):
			cmd := m.moveSidebarCursor(1)
			return m, cmd
		case key.Matches(msg, k.Right):
			if m.hasDecksSection() {
				m.activeSection = SectionDecks
				m.activeDeckIdx = 0
			}
			return m, nil
		case key.Matches(msg, k.Left):
			return m, nil
		case key.Matches(msg, k.Enter):
			cmd := m.activateSidebarCursor()
			return m, cmd
		}
	}

	// Grid-aware deck navigation (2-column layout)
	if m.activeSection == SectionDecks {
		cols := m.deckColumnCount()
		col := m.activeDeckIdx % cols
		switch {
		case key.Matches(msg, k.Right):
			if col < cols-1 && m.activeDeckIdx+1 < len(m.decks) {
				m.activeDeckIdx++
			} else {
				cmd := m.nextView()
				m.activeDeckIdx = 0
				return m, cmd
			}
			return m, nil
		case key.Matches(msg, k.Left):
			if col > 0 {
				m.activeDeckIdx--
			} else if m.sidebarVisible {
				m.activeSection = SectionSidebar
			} else {
				cmd := m.prevView()
				m.activeDeckIdx = max(0, len(m.decks)-1)
				return m, cmd
			}
			return m, nil
		case key.Matches(msg, k.Down):
			newIdx := m.activeDeckIdx + cols
			if newIdx < len(m.decks) {
				m.activeDeckIdx = newIdx
			}
			return m, nil
		case key.Matches(msg, k.Up):
			newIdx := m.activeDeckIdx - cols
			if newIdx >= 0 {
				m.activeDeckIdx = newIdx
			}
			return m, nil
		}
	}

	// Navigation shortcuts
	switch {
	case key.Matches(msg, k.NextSection):
		m.nextSection()
		return m, nil

	case key.Matches(msg, k.PrevSection):
		m.prevSection()
		return m, nil

	case key.Matches(msg, k.Up):
		if m.activeSection == SectionLogs && len(m.logEntries) <= 0 {
			if m.instructionsScrollOffset > 0 {
				m.instructionsScrollOffset--
			}
			return m, nil
		}
		m.moveSelection(-1)
		return m, nil

	case key.Matches(msg, k.Down):
		if m.activeSection == SectionLogs && len(m.logEntries) <= 0 {
			m.instructionsScrollOffset++
			return m, nil
		}
		m.moveSelection(1)
		return m, nil

	case key.Matches(msg, k.Home):
		if m.activeSection == SectionLogs {
			if len(m.logEntries) <= 0 {
				m.instructionsScrollOffset = 0
				return m, nil
			}
			m.selectedLogIndex = 0
			m.logAutoScroll = false
			return m, nil
		}

	case key.Matches(msg, k.End):
		if m.activeSection == SectionLogs {
			if len(m.logEntries) <= 0 {
				m.instructionsScrollOffset = 9999
				return m, nil
			}
			m.selectedLogIndex = max(0, len(m.logEntries)-1)
			m.logAutoScroll = true
			return m, nil
		}

	case key.Matches(msg, k.PageUp):
		if m.activeSection == SectionLogs {
			if len(m.logEntries) <= 0 {
				m.instructionsScrollOffset = max(0, m.instructionsScrollOffset-5)
				return m, nil
			}
			m.selectedLogIndex = max(0, m.selectedLogIndex-10)
			if m.selectedLogIndex == 0 {
				m.logAutoScroll = false
			}
			return m, nil
		}

	case key.Matches(msg, k.PageDown):
		if m.activeSection == SectionLogs {
			if len(m.logEntries) <= 0 {
				m.instructionsScrollOffset += 5
				return m, nil
			}
			maxIndex := max(0, len(m.logEntries)-1)
			m.selectedLogIndex = min(maxIndex, m.selectedLogIndex+10)
			if m.selectedLogIndex == maxIndex {
				m.logAutoScroll = true
			}
			return m, nil
		}

	case key.Matches(msg, k.Enter):
		return m.showDetails()
	}

	return m, nil
}

// hasLogsSection returns true when the current view shows a log scroll area.
func (m *DashboardModel) hasLogsSection() bool {
	return len(m.decks) == 0
}

// hasDecksSection returns true when the current view shows decks.
func (m *DashboardModel) hasDecksSection() bool {
	return len(m.decks) > 0
}

// nextSection moves to the next section
func (m *DashboardModel) nextSection() {
	if m.activeSection == SectionSidebar {
		if m.hasDecksSection() {
			m.activeSection = SectionDecks
			if m.activeDeckIdx >= len(m.decks) {
				m.activeDeckIdx = max(0, len(m.decks)-1)
			}
		} else if m.hasLogsSection() {
			m.activeSection = SectionLogs
		}
		return
	}

	if m.activeSection == SectionFilter {
		if m.hasDecksSection() {
			m.activeSection = SectionDecks
			if m.activeDeckIdx >= len(m.decks) {
				m.activeDeckIdx = max(0, len(m.decks)-1)
			}
		} else if m.hasLogsSection() {
			m.activeSection = SectionLogs
		}
		return
	}

	if m.activeSection == SectionDecks {
		if m.activeDeckIdx < len(m.decks)-1 {
			m.activeDeckIdx++
		} else if m.sidebarVisible {
			m.activeSection = SectionSidebar
		} else {
			// Wrap to first deck (no logs section in decks-only view).
			m.activeDeckIdx = 0
		}
		return
	}

	// SectionLogs → sidebar (if visible) or stay on logs (no decks in list view).
	if m.sidebarVisible {
		m.activeSection = SectionSidebar
	} else {
		// In list view (no decks), stay on logs.
		m.activeSection = SectionLogs
	}
}

// prevSection moves to the previous section
func (m *DashboardModel) prevSection() {
	if m.activeSection == SectionSidebar {
		if m.hasLogsSection() {
			m.activeSection = SectionLogs
		} else if m.hasDecksSection() {
			m.activeSection = SectionDecks
			m.activeDeckIdx = len(m.decks) - 1
		}
		return
	}

	if m.activeSection == SectionFilter {
		if m.hasLogsSection() {
			m.activeSection = SectionLogs
		} else if m.hasDecksSection() {
			m.activeSection = SectionDecks
			m.activeDeckIdx = len(m.decks) - 1
		}
		return
	}

	if m.activeSection == SectionDecks {
		if m.activeDeckIdx > 0 {
			m.activeDeckIdx--
		} else if m.sidebarVisible {
			m.activeSection = SectionSidebar
		} else {
			// Wrap to last deck (no logs section in decks-only view).
			m.activeDeckIdx = len(m.decks) - 1
		}
		return
	}

	// SectionLogs → sidebar (if visible) or stay on logs (no decks in list view).
	if m.sidebarVisible {
		m.activeSection = SectionSidebar
	} else {
		m.activeSection = SectionLogs
	}
}

// moveSelection moves the selection within the active section
func (m *DashboardModel) moveSelection(delta int) {
	if m.activeSection == SectionLogs {
		maxItems := len(m.logEntries)
		if maxItems == 0 {
			return
		}
		newIndex := m.selectedLogIndex + delta
		if newIndex < 0 {
			newIndex = 0
		} else if newIndex >= maxItems {
			newIndex = maxItems - 1
		}
		m.selectedLogIndex = newIndex
		if m.selectedLogIndex == 0 {
			m.logAutoScroll = false
		} else if m.selectedLogIndex == maxItems-1 {
			m.logAutoScroll = true
		}
		return
	}

	if m.activeSection != SectionDecks || m.activeDeckIdx >= len(m.decks) {
		return
	}

	maxItems := m.decks[m.activeDeckIdx].ItemCount()
	if maxItems == 0 {
		return
	}

	current := m.deckSelIdx[m.activeDeckIdx]
	newIndex := current + delta
	if newIndex < 0 {
		newIndex = 0
	} else if newIndex >= maxItems {
		newIndex = maxItems - 1
	}
	m.deckSelIdx[m.activeDeckIdx] = newIndex
}

// updateSeverityFilterActiveStatus updates whether severity filtering is active
func (m *DashboardModel) updateSeverityFilterActiveStatus() {
	m.severityFilterActive = false
	for _, enabled := range m.severityFilter {
		if !enabled {
			m.severityFilterActive = true
			break
		}
	}
}

// showDetails shows details for the selected item
func (m *DashboardModel) showDetails() (tea.Model, tea.Cmd) {
	if m.activeSection == SectionLogs {
		if m.selectedLogIndex >= 0 && m.selectedLogIndex < len(m.logEntries) {
			entry := m.logEntries[m.selectedLogIndex]
			m.PushModal(NewDetailModal(m, &entry))
		}
		return m, nil
	}

	if m.activeSection == SectionDecks && m.activeDeckIdx < len(m.decks) {
		cmd := m.decks[m.activeDeckIdx].OnSelect(m.viewContext(), m.deckSelIdx[m.activeDeckIdx])
		return m, cmd
	}

	return m, nil
}
