package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// TopGoEntry is a row in the top-tab "Go to" picker.
type TopGoEntry struct {
	Label string
	// ID is whatever opaque value the root model wants returned (e.g. an int
	// matching panes.TopTabKind). The overlay does not interpret it.
	ID int
}

// TopGoSelectedMsg is published when the user picks an entry.
type TopGoSelectedMsg struct {
	ID int
}

// TopGo is the overlay shown by the "go to top" key. Lists every top-level
// category, supports type-to-filter narrowing.
type TopGo struct {
	visible      bool
	entries      []TopGoEntry
	filtered     []int // indices into entries
	cursor       int
	filter       textinput.Model
	closeBinding key.Binding
}

// NewTopGo builds a hidden overlay. closeKey hides it (typically Esc).
func NewTopGo(closeKey key.Binding) TopGo {
	in := textinput.New()
	in.Prompt = "filter> "
	in.CharLimit = 30
	in.Width = 20
	return TopGo{closeBinding: closeKey, filter: in}
}

// Visible reports whether the overlay is shown.
func (p TopGo) Visible() bool { return p.visible }

// Show opens the overlay, highlighting the entry whose ID matches activeID.
func (p TopGo) Show(entries []TopGoEntry, activeID int) TopGo {
	p.entries = append([]TopGoEntry(nil), entries...)
	p.cursor = 0
	for i, e := range p.entries {
		if e.ID == activeID {
			p.cursor = i
			break
		}
	}
	p.filter.SetValue("")
	p.filter.Focus()
	p.refilter()
	p.visible = true
	return p
}

// Hide clears state.
func (p TopGo) Hide() TopGo {
	p.visible = false
	p.entries = nil
	p.filtered = nil
	p.cursor = 0
	p.filter.SetValue("")
	p.filter.Blur()
	return p
}

// Update consumes input while visible.
func (p TopGo) Update(msg tea.Msg) (TopGo, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "down", "ctrl+n":
			if len(p.filtered) > 0 && p.cursor < len(p.filtered)-1 {
				p.cursor++
			}
			return p, nil
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "enter":
			if len(p.filtered) == 0 {
				return p, nil
			}
			id := p.entries[p.filtered[p.cursor]].ID
			hidden := p.Hide()
			return hidden, func() tea.Msg { return TopGoSelectedMsg{ID: id} }
		}
		if key.Matches(k, p.closeBinding) {
			return p.Hide(), nil
		}
	}
	var cmd tea.Cmd
	prev := p.filter.Value()
	p.filter, cmd = p.filter.Update(msg)
	if p.filter.Value() != prev {
		p.refilter()
	}
	return p, cmd
}

func (p *TopGo) refilter() {
	needle := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.filtered = p.filtered[:0]
	for i, e := range p.entries {
		if needle == "" || strings.Contains(strings.ToLower(e.Label), needle) {
			p.filtered = append(p.filtered, i)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = 0
	}
}

// View renders the overlay.
func (p TopGo) View(s styles.Styles) string {
	if !p.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Go to")
	rows := make([]string, 0, len(p.filtered))
	if len(p.filtered) == 0 {
		rows = append(rows, s.Muted.Render("(no matches)"))
	} else {
		for i, idx := range p.filtered {
			label := p.entries[idx].Label
			if i == p.cursor {
				label = "▶ " + label
			} else {
				label = "  " + label
			}
			rows = append(rows, label)
		}
	}
	hint := s.Muted.Render("enter go   " +
		p.closeBinding.Help().Key + " " + p.closeBinding.Help().Desc)
	parts := []string{title, "", p.filter.View(), ""}
	parts = append(parts, rows...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
