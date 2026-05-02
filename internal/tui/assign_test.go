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
	"github.com/billygate/ripjira/internal/tui/themes"
)

// TestAssignOverlay_OptimisticUpdate drives the assign flow directly via
// Model.Update so each state transition is observable: ResultsMsg fills the
// candidate list, Enter publishes the selection, AssignSelected applies the
// optimistic assignee, and the deferred AssignIssue call is dispatched.
func TestAssignOverlay_OptimisticUpdate(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}

	original := jira.User{AccountID: "old-1", DisplayName: "Old Owner"}
	issue := jira.Issue{
		Key: "PROJ-1", Summary: "First",
		Status:   jira.Status{Name: "To Do", Category: "new"},
		Priority: jira.Priority{Name: "Medium"},
		Assignee: &original,
	}

	model := tui.New(palette,
		tui.WithLoader(loader),
		tui.WithInitialIssues([]jira.Issue{issue}),
		tui.WithAssignDebounce(0),
	)
	var mod tea.Model = model
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// Move from group header to issue row so detail.Issue() is non-nil.
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyDown})

	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !mod.(tui.Model).AssignVisible() {
		t.Fatal("assign overlay should be visible after pressing a")
	}

	// Type "an" — debounce=0 so the overlay schedules a search request the
	// app would normally dispatch. Tests skip the dispatch round-trip and
	// inject the AssignResultsMsg directly with the current token.
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	a := mod.(tui.Model).Assign()
	mod, _ = mod.Update(overlays.AssignResultsMsg{
		Query: "an", Token: a.Token(),
		Users: []jira.User{{AccountID: "u-new", DisplayName: "New Owner"}},
	})

	// Enter publishes a selection; route the message through Update so the
	// optimistic assignee shows in the detail pane.
	var cmd tea.Cmd
	mod, cmd = mod.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return a cmd carrying AssignSelectedMsg")
	}
	sel, ok := cmd().(overlays.AssignSelectedMsg)
	if !ok {
		t.Fatalf("expected AssignSelectedMsg, got %T", cmd())
	}
	if sel.User.AccountID != "u-new" {
		t.Errorf("selected AccountID = %q, want u-new", sel.User.AccountID)
	}

	mod, _ = mod.Update(sel)
	m := mod.(tui.Model)

	if got := m.Detail().Issue().Assignee; got == nil || got.AccountID != "u-new" {
		t.Errorf("detail assignee after optimistic = %+v, want u-new", got)
	}
	listIssues := m.List().Issues()
	if listIssues[0].Assignee == nil || listIssues[0].Assignee.AccountID != "u-new" {
		t.Errorf("list assignee after optimistic = %+v, want u-new", listIssues[0].Assignee)
	}
}

// TestAssignOverlay_ErrorRollback runs the full flow under teatest, including
// the error path: AssignIssue resolves with an error, the optimistic
// assignee is reverted, and the failure surfaces as a toast.
func TestAssignOverlay_ErrorRollback(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}

	original := jira.User{AccountID: "old-1", DisplayName: "Old Owner"}
	issue := jira.Issue{
		Key: "PROJ-2", Summary: "Second",
		Status:   jira.Status{Name: "To Do", Category: "new"},
		Priority: jira.Priority{Name: "Low"},
		Assignee: &original,
	}

	model := tui.New(palette,
		tui.WithLoader(loader),
		tui.WithAssignDebounce(0),
	)
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(140, 80))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Issues"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	loader.listCh <- listResult{issues: []jira.Issue{issue}}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("PROJ-2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	loader.issue("PROJ-2") <- issueResult{issue: issue}
	loader.comments("PROJ-2") <- commentsResult{}
	loader.transitions("PROJ-2") <- transitionsResult{}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Old Owner"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Assign PROJ-2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type "ne" → debounce=0 fires SearchUsers("ne") immediately.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	loader.searchUsers("ne") <- searchUsersResult{users: []jira.User{
		{AccountID: "u-new", DisplayName: "New Owner", Email: "n@example.com"},
	}}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("New Owner"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Optimistic update: detail now shows New Owner.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Assignee:  New Owner"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Fail the assign call.
	loader.assign("PROJ-2") <- stubErr("forbidden")

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Assign failed: forbidden")) &&
			bytes.Contains(b, []byte("Assignee:  Old Owner"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	if len(loader.assignLog) != 1 || loader.assignLog[0].accountID != "u-new" {
		t.Errorf("AssignIssue call log = %+v, want one call with u-new", loader.assignLog)
	}
}

// TestAssignOverlay_NoIssueSelected_Noop verifies that pressing `a` with no
// issue selected (group header is initial selection) is a quiet no-op.
func TestAssignOverlay_NoIssueSelected_Noop(t *testing.T) {
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	issue := jira.Issue{Key: "PROJ-9", Summary: "x", Status: jira.Status{Name: "To Do"}}
	model := tui.New(palette, tui.WithInitialIssues([]jira.Issue{issue}))
	var mod tea.Model = model
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if mod.(tui.Model).Detail().Issue() != nil {
		t.Fatal("expected no detail issue at startup (group header selected)")
	}

	mod, cmd := mod.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if mod.(tui.Model).AssignVisible() {
		t.Error("overlay should not open when no issue is selected")
	}
	if cmd != nil {
		t.Errorf("a with no issue selected returned cmd: %v", cmd)
	}
}
