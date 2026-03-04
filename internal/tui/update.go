package tui

import (
	"time"

	"github.com/tinytelemetry/lotus/internal/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickDataLoadedMsg struct {
	totalCount      int64
	hasTotalCount   bool
	appList         []string
	hasAppList      bool
	logEntries      []model.LogRecord
	hasLogEntries   bool
	drain3Records   []model.LogRecord
	drain3Processed int
	hasDrain3       bool
	lastError       string // first DB error encountered during this tick
}

// Update handles messages
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewStyle = lipgloss.NewStyle().
			MaxHeight(m.height).
			MaxWidth(m.width)
		m.initializeDecks()
		_, _, logsH := m.layoutHeights()
		m.clampInstructionsScroll(logsH)

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case ActionMsg:
		switch msg.Action {
		case ActionSetSearchTerm:
			if term, ok := msg.Payload.(string); ok {
				m.searchTerm = term
			}
		case ActionPushModal:
			if modal, ok := msg.Payload.(Modal); ok {
				m.PushModal(modal)
			}
		}
		return m, nil

	case searchDebounceMsg, searchResultsMsg:
		if modal := m.TopModal(); modal != nil {
			pop, cmd := modal.Update(msg)
			if pop {
				m.PopModal()
			}
			return m, cmd
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouseEvent(msg)

	case ManualResetMsg:
		// Reset drain3 pattern extraction state
		if m.drain3Manager != nil {
			m.drain3Manager.Reset()
			m.drain3LastProcessed = 0
		}
		for _, drain3Instance := range m.drain3BySeverity {
			if drain3Instance != nil {
				drain3Instance.Reset()
			}
		}
		return m, nil

	case TickMsg:
		// Freeze refresh while user is reading logs (or manually paused)
		// so selection/scroll position remains stable.
		if m.liveUpdatesPaused() {
			return m, tea.Tick(m.updateInterval, func(t time.Time) tea.Msg {
				return TickMsg(t)
			})
		}

		if m.tickInFlight {
			return m, tea.Tick(m.updateInterval, func(t time.Time) tea.Msg {
				return TickMsg(t)
			})
		}
		m.tickInFlight = true

		opts := m.queryOpts()
		severityLevels := m.activeSeverityLevels()
		var messagePattern string
		if m.filterRegex != nil {
			messagePattern = m.filterRegex.String()
		}
		logLimit := m.visibleLogLines()
		drainFrom := m.drain3LastProcessed

		// Continue periodic ticks
		return m, tea.Batch(
			m.fetchTickDataCmd(opts, severityLevels, messagePattern, logLimit, drainFrom),
			tea.Tick(m.updateInterval, func(t time.Time) tea.Msg {
				return TickMsg(t)
			}),
		)

	case tickDataLoadedMsg:
		m.tickInFlight = false
		m.applyTickData(msg)
		_, _, logsH := m.layoutHeights()
		m.clampInstructionsScroll(logsH)
		// Visibility-aware refresh: only refresh modal data when it's visible.
		if modal := m.TopModal(); modal != nil {
			if r, ok := modal.(Refreshable); ok {
				r.Refresh()
			}
		}
		return m, nil

	case DeckTickMsg:
		return m.handleDeckTick(msg)

	case DeckDataMsg:
		return m.handleDeckData(msg)

	case DeckPauseMsg:
		return m.handleDeckPause(msg)

	case ViewFlashMsg:
		m.viewFlashTitle = ""
		return m, nil

	case SpinnerTickMsg:
		return m.handleSpinnerTick()

	}

	return m, tea.Batch(cmds...)
}

// handleMouseEvent processes mouse interactions
func (m *DashboardModel) handleMouseEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Modal on stack gets the mouse event first.
	if modal := m.TopModal(); modal != nil {
		pop, cmd := modal.Update(msg)
		if pop {
			m.PopModal()
		}
		return m, cmd
	}

	// Inline handlers (filter/search).
	for _, entry := range m.inlineHandlers {
		if entry.isActive(m) {
			handled, cmd := entry.handler.HandleMouse(m, msg)
			if handled {
				return m, cmd
			}
			break
		}
	}

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonLeft:
			// Handle left mouse button clicks to switch sections
			return m.handleMouseClick(msg.X, msg.Y)

		case tea.MouseButtonWheelUp:
			// Scroll wheel up = move selection up (like up arrow), or down if reversed
			if m.reverseScrollWheel {
				m.moveSelection(-1)
			} else {
				m.moveSelection(1)
			}
			return m, nil

		case tea.MouseButtonWheelDown:
			// Scroll wheel down = move selection down (like down arrow), or up if reversed
			if m.reverseScrollWheel {
				m.moveSelection(1)
			} else {
				m.moveSelection(-1)
			}
			return m, nil
		}
	}

	return m, nil
}

// handleMouseClick processes mouse clicks to switch between sections
func (m *DashboardModel) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	if m.width <= 0 || m.height <= 0 {
		return m, nil
	}

	if m.sidebarVisible {
		if x < sidebarWidth {
			m.activeSection = SectionSidebar

			// Sidebar rows are mixed views + apps; resolve click via rendered rows.
			if idx, ok := m.sidebarCursorAtMouseRow(y); ok {
				m.sidebarCursor = idx
				cmd := m.activateSidebarCursor()
				return m, cmd
			}
			return m, nil
		}
		x -= sidebarWidth
	}

	contentWidth := m.width
	if m.sidebarVisible {
		contentWidth -= sidebarWidth
	}

	decksHeight, filterHeight, _ := m.layoutHeights()

	if y < decksHeight {
		if idx, ok := m.deckAt(contentWidth, decksHeight, x, y); ok {
			m.activeSection = SectionDecks
			m.activeDeckIdx = idx
		}
		return m, nil
	}

	if filterHeight > 0 && y < decksHeight+filterHeight {
		m.activeSection = SectionFilter
		return m, nil
	}

	m.activeSection = SectionLogs

	return m, nil
}

func minuteCountsToSeverity(rows []model.MinuteCounts) []SeverityCounts {
	history := make([]SeverityCounts, 0, len(rows))
	for _, row := range rows {
		history = append(history, SeverityCounts{
			Trace: int(row.Trace),
			Debug: int(row.Debug),
			Info:  int(row.Info),
			Warn:  int(row.Warn),
			Error: int(row.Error),
			Fatal: int(row.Fatal),
			Total: int(row.Total),
		})
	}
	return history
}

// activeSeverityLevels returns the list of enabled severity levels when
// severity filtering is active, or nil when all levels are shown.
func (m *DashboardModel) activeSeverityLevels() []string {
	if !m.severityFilterActive {
		return nil
	}
	var levels []string
	for level, enabled := range m.severityFilter {
		if enabled {
			levels = append(levels, level)
		}
	}
	return levels
}

// visibleLogLines returns how many log lines fit on screen given the current
// terminal dimensions, using the shared layoutHeights calculation.
// When the List view (ListDeck) is active, the deck area height is used
// because the log scroll is rendered inside the deck grid.
func (m *DashboardModel) visibleLogLines() int {
	decksH, _, logsH := m.layoutHeights()
	if logsH > 0 {
		return logsH
	}
	// ListDeck: logs are rendered inside the decks area.
	if decksH > 0 {
		return decksH
	}
	return 0
}

func (m *DashboardModel) fetchTickDataCmd(opts model.QueryOpts, severityLevels []string, messagePattern string, logLimit int, drainFrom int) tea.Cmd {
	store := m.store
	if store == nil {
		return func() tea.Msg { return tickDataLoadedMsg{} }
	}

	severityCopy := append([]string(nil), severityLevels...)

	return func() tea.Msg {
		msg := tickDataLoadedMsg{}

		// collectErr records the first DB error encountered.
		collectErr := func(err error) {
			if err != nil && msg.lastError == "" {
				msg.lastError = err.Error()
			}
		}

		if v, err := store.TotalLogCount(opts); err == nil {
			msg.totalCount = v
			msg.hasTotalCount = true
		} else {
			collectErr(err)
		}

		if apps, err := store.ListApps(); err == nil {
			msg.appList = apps
			msg.hasAppList = true
		} else {
			collectErr(err)
		}

		if msg.hasTotalCount && msg.totalCount > int64(drainFrom) {
			newCount := int(msg.totalCount) - drainFrom
			if newCount > 5000 {
				newCount = 5000
			}
			if newCount > 0 {
				if records, err := store.RecentLogsFiltered(newCount, opts.App, nil, ""); err == nil {
					startIdx := 0
					if len(records) > newCount {
						startIdx = len(records) - newCount
					}
					msg.drain3Records = append([]model.LogRecord(nil), records[startIdx:]...)
					msg.drain3Processed = int(msg.totalCount)
					msg.hasDrain3 = true
				} else {
					collectErr(err)
				}
			}
		}

		if len(severityCopy) == 0 && severityLevels != nil {
			msg.logEntries = []model.LogRecord{}
			msg.hasLogEntries = true
		} else if records, err := store.RecentLogsFiltered(logLimit, opts.App, severityCopy, messagePattern); err == nil {
			msg.logEntries = records
			msg.hasLogEntries = true
		} else {
			collectErr(err)
		}

		return msg
	}
}

func (m *DashboardModel) applyTickData(msg tickDataLoadedMsg) {
	// Surface DB errors to the status line; auto-clears on next successful tick.
	if msg.lastError != "" {
		m.lastError = msg.lastError
		m.lastErrorAt = time.Now()
		m.lastTickOK = false
		m.consecutiveErrors++
	} else {
		m.lastError = ""
		m.lastTickOK = true
		m.lastTickAt = time.Now()
		m.consecutiveErrors = 0
	}

	if msg.hasTotalCount {
		m.updateProcessingRateStats(msg.totalCount)
	}

	if msg.hasAppList {
		m.appList = msg.appList
		m.clampSidebarCursor()
	}

	if msg.hasDrain3 {
		m.applyDrain3Records(msg.drain3Records, msg.drain3Processed)
	}

	if msg.hasLogEntries && !m.liveUpdatesPaused() {
		m.applyLogEntries(msg.logEntries)
	}
}

func (m *DashboardModel) applyDrain3Records(records []model.LogRecord, processed int) {
	if m.drain3Manager == nil {
		return
	}

	for _, r := range records {
		if r.Message == "" {
			continue
		}
		m.drain3Manager.AddLogMessage(r.Message)
		if drain3Instance, exists := m.drain3BySeverity[r.Level]; exists && drain3Instance != nil {
			drain3Instance.AddLogMessage(r.Message)
		}
	}
	m.drain3LastProcessed = processed
}

func (m *DashboardModel) applyLogEntries(records []model.LogRecord) {
	m.logEntries = records

	// Clamp selection to bounds; auto-scroll pins to the latest entry.
	if m.logAutoScroll {
		m.selectedLogIndex = max(0, len(m.logEntries)-1)
	} else if m.selectedLogIndex >= len(m.logEntries) {
		m.selectedLogIndex = max(0, len(m.logEntries)-1)
	}
}

// initializeDecks sets up the charts based on current dimensions
func (m *DashboardModel) initializeDecks() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

}

// updateProcessingRateStats computes processing rate from DuckDB count deltas between ticks.
// totalCount is the pre-fetched TotalLogCount shared across the tick.
func (m *DashboardModel) updateProcessingRateStats(totalCount int64) {
	now := time.Now()

	currentTotal := totalCount
	m.stats.TotalLogsEver = int(currentTotal)

	// Compute delta since last tick
	delta := int(currentTotal) - m.stats.lastTickCount
	if delta < 0 {
		delta = 0
	}

	// Compute elapsed time since last tick
	elapsed := now.Sub(m.stats.lastTickTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	// Compute instantaneous rate (logs per second over this tick interval)
	rate := float64(delta) / elapsed
	if rate > m.stats.PeakLogsPerSec {
		m.stats.PeakLogsPerSec = rate
	}

	// Add to sliding window (one entry per tick)
	m.stats.RecentCounts = append(m.stats.RecentCounts, delta)
	m.stats.RecentTimes = append(m.stats.RecentTimes, now)

	// Keep only entries from the last 10 seconds
	cutoffTime := now.Add(-10 * time.Second)
	for len(m.stats.RecentTimes) > 0 && m.stats.RecentTimes[0].Before(cutoffTime) {
		m.stats.RecentCounts = m.stats.RecentCounts[1:]
		m.stats.RecentTimes = m.stats.RecentTimes[1:]
	}

	// Track for next tick
	m.stats.lastTickCount = int(currentTotal)
	m.stats.lastTickTime = now
	m.stats.LogsThisSecond = delta // Used by formatCurrentRate
}

// getDisplayTimestamp returns the appropriate timestamp based on useLogTime setting
// Falls back to receive time (Timestamp) if OrigTimestamp is not available
func (m *DashboardModel) getDisplayTimestamp(entry model.LogRecord) time.Time {
	if m.useLogTime && !entry.OrigTimestamp.IsZero() {
		return entry.OrigTimestamp
	}
	return entry.Timestamp
}

// handleDeckTick processes a per-deck tick.
func (m *DashboardModel) handleDeckTick(msg DeckTickMsg) (tea.Model, tea.Cmd) {
	state, ok := m.deckStates[msg.DeckTypeID]
	if !ok {
		return m, nil
	}

	reschedule := tea.Tick(state.Interval, func(t time.Time) tea.Msg {
		return DeckTickMsg{DeckTypeID: msg.DeckTypeID, At: t}
	})

	// Auto-pause: skip fetch when the user is focused on this deck.
	focusedOnThis := m.activeSection == SectionDecks &&
		m.activeDeckIdx < len(m.decks) &&
		func() bool {
			if tp, ok := m.decks[m.activeDeckIdx].(TickableDeck); ok {
				return tp.TypeID() == msg.DeckTypeID
			}
			return false
		}()

	if state.Paused || state.FetchInFlight || focusedOnThis {
		return m, reschedule
	}

	// Find one TickableDeck instance with this TypeID and issue its FetchCmd.
	var fetchCmd tea.Cmd
	for _, vw := range m.allViews() {
		for _, dk := range vw.Decks {
			if tp, ok := dk.(TickableDeck); ok && tp.TypeID() == msg.DeckTypeID {
				state.FetchInFlight = true
				fetchCmd = tp.FetchCmd(m.store, m.queryOpts())
				break
			}
		}
		if fetchCmd != nil {
			break
		}
	}

	if fetchCmd == nil {
		return m, reschedule
	}

	// Start spinner animation while fetch is in flight.
	spinnerCmd := m.startSpinnerIfNeeded()
	return m, tea.Batch(fetchCmd, reschedule, spinnerCmd)
}

// handleDeckData processes fetched deck data and distributes to all instances.
func (m *DashboardModel) handleDeckData(msg DeckDataMsg) (tea.Model, tea.Cmd) {
	state, ok := m.deckStates[msg.DeckTypeID]
	if ok {
		state.FetchInFlight = false
		if msg.Err != nil {
			state.LastError = msg.Err.Error()
			state.LastErrorAt = time.Now()
			state.LastTickOK = false
			state.ConsecutiveErrs++
		} else {
			state.LastError = ""
			state.LastTickOK = true
			state.LastTickAt = time.Now()
			state.ConsecutiveErrs = 0
		}
	}

	// Apply data to ALL instances of this TypeID across ALL pages.
	for _, vw := range m.allViews() {
		for _, dk := range vw.Decks {
			if tp, ok := dk.(TickableDeck); ok && tp.TypeID() == msg.DeckTypeID {
				tp.ApplyData(msg.Data, msg.Err)
			}
		}
	}

	return m, nil
}

// handleDeckPause toggles pause for a deck type.
func (m *DashboardModel) handleDeckPause(msg DeckPauseMsg) (tea.Model, tea.Cmd) {
	state, ok := m.deckStates[msg.DeckTypeID]
	if ok {
		state.Paused = !state.Paused
	}
	return m, nil
}

// registerDeckTicks scans all views for TickableDeck instances and registers
// their TypeIDs in deckStates (deduped). Returns Init cmds to start ticking.
func (m *DashboardModel) registerDeckTicks() []tea.Cmd {
	seen := make(map[string]bool)
	var cmds []tea.Cmd

	for _, vw := range m.allViews() {
		for _, dk := range vw.Decks {
			tp, ok := dk.(TickableDeck)
			if !ok {
				continue
			}
			tid := tp.TypeID()
			if seen[tid] {
				continue
			}
			seen[tid] = true

			if _, exists := m.deckStates[tid]; !exists {
				m.deckStates[tid] = &DeckTypeState{
					TypeID:     tid,
					Interval:   tp.DefaultInterval(),
					LastTickOK: true,
					LastTickAt: time.Now(),
				}
			}

			interval := m.deckStates[tid].Interval
			cmds = append(cmds, tea.Tick(interval, func(t time.Time) tea.Msg {
				return DeckTickMsg{DeckTypeID: tid, At: t}
			}))
		}
	}

	return cmds
}
