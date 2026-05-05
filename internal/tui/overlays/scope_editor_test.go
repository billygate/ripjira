package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/structureadapter"
)

func TestScopeEditor_OpensWithRowsAndShowsHint(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey)
	e = e.Show("My Structure", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"Q12026", "Q22026"}},
	}, nil)
	if !e.Visible() {
		t.Fatal("expected visible")
	}
	out := e.View(testStyles(t))
	if !strings.Contains(out, "labels") || !strings.Contains(out, "Q12026") {
		t.Fatalf("expected labels row in view:\n%s", out)
	}
	if !strings.Contains(out, "+ add row") {
		t.Fatalf("expected add-row affordance:\n%s", out)
	}
}

func TestScopeEditor_DeleteRow(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"x"}},
		{Field: "status", Op: structureadapter.OpNot, Values: []string{"Done"}},
	}, nil)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := e.Rows(); len(got) != 1 || got[0].Field != "status" {
		t.Fatalf("after delete cursor=0: want only status, got %#v", got)
	}
}

func TestScopeEditor_SaveEmitsMsg(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"x"}},
	}, nil)
	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	msg := cmd()
	saved, ok := msg.(ScopeSavedMsg)
	if !ok {
		t.Fatalf("want ScopeSavedMsg, got %T", msg)
	}
	if len(saved.Rows) != 1 || saved.Rows[0].Field != "labels" {
		t.Fatalf("unexpected rows: %#v", saved.Rows)
	}
}
