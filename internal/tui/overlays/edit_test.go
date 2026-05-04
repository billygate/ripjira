package overlays_test

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/overlays"
)

func newEdit() overlays.Edit {
	return overlays.NewEdit(key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")))
}

func TestEdit_HiddenAtConstruction(t *testing.T) {
	e := newEdit()
	if e.Visible() {
		t.Fatal("new overlay should start hidden")
	}
}

func TestEdit_ShowPrefillsAndFocuses(t *testing.T) {
	e, _ := newEdit().Show("PROJ-1", overlays.EditSummary, "Old title")
	if !e.Visible() {
		t.Fatal("Show did not flip Visible")
	}
	if e.Value() != "Old title" {
		t.Fatalf("prefill = %q, want %q", e.Value(), "Old title")
	}
	if e.IssueKey() != "PROJ-1" {
		t.Fatalf("issueKey = %q", e.IssueKey())
	}
	if e.Field() != overlays.EditSummary {
		t.Fatalf("field = %v", e.Field())
	}
}

func TestEdit_EnterEmitsSubmittedAndHides(t *testing.T) {
	e, _ := newEdit().Show("PROJ-1", overlays.EditSummary, "")
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("New title")})
	e, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.Visible() {
		t.Fatal("Enter should hide overlay")
	}
	if cmd == nil {
		t.Fatal("Enter should produce a tea.Cmd")
	}
	msg, ok := cmd().(overlays.EditSubmittedMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want EditSubmittedMsg", cmd())
	}
	if msg.IssueKey != "PROJ-1" || msg.Field != overlays.EditSummary || msg.Value != "New title" {
		t.Fatalf("submitted = %+v", msg)
	}
}

func TestEdit_EscClosesWithoutSubmit(t *testing.T) {
	e, _ := newEdit().Show("PROJ-1", overlays.EditPriority, "High")
	e, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.Visible() {
		t.Fatal("Esc should hide overlay")
	}
	if cmd != nil {
		t.Fatalf("Esc should not produce cmd, got %T", cmd())
	}
}
