package overlays

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStructures_EmitsEditScopeOnE(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	p := NewStructures(closeKey).Show([]StructureEntry{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}, "b")
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("expected cmd from 'e'")
	}
	msg := cmd()
	got, ok := msg.(StructureEditScopeMsg)
	if !ok {
		t.Fatalf("want StructureEditScopeMsg, got %T", msg)
	}
	if got.ID != "b" {
		t.Fatalf("want ID b, got %q", got.ID)
	}
}

func TestStructures_EditScopeRejectsReadOnly(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	p := NewStructures(closeKey).Show([]StructureEntry{
		{ID: "a", Name: "A", ReadOnly: true},
	}, "a")
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("expected cmd from 'e' even on read-only (toast)")
	}
	if _, ok := cmd().(StructureEditScopeMsg); ok {
		t.Fatal("read-only structure should not emit StructureEditScopeMsg")
	}
}
