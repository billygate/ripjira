package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// FavoriteEntry is the overlay's read-only view of a saved JQL query. The
// overlay does not import internal/state so it can stay testable without
// pulling in the on-disk format.
type FavoriteEntry struct {
	Name string
	JQL  string
}

// FavoriteAppliedMsg is published when the user picks a favorite to run.
// The root model is expected to switch to the Search view and dispatch the
// JQL via its existing search refresh path.
type FavoriteAppliedMsg struct {
	JQL string
}

// FavoriteSavedMsg is published when the user enters save-mode and confirms
// a name. The root model persists it to state.json and re-opens the
// overlay (or closes it; the choice is up to the root).
type FavoriteSavedMsg struct {
	Name string
	JQL  string
}

// FavoriteDeletedMsg is published when the user removes a favorite from
// the picker.
type FavoriteDeletedMsg struct {
	Name string
}

type favoritesMode int

const (
	favoritesHidden favoritesMode = iota
	favoritesPicking
	favoritesNaming
)

// Favorites is the `*` overlay: a vertical picker of saved JQL queries
// with j/k navigation, Enter to apply, `s` to save the current search
// under a new name, and `d` to delete the highlighted entry. The current
// search query is supplied at Show time so save-mode has something to
// persist.
type Favorites struct {
	mode         favoritesMode
	entries      []FavoriteEntry
	cursor       int
	currentJQL   string // populated at Show; empty disables save-mode
	nameInput    textinput.Model
	closeBinding key.Binding
}

// NewFavorites builds a hidden overlay. closeKey hides the overlay
// (typically Esc).
func NewFavorites(closeKey key.Binding) Favorites {
	in := textinput.New()
	in.Prompt = "name> "
	in.Placeholder = "favorite name"
	in.CharLimit = 40
	in.Width = 40
	return Favorites{
		closeBinding: closeKey,
		nameInput:    in,
	}
}

// Visible reports whether the overlay is currently shown.
func (f Favorites) Visible() bool { return f.mode != favoritesHidden }

// Naming reports whether the overlay is currently asking for a name.
func (f Favorites) Naming() bool { return f.mode == favoritesNaming }

// Cursor returns the index of the highlighted entry (0 when empty).
func (f Favorites) Cursor() int { return f.cursor }

// Show opens the picker. entries is the current saved-favorites list;
// currentJQL is the active search (may be ""). When currentJQL is empty
// save-mode is unavailable.
func (f Favorites) Show(entries []FavoriteEntry, currentJQL string) Favorites {
	f.entries = append([]FavoriteEntry(nil), entries...)
	f.currentJQL = currentJQL
	f.cursor = 0
	f.mode = favoritesPicking
	f.nameInput.SetValue("")
	f.nameInput.Blur()
	return f
}

// Hide returns a copy of f with everything cleared.
func (f Favorites) Hide() Favorites {
	f.mode = favoritesHidden
	f.entries = nil
	f.currentJQL = ""
	f.cursor = 0
	f.nameInput.SetValue("")
	f.nameInput.Blur()
	return f
}

// Update consumes input while the overlay is visible.
func (f Favorites) Update(msg tea.Msg) (Favorites, tea.Cmd) {
	if f.mode == favoritesHidden {
		return f, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}

	if f.mode == favoritesNaming {
		switch k.Type {
		case tea.KeyEnter:
			name := strings.TrimSpace(f.nameInput.Value())
			if name == "" {
				return f, nil
			}
			jql := f.currentJQL
			hidden := f.Hide()
			return hidden, func() tea.Msg {
				return FavoriteSavedMsg{Name: name, JQL: jql}
			}
		case tea.KeyEsc:
			f.mode = favoritesPicking
			f.nameInput.Blur()
			return f, nil
		}
		var cmd tea.Cmd
		f.nameInput, cmd = f.nameInput.Update(msg)
		return f, cmd
	}

	switch k.String() {
	case "j", "down":
		if len(f.entries) > 0 && f.cursor < len(f.entries)-1 {
			f.cursor++
		}
		return f, nil
	case "k", "up":
		if f.cursor > 0 {
			f.cursor--
		}
		return f, nil
	case "enter":
		if len(f.entries) == 0 {
			return f, nil
		}
		jql := f.entries[f.cursor].JQL
		hidden := f.Hide()
		return hidden, func() tea.Msg { return FavoriteAppliedMsg{JQL: jql} }
	case "d":
		if len(f.entries) == 0 {
			return f, nil
		}
		name := f.entries[f.cursor].Name
		// Reflect the removal locally for snappy feedback; the root
		// model's persisted-to-disk version will replace this on next
		// open anyway.
		f.entries = append(f.entries[:f.cursor], f.entries[f.cursor+1:]...)
		if f.cursor >= len(f.entries) && f.cursor > 0 {
			f.cursor--
		}
		return f, func() tea.Msg { return FavoriteDeletedMsg{Name: name} }
	case "s":
		if f.currentJQL == "" {
			return f, nil
		}
		f.mode = favoritesNaming
		f.nameInput.SetValue("")
		return f, f.nameInput.Focus()
	}
	if key.Matches(k, f.closeBinding) {
		return f.Hide(), nil
	}
	return f, nil
}

// View renders the overlay.
func (f Favorites) View(s styles.Styles) string {
	if f.mode == favoritesHidden {
		return ""
	}
	title := s.OverlayTitle.Render("Favorites")
	if f.mode == favoritesNaming {
		body := f.nameInput.View()
		hint := s.Muted.Render("enter save    esc back")
		jqlPreview := s.Muted.Render("Saving: " + truncate(f.currentJQL, 60))
		return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left,
			title, "", jqlPreview, "", body, "", hint))
	}
	var rows []string
	if len(f.entries) == 0 {
		rows = append(rows, s.Muted.Render("(no saved favorites — press `s` while a search is active to save one)"))
	} else {
		for i, e := range f.entries {
			line := e.Name + "  " + s.Muted.Render(truncate(e.JQL, 50))
			if i == f.cursor {
				line = "▶ " + line
			} else {
				line = "  " + line
			}
			rows = append(rows, line)
		}
	}
	hint := s.Muted.Render("enter apply    s save current    d delete    " +
		f.closeBinding.Help().Key + " " + f.closeBinding.Help().Desc)
	parts := []string{title, ""}
	parts = append(parts, rows...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
