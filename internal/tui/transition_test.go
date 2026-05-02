package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// loadIssueIntoDetail seeds the app with one issue, navigates to it, and
// resolves the three detail loads. Returns once the detail pane is fully
// populated. Used as the prelude for the transition-overlay tests below so
// each test starts from a known "issue selected, transitions loaded" state.
func loadIssueIntoDetail(t *testing.T, tm *teatest.TestModel, loader *stubLoader,
	issue jira.Issue, transitions []jira.Transition,
) {
	t.Helper()

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Issues"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	loader.listCh <- listResult{issues: []jira.Issue{issue}}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte(issue.Key))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyDown})

	loader.issue(issue.Key) <- issueResult{issue: issue}
	loader.comments(issue.Key) <- commentsResult{}
	loader.transitions(issue.Key) <- transitionsResult{transitions: transitions}

	// Transitions don't render in the detail pane any more. We can't easily
	// observe their arrival from the rendered frame alone, but waiting on the
	// issue summary appearing confirms the detail pane is populated; the
	// transition overlay (`s`) is exercised by other tests.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte(issue.Summary))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestTransitionOverlay_OptimisticUpdate exercises the optimistic-update path
// without going through teatest: it drives Model.Update directly so each
// state transition is observable and free of timing races. The companion
// TestTransitionOverlay_ErrorRollback test below uses teatest to cover the
// end-to-end flow including toast rendering.
func TestTransitionOverlay_OptimisticUpdate(t *testing.T) {
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}

	issue := jira.Issue{
		Key: "PROJ-1", Summary: "First",
		Status:   jira.Status{Name: "To Do", Category: "new"},
		Priority: jira.Priority{Name: "Medium"},
	}
	transition := jira.Transition{
		ID: "31", Name: "Start work",
		To: jira.Status{Name: "In Progress", Category: "indeterminate"},
	}

	// WithInitialIssues seeds the list synchronously; no loader needed for
	// the optimistic-state assertions.
	model := tui.New(palette, tui.WithInitialIssues([]jira.Issue{issue}))
	var mod tea.Model = model
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// First row in the list is the "To Do" group header. ↓ moves onto the
	// PROJ-1 issue row, which fires the detail-pane SetIssue and gives us
	// a current detail.Token() to attach to a TransitionsLoadedMsg.
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyDown})

	m := mod.(tui.Model)
	if got := m.Detail().Issue(); got == nil || got.Key != "PROJ-1" {
		t.Fatalf("detail issue after navigation = %+v, want PROJ-1", got)
	}

	// Inject loaded transitions for the current detail load generation.
	mod, _ = mod.Update(panes.TransitionsLoadedMsg{
		Key: "PROJ-1", Token: m.Detail().Token(),
		Transitions: []jira.Transition{transition},
	})

	// `s` opens the overlay. With 1 transition the cursor is already on it.
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !mod.(tui.Model).TransitionVisible() {
		t.Fatal("overlay should be visible after pressing s")
	}

	// Enter hides the overlay and produces a cmd carrying TransitionSelectedMsg.
	var cmd tea.Cmd
	mod, cmd = mod.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mod.(tui.Model).TransitionVisible() {
		t.Fatal("overlay should be hidden after pressing enter")
	}
	if cmd == nil {
		t.Fatal("enter should return a cmd carrying TransitionSelectedMsg")
	}
	selMsg, ok := cmd().(overlays.TransitionSelectedMsg)
	if !ok {
		t.Fatalf("expected TransitionSelectedMsg, got %T", cmd())
	}
	if selMsg.Transition.ID != "31" {
		t.Errorf("selected transition ID = %q, want 31", selMsg.Transition.ID)
	}

	// Routing TransitionSelectedMsg through Update applies the optimistic
	// status change to BOTH list and detail panes.
	mod, _ = mod.Update(selMsg)
	m = mod.(tui.Model)

	if got := m.Detail().Issue().Status.Name; got != "In Progress" {
		t.Errorf("detail status after optimistic = %q, want In Progress", got)
	}
	listIssues := m.List().Issues()
	if len(listIssues) != 1 || listIssues[0].Status.Name != "In Progress" {
		t.Errorf("list status after optimistic = %+v, want In Progress", listIssues)
	}
}

// TestTransitionOverlay_ErrorRollback opens the overlay, picks a transition,
// observes the optimistic update, then resolves DoTransition with an error
// and verifies the status reverts plus a toast appears.
func TestTransitionOverlay_ErrorRollback(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}

	issue := jira.Issue{
		Key: "PROJ-2", Summary: "Second",
		Status:   jira.Status{Name: "To Do", Category: "new"},
		Priority: jira.Priority{Name: "Low"},
	}
	transitions := []jira.Transition{
		{ID: "41", Name: "Resolve", To: jira.Status{Name: "Done", Category: "done"}},
	}

	model := tui.New(palette, tui.WithLoader(loader))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 80))
	loadIssueIntoDetail(t, tm, loader, issue, transitions)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Transition PROJ-2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// optimistic flip
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Status:    Done"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// fail the network call
	loader.doTransition("41") <- stubErr("nope")

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Transition failed: nope")) &&
			bytes.Contains(b, []byte("Status:    To Do"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestTransitionOverlay_NoIssueSelected_Noop verifies that pressing `s` with
// no issue selected (group header is initial selection) is a quiet no-op.
func TestTransitionOverlay_NoIssueSelected_Noop(t *testing.T) {
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	issue := jira.Issue{Key: "PROJ-9", Summary: "x", Status: jira.Status{Name: "To Do"}}
	model := tui.New(palette, tui.WithInitialIssues([]jira.Issue{issue}))
	var mod tea.Model = model
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Initial selection is the group header (Detail.Issue() == nil).
	if mod.(tui.Model).Detail().Issue() != nil {
		t.Fatal("expected no detail issue at startup (group header selected)")
	}

	mod, cmd := mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if mod.(tui.Model).TransitionVisible() {
		t.Error("overlay should not open when no issue is selected")
	}
	if cmd != nil {
		t.Errorf("s with no issue selected returned cmd: %v", cmd)
	}
}
