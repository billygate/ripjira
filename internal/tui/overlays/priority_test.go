package overlays_test

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/overlays"
)

func newPriority() overlays.Priority {
	return overlays.NewPriority(key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")))
}

func TestPriority_ShowPositionsCursorOnCurrent(t *testing.T) {
	p := newPriority().Show("PROJ-1", "Low")
	// Low is at index 3 in PriorityNames {Highest, High, Medium, Low, Lowest}.
	if p.Cursor() != 3 {
		t.Fatalf("cursor for Low = %d, want 3", p.Cursor())
	}
}

func TestPriority_EnterSelectsAndHides(t *testing.T) {
	p := newPriority().Show("PROJ-1", "Medium")
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if p.Cursor() != 1 {
		t.Fatalf("cursor after k = %d, want 1 (High)", p.Cursor())
	}
	p, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.Visible() {
		t.Fatal("Enter should hide overlay")
	}
	msg, ok := cmd().(overlays.PrioritySelectedMsg)
	if !ok || msg.Name != "High" {
		t.Fatalf("selected = %+v", cmd())
	}
}
