package overlays

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// OptionsAppliedMsg is emitted on Enter.
type OptionsAppliedMsg struct {
	Grouping string
	Sort     string
	Desc     bool
}

// OptionsCancelledMsg is emitted on Esc.
type OptionsCancelledMsg struct{}

// optionsGroupings and optionsSorts define the menu order. Keep these in
// sync with grouping.ByName and grouping.SortByName.
var optionsGroupings = []struct {
	Name  string
	Label string
}{
	{"status", "Status"},
	{"priority", "Priority"},
	{"epic", "Epic"},
}

var optionsSorts = []struct {
	Name  string
	Label string
}{
	{"priority", "Priority"},
	{"updated", "Updated"},
	{"created", "Created"},
	{"key", "Key"},
	{"summary", "Summary"},
}

// Options is the `,` overlay for picking grouping + sort + direction.
type Options struct {
	visible      bool
	section      int    // 0 = grouping, 1 = sort
	cursors      [2]int // cursor per section
	desc         bool
	closeBinding key.Binding
}

// NewOptions builds a hidden Options overlay seeded with the given current
// grouping/sort selections. Cursors snap to the matching rows.
func NewOptions(closeKey key.Binding, currentGrouping, currentSort string, desc bool) Options {
	o := Options{closeBinding: closeKey, desc: desc}
	o.cursors[0] = indexOfGrouping(currentGrouping)
	o.cursors[1] = indexOfSort(currentSort)
	return o
}

func indexOfGrouping(name string) int {
	for i, g := range optionsGroupings {
		if g.Name == name {
			return i
		}
	}
	return 0
}

func indexOfSort(name string) int {
	for i, s := range optionsSorts {
		if s.Name == name {
			return i
		}
	}
	return 0
}

// Visible reports whether the overlay is currently shown.
func (o Options) Visible() bool { return o.visible }

// Grouping returns the name of the row under the grouping cursor.
func (o Options) Grouping() string { return optionsGroupings[o.cursors[0]].Name }

// SortName returns the name of the row under the sort cursor.
func (o Options) SortName() string { return optionsSorts[o.cursors[1]].Name }

// Desc returns the current direction flag.
func (o Options) Desc() bool { return o.desc }

// Show returns a copy of o made visible and re-seeded with the supplied
// current selection.
func (o Options) Show(currentGrouping, currentSort string, desc bool) Options {
	o.visible = true
	o.section = 0
	o.cursors[0] = indexOfGrouping(currentGrouping)
	o.cursors[1] = indexOfSort(currentSort)
	o.desc = desc
	return o
}

// Hide closes the overlay.
func (o Options) Hide() Options {
	o.visible = false
	return o
}

// Update processes key events while visible.
func (o Options) Update(msg tea.Msg) (Options, tea.Cmd) {
	if !o.visible {
		return o, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	if key.Matches(k, o.closeBinding) {
		return o.Hide(), func() tea.Msg { return OptionsCancelledMsg{} }
	}
	switch k.String() {
	case "tab", "shift+tab":
		o.section = (o.section + 1) % 2
		return o, nil
	case "up", "k":
		if o.cursors[o.section] > 0 {
			o.cursors[o.section]--
		}
		return o, nil
	case "down", "j":
		last := o.sectionLen() - 1
		if o.cursors[o.section] < last {
			o.cursors[o.section]++
		}
		return o, nil
	case "d":
		o.desc = !o.desc
		return o, nil
	case "enter":
		applied := OptionsAppliedMsg{
			Grouping: o.Grouping(),
			Sort:     o.SortName(),
			Desc:     o.desc,
		}
		return o.Hide(), func() tea.Msg { return applied }
	}
	return o, nil
}

func (o Options) sectionLen() int {
	if o.section == 0 {
		return len(optionsGroupings)
	}
	return len(optionsSorts)
}

// View renders the overlay; returns "" when hidden.
func (o Options) View(s styles.Styles) string {
	if !o.visible {
		return ""
	}
	arrow := "↑"
	if o.desc {
		arrow = "↓"
	}
	rows := []string{s.OverlayTitle.Render("Options"), ""}

	rows = append(rows, s.OverlayTitle.Render("Grouping"))
	for i, g := range optionsGroupings {
		line := "  " + g.Label
		if i == o.cursors[0] {
			line = "▸ " + g.Label
			if o.section == 0 {
				line = s.ListItemSelected.Render(line)
			} else {
				line = s.ListItem.Render(line)
			}
		} else {
			line = s.ListItem.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "")
	rows = append(rows, s.OverlayTitle.Render(fmt.Sprintf("Sorting (%s)", arrow)))
	for i, st := range optionsSorts {
		line := "  " + st.Label
		if i == o.cursors[1] {
			line = "▸ " + st.Label
			if o.section == 1 {
				line = s.ListItemSelected.Render(line)
			} else {
				line = s.ListItem.Render(line)
			}
		} else {
			line = s.ListItem.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "")
	rows = append(rows, s.Muted.Render("tab section · ↑/↓ move · d direction · enter apply · esc cancel"))
	inner := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return s.OverlayBorder.Render(inner)
}
