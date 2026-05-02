package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

func sampleTransitions() []jira.Transition {
	return []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress", Category: "indeterminate"}},
		{ID: "21", Name: "Done", To: jira.Status{Name: "Done", Category: "done"}},
	}
}

func TestTransition_HiddenByDefault(t *testing.T) {
	tr := NewTransition(closeBinding())
	if tr.Visible() {
		t.Error("Transition should start hidden")
	}
	if got := tr.View(newStyles(t)); got != "" {
		t.Errorf("hidden View should be empty, got %q", got)
	}
}

func TestTransition_ShowAndHide(t *testing.T) {
	tr := NewTransition(closeBinding())
	tr = tr.Show("PROJ-1", sampleTransitions())
	if !tr.Visible() {
		t.Fatal("Show did not flip visible")
	}
	if tr.IssueKey() != "PROJ-1" {
		t.Errorf("IssueKey = %q, want PROJ-1", tr.IssueKey())
	}
	if got := len(tr.Transitions()); got != 2 {
		t.Errorf("Transitions len = %d, want 2", got)
	}
	if tr.Cursor() != 0 {
		t.Errorf("Cursor = %d, want 0 on fresh Show", tr.Cursor())
	}
	tr = tr.Hide()
	if tr.Visible() {
		t.Fatal("Hide did not flip visible")
	}
}

func TestTransition_Show_DefensiveCopy(t *testing.T) {
	tr := NewTransition(closeBinding())
	src := sampleTransitions()
	tr = tr.Show("PROJ-1", src)
	src[0].Name = "Mutated"
	if got := tr.Transitions()[0].Name; got == "Mutated" {
		t.Error("Show did not defensively copy transitions slice")
	}
}

func TestTransition_CursorMovement(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-1", sampleTransitions())

	// down moves cursor
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tr.Cursor() != 1 {
		t.Errorf("after down cursor = %d, want 1", tr.Cursor())
	}
	// down past end clamps
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tr.Cursor() != 1 {
		t.Errorf("after second down cursor = %d, want 1 (clamped)", tr.Cursor())
	}
	// up moves cursor back
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyUp})
	if tr.Cursor() != 0 {
		t.Errorf("after up cursor = %d, want 0", tr.Cursor())
	}
	// up past start clamps
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyUp})
	if tr.Cursor() != 0 {
		t.Errorf("after second up cursor = %d, want 0 (clamped)", tr.Cursor())
	}
	// j/k mirrors
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if tr.Cursor() != 1 {
		t.Errorf("j did not move cursor down: %d", tr.Cursor())
	}
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if tr.Cursor() != 0 {
		t.Errorf("k did not move cursor up: %d", tr.Cursor())
	}
}

func TestTransition_EnterPublishesSelectedAndHides(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-1", sampleTransitions())
	tr, _ = tr.Update(tea.KeyMsg{Type: tea.KeyDown})
	hidden, cmd := tr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if hidden.Visible() {
		t.Error("enter should hide overlay")
	}
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	msg := cmd()
	sel, ok := msg.(TransitionSelectedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want TransitionSelectedMsg", msg)
	}
	if sel.IssueKey != "PROJ-1" {
		t.Errorf("IssueKey = %q, want PROJ-1", sel.IssueKey)
	}
	if sel.Transition.ID != "21" {
		t.Errorf("selected transition ID = %q, want 21", sel.Transition.ID)
	}
}

func TestTransition_EnterEmptyIsNoop(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-1", nil)
	updated, cmd := tr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.Visible() {
		t.Error("enter on empty overlay should not hide it")
	}
	if cmd != nil {
		t.Errorf("enter on empty overlay returned cmd: %v", cmd)
	}
}

func TestTransition_EscClosesOverlay(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-1", sampleTransitions())
	updated, _ := tr.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("esc should hide overlay")
	}
}

func TestTransition_OtherKeysSwallowed(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-1", sampleTransitions())
	updated, cmd := tr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !updated.Visible() {
		t.Error("non-recognized key should not close overlay")
	}
	if cmd != nil {
		t.Errorf("non-recognized key returned cmd: %v", cmd)
	}
}

func TestTransition_UpdateNoopWhileHidden(t *testing.T) {
	tr := NewTransition(closeBinding())
	updated, cmd := tr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("hidden overlay should stay hidden")
	}
	if cmd != nil {
		t.Errorf("hidden update returned cmd: %v", cmd)
	}
}

func TestTransition_RendersIssueKeyAndTransitions(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-7", sampleTransitions())
	view := stripANSI(tr.View(newStyles(t)))

	for _, want := range []string{"PROJ-7", "Start", "In Progress", "Done", "enter"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q\n%s", want, view)
		}
	}
}

func TestTransition_RendersEmptyState(t *testing.T) {
	tr := NewTransition(closeBinding()).Show("PROJ-7", nil)
	view := stripANSI(tr.View(newStyles(t)))
	if !strings.Contains(view, "no transitions available") {
		t.Errorf("empty overlay missing placeholder:\n%s", view)
	}
}

// closeBinding helper is defined in help_test.go in this package; sampleColumns
// is also there. Use an explicit alias in case help_test.go is renamed.
var _ = key.NewBinding
