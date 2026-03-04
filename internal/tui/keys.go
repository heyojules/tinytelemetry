package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all dashboard key bindings with built-in help text.
type KeyMap struct {
	// Global
	Quit        key.Binding
	ForceQuit   key.Binding
	Help        key.Binding
	Escape      key.Binding
	ToggleSidebar key.Binding

	// Navigation
	NextSection key.Binding
	PrevSection key.Binding
	Up          key.Binding
	Down        key.Binding
	Home        key.Binding
	End         key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Enter       key.Binding
	Left        key.Binding
	Right       key.Binding

	// Views
	NextView key.Binding
	PrevView key.Binding

	// Actions
	Filter         key.Binding
	Search         key.Binding
	SeverityFilter key.Binding
	LogViewer      key.Binding
	Inspect        key.Binding
	ToggleColumns  key.Binding
	ToggleTimestamp key.Binding
	ResetPatterns  key.Binding
	IntervalUp     key.Binding
	IntervalDown   key.Binding
	Pause          key.Binding
	DeckPause      key.Binding
	SearchModal    key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "h"),
			key.WithHelp("?/h", "help"),
		),
		Escape: key.NewBinding(
			key.WithKeys("escape", "esc"),
			key.WithHelp("esc", "clear/close"),
		),
		ToggleSidebar: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "toggle sidebar"),
		),

		NextSection: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next section"),
		),
		PrevSection: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev section"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "go to top"),
		),
		End: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "go to bottom"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "pagedown"),
			key.WithHelp("pgdn", "page down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "details"),
		),
		Left: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "navigate left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "navigate right"),
		),

		NextView: key.NewBinding(
			key.WithKeys("]"),
			key.WithHelp("]", "next view"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("["),
			key.WithHelp("[", "prev view"),
		),

		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Search: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "search"),
		),
		SeverityFilter: key.NewBinding(
			key.WithKeys("ctrl+f"),
			key.WithHelp("ctrl+f", "severity filter"),
		),
		LogViewer: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "log viewer"),
		),
		Inspect: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "inspect/stats"),
		),
		ToggleColumns: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "toggle columns"),
		),
		ToggleTimestamp: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "toggle timestamp"),
		),
		ResetPatterns: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reset patterns"),
		),
		IntervalUp: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "faster refresh"),
		),
		IntervalDown: key.NewBinding(
			key.WithKeys("U"),
			key.WithHelp("U", "slower refresh"),
		),
		Pause: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "pause/resume"),
		),
		DeckPause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause deck"),
		),
		SearchModal: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "search logs"),
		),
	}
}
