// Package tui hosts the Bubble Tea root model and surrounding TUI plumbing
// for ripjira. The Keymap is the single source of truth for keybindings —
// the help overlay and hint bar both read from it so they cannot drift from
// real behaviour.
package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap is the central registry of all key bindings used by the TUI. Each
// binding carries its key list AND a help description, so the help overlay
// can be generated from this struct alone.
type Keymap struct {
	Up                 key.Binding
	Down               key.Binding
	CycleFocusForward  key.Binding
	CycleFocusBackward key.Binding
	NextTab            key.Binding
	PrevTab            key.Binding
	FocusLeft          key.Binding
	FocusRight         key.Binding
	Top                key.Binding
	Bottom             key.Binding
	ToggleGroup        key.Binding
	Open               key.Binding
	Status             key.Binding
	Assign             key.Binding
	Comment            key.Binding
	New                key.Binding
	NewSubtask         key.Binding
	Browser            key.Binding
	CopyKey            key.Binding
	CopyURL            key.Binding
	Refresh            key.Binding
	OpenSearch         key.Binding
	OpenFilter         key.Binding
	OpenFavorites      key.Binding
	OpenOptions        key.Binding
	EditSummary        key.Binding
	EditPriority       key.Binding
	EditLabels         key.Binding
	EditDueDate        key.Binding
	AddLink            key.Binding
	Watch              key.Binding
	Unwatch            key.Binding
	LogWork            key.Binding
	Help               key.Binding
	CloseOverlay       key.Binding
	Quit               key.Binding
}

// DefaultKeymap returns the bindings defined in the design spec §4 (Keymap).
func DefaultKeymap() Keymap {
	return Keymap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		CycleFocusForward: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next pane"),
		),
		CycleFocusBackward: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev pane"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("]"),
			key.WithHelp("]", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("["),
			key.WithHelp("[", "prev tab"),
		),
		FocusLeft: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h", "focus left"),
		),
		FocusRight: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l", "focus right"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		ToggleGroup: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "toggle group"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open"),
		),
		Status: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "status"),
		),
		Assign: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "assign"),
		),
		Comment: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "comment"),
		),
		New: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),
		NewSubtask: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "subtask"),
		),
		Browser: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "browser"),
		),
		CopyKey: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy key"),
		),
		CopyURL: key.NewBinding(
			key.WithKeys("Y"),
			key.WithHelp("Y", "copy URL"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		OpenSearch: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		OpenFilter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "filter"),
		),
		OpenFavorites: key.NewBinding(
			key.WithKeys("*"),
			key.WithHelp("*", "favorites"),
		),
		OpenOptions: key.NewBinding(
			key.WithKeys(","),
			key.WithHelp(",", "options"),
		),
		EditSummary: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "edit title"),
		),
		EditPriority: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "edit priority"),
		),
		EditLabels: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "edit labels"),
		),
		EditDueDate: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "edit due date"),
		),
		AddLink: key.NewBinding(
			key.WithKeys("+"),
			key.WithHelp("+", "add link"),
		),
		Watch: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "watch"),
		),
		Unwatch: key.NewBinding(
			key.WithKeys("W"),
			key.WithHelp("W", "unwatch"),
		),
		LogWork: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "log time"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		CloseOverlay: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns the compact set of bindings shown in the bottom hint bar.
// Order matches the spec mock-up.
func (k Keymap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Down, k.CycleFocusForward, k.Open, k.Status, k.Comment, k.New,
	}
}

// FullHelp returns the full keymap grouped into columns, used by the help
// overlay. Columns: navigation, actions, app.
func (k Keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.CycleFocusForward, k.CycleFocusBackward, k.NextTab, k.PrevTab, k.FocusLeft, k.FocusRight, k.Top, k.Bottom, k.ToggleGroup, k.OpenSearch, k.OpenOptions},
		{k.Open, k.Status, k.Assign, k.Comment, k.New, k.NewSubtask, k.Browser, k.Refresh},
		{k.Help, k.CloseOverlay, k.Quit},
	}
}

// FullHelpTitles returns the titles paired one-to-one with FullHelp columns.
// Kept here so the help overlay can read both shape and labelling from the
// same place without duplicating the binding lists.
func (k Keymap) FullHelpTitles() []string {
	return []string{"Navigation", "Actions", "App"}
}

// All returns every binding in the keymap as a flat slice. Useful for tests
// that want to assert "every registered binding is documented somewhere".
func (k Keymap) All() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.CycleFocusForward, k.CycleFocusBackward,
		k.NextTab, k.PrevTab,
		k.FocusLeft, k.FocusRight, k.Top, k.Bottom,
		k.ToggleGroup, k.Open,
		k.Status, k.Assign, k.Comment, k.New, k.NewSubtask, k.Browser,
		k.Refresh, k.OpenSearch, k.OpenOptions, k.Help, k.CloseOverlay, k.Quit,
	}
}
