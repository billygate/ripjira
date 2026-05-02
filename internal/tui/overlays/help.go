// Package overlays hosts the modal pop-ups that float above the two-pane
// layout — help cheatsheet, transition picker, comment editor, etc. Every
// overlay is a self-contained Bubble Tea sub-model so app.go only has to
// route key events and render the result.
package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// HelpColumn groups a labelled set of keybindings for the help cheatsheet.
// The bindings come straight from the central Keymap registry so the help
// overlay never drifts from real behaviour.
type HelpColumn struct {
	Title    string
	Bindings []key.Binding
}

// Help is the `?` cheatsheet overlay. It is hidden by default; the root model
// flips it on when the user presses `?` and off when they press the close key.
type Help struct {
	visible      bool
	columns      []HelpColumn
	closeBinding key.Binding
}

// NewHelp constructs a Help overlay rendering the given columns. The close
// binding is the key that hides the overlay (typically `esc`).
func NewHelp(columns []HelpColumn, closeKey key.Binding) Help {
	return Help{columns: columns, closeBinding: closeKey}
}

// Visible reports whether the overlay is currently showing.
func (h Help) Visible() bool { return h.visible }

// Show returns a copy of h with the overlay visible.
func (h Help) Show() Help {
	h.visible = true
	return h
}

// Hide returns a copy of h with the overlay hidden.
func (h Help) Hide() Help {
	h.visible = false
	return h
}

// Update consumes key events while the overlay is visible. It currently only
// handles the configured close key; any other key is swallowed so it cannot
// reach the underlying app while the overlay is up.
func (h Help) Update(msg tea.Msg) (Help, tea.Cmd) {
	if !h.visible {
		return h, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(k, h.closeBinding) {
			return h.Hide(), nil
		}
	}
	return h, nil
}

// View renders the cheatsheet inside a bordered box. Returns "" when hidden
// so the caller can skip layout work.
func (h Help) View(s styles.Styles) string {
	if !h.visible {
		return ""
	}

	rendered := make([]string, 0, len(h.columns))
	for _, col := range h.columns {
		lines := make([]string, 0, len(col.Bindings)+1)
		if col.Title != "" {
			lines = append(lines, s.SectionHeader.Render(col.Title))
		}
		for _, b := range col.Bindings {
			hh := b.Help()
			line := s.Accent.Render(padRight(hh.Key, 10)) + s.Muted.Render(hh.Desc)
			lines = append(lines, line)
		}
		rendered = append(rendered, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	gap := "    "
	body := lipgloss.JoinHorizontal(lipgloss.Top, joinWithGap(rendered, gap)...)
	title := s.OverlayTitle.Render("Keymap")
	closeHint := s.Muted.Render(h.closeBinding.Help().Key + " " + h.closeBinding.Help().Desc)
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", closeHint)
	return s.OverlayBorder.Render(inner)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + spaces(width-len(s))
}

func spaces(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}

func joinWithGap(parts []string, gap string) []string {
	if len(parts) <= 1 {
		return parts
	}
	out := make([]string, 0, len(parts)*2-1)
	for i, p := range parts {
		if i > 0 {
			out = append(out, gap)
		}
		out = append(out, p)
	}
	return out
}
