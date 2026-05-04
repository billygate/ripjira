package overlays_test

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/overlays"
)

func newFavorites() overlays.Favorites {
	return overlays.NewFavorites(key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")))
}

func TestFavorites_HiddenAtConstruction(t *testing.T) {
	if newFavorites().Visible() {
		t.Fatal("new overlay should start hidden")
	}
}

func TestFavorites_EnterAppliesSelected(t *testing.T) {
	f := newFavorites().Show([]overlays.FavoriteEntry{
		{Name: "a", JQL: "x = 1"},
		{Name: "b", JQL: "y = 2"},
	}, "")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if f.Cursor() != 1 {
		t.Fatalf("cursor after j = %d, want 1", f.Cursor())
	}
	f, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Visible() {
		t.Fatal("Enter should hide overlay")
	}
	if cmd == nil {
		t.Fatal("Enter should produce cmd")
	}
	msg, ok := cmd().(overlays.FavoriteAppliedMsg)
	if !ok {
		t.Fatalf("type: %T", cmd())
	}
	if msg.JQL != "y = 2" {
		t.Fatalf("applied JQL = %q", msg.JQL)
	}
}

func TestFavorites_DeleteEmitsMsgAndDropsLocally(t *testing.T) {
	f := newFavorites().Show([]overlays.FavoriteEntry{
		{Name: "a", JQL: "x = 1"},
		{Name: "b", JQL: "y = 2"},
	}, "")
	f, cmd := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd == nil {
		t.Fatal("d should produce cmd")
	}
	msg, ok := cmd().(overlays.FavoriteDeletedMsg)
	if !ok || msg.Name != "a" {
		t.Fatalf("deleted msg: %+v", cmd())
	}
	if !f.Visible() {
		t.Fatal("delete should not hide overlay")
	}
}

func TestFavorites_SaveModeRequiresCurrentJQL(t *testing.T) {
	// Without currentJQL, `s` is a no-op.
	f := newFavorites().Show(nil, "")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if f.Naming() {
		t.Fatal("`s` with empty currentJQL should not enter naming mode")
	}
}

func TestFavorites_SaveFlow(t *testing.T) {
	f := newFavorites().Show(nil, "project = ABC")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !f.Naming() {
		t.Fatal("`s` with currentJQL should enter naming mode")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("My ABC")})
	f, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Visible() {
		t.Fatal("Enter in naming should hide overlay")
	}
	if cmd == nil {
		t.Fatal("Enter should produce cmd")
	}
	msg, ok := cmd().(overlays.FavoriteSavedMsg)
	if !ok || msg.Name != "My ABC" || msg.JQL != "project = ABC" {
		t.Fatalf("saved msg: %+v", cmd())
	}
}
