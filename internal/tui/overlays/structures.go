package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// StructureEntry is the picker's read-only view of a structure. The overlay
// avoids importing internal/structure so it stays pure-UI testable.
type StructureEntry struct {
	ID       string
	Name     string
	ReadOnly bool
	Builtin  bool
}

// StructureSelectedMsg fires when the user picks an entry. Root models switch
// the active structure for the current project, persist, and re-render.
type StructureSelectedMsg struct {
	ID string
}

// Structures is the `S` overlay: vertical picker of available structures
// (built-ins + user) for the active project. j/k navigate, Enter selects,
// Esc closes.
type Structures struct {
	visible      bool
	entries      []StructureEntry
	cursor       int
	closeBinding key.Binding
}

// NewStructures builds a hidden overlay. closeKey hides it (typically Esc).
func NewStructures(closeKey key.Binding) Structures {
	return Structures{closeBinding: closeKey}
}

// Visible reports whether the overlay is currently shown.
func (p Structures) Visible() bool { return p.visible }

// Cursor returns the highlighted index (0 when empty).
func (p Structures) Cursor() int { return p.cursor }

// Show opens the picker with entries, highlighting selectedID when present.
func (p Structures) Show(entries []StructureEntry, selectedID string) Structures {
	p.entries = append([]StructureEntry(nil), entries...)
	p.cursor = 0
	for i, e := range p.entries {
		if e.ID == selectedID {
			p.cursor = i
			break
		}
	}
	p.visible = true
	return p
}

// Hide clears state and hides the overlay.
func (p Structures) Hide() Structures {
	p.visible = false
	p.entries = nil
	p.cursor = 0
	return p
}

// Update consumes input while the overlay is visible.
func (p Structures) Update(msg tea.Msg) (Structures, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch k.String() {
	case "j", "down":
		if len(p.entries) > 0 && p.cursor < len(p.entries)-1 {
			p.cursor++
		}
		return p, nil
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case "enter":
		if len(p.entries) == 0 {
			return p, nil
		}
		id := p.entries[p.cursor].ID
		hidden := p.Hide()
		return hidden, func() tea.Msg { return StructureSelectedMsg{ID: id} }
	}
	if key.Matches(k, p.closeBinding) {
		return p.Hide(), nil
	}
	return p, nil
}

// View renders the overlay.
func (p Structures) View(s styles.Styles) string {
	if !p.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Structures")
	var rows []string
	if len(p.entries) == 0 {
		rows = append(rows, s.Muted.Render("(no structures available)"))
	} else {
		for i, e := range p.entries {
			label := e.Name
			tags := ""
			if e.Builtin {
				tags += " " + s.Muted.Render("[builtin]")
			}
			if e.ReadOnly {
				tags += " " + s.Muted.Render("[ro]")
			}
			line := label + tags
			if i == p.cursor {
				line = "▶ " + line
			} else {
				line = "  " + line
			}
			rows = append(rows, line)
		}
	}
	hint := s.Muted.Render("enter select    j/k navigate    " +
		p.closeBinding.Help().Key + " " + p.closeBinding.Help().Desc)
	parts := []string{title, ""}
	parts = append(parts, rows...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
