package tui_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// TestCommentOverlay_OpenSubmitSuccess drives the overlay end-to-end through
// the root model. Once the textarea has a body, submitting with Ctrl+S fires
// AddComment; on success the comment is appended to the detail pane and a
// "Comment added" toast is rendered. teatest is used so the asynchronous
// network call goes through the real Bubble Tea runtime.
func TestCommentOverlay_OpenSubmitSuccess(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	model := tui.New(palette, tui.WithLoader(loader))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 40))

	issue := jira.Issue{
		Key:      "PROJ-1",
		Summary:  "Issue under test",
		Status:   jira.Status{Name: "To Do", Category: "new"},
		Priority: jira.Priority{Name: "Medium"},
	}
	loadIssueIntoDetail(t, tm, loader, issue, []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
	})

	// Open comment overlay.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Comment on PROJ-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type body — single message carrying every rune is what the textarea
	// expects when text is pasted in (one KeyMsg per character is also valid,
	// but the batched form is deterministic under teatest).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})

	// Submit with Ctrl+S.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	// Resolve the AddComment call with success.
	loader.addComment("PROJ-1") <- nil

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Comment added")) &&
			bytes.Contains(b, []byte("hello"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	// Verify exactly one AddComment call was made with the right body.
	loader.mu.Lock()
	calls := append([]addCommentCall(nil), loader.addCommentLog...)
	loader.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("AddComment calls = %d, want 1: %#v", len(calls), calls)
	}
	if calls[0].key != "PROJ-1" {
		t.Errorf("AddComment key = %q, want PROJ-1", calls[0].key)
	}
	if !strings.Contains(calls[0].body, "hello") {
		t.Errorf("AddComment body = %q, want it to contain 'hello'", calls[0].body)
	}
}

// TestCommentOverlay_SubmitErrorShowsToast confirms a failing AddComment
// surfaces as an error toast and does NOT append a synthetic comment to
// the detail pane.
func TestCommentOverlay_SubmitErrorShowsToast(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	model := tui.New(palette, tui.WithLoader(loader))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 40))

	issue := jira.Issue{
		Key:     "PROJ-9",
		Summary: "Will fail to comment",
		Status:  jira.Status{Name: "To Do", Category: "new"},
	}
	loadIssueIntoDetail(t, tm, loader, issue, []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
	})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Comment on PROJ-9"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	for _, r := range "nope" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	loader.addComment("PROJ-9") <- errFetch

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Comment failed"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestCommentOverlay_NoIssueIsNoop confirms pressing `c` with no selected
// issue does not open the overlay.
func TestCommentOverlay_NoIssueIsNoop(t *testing.T) {
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	model := tui.New(palette)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m := updated.(tui.Model)
	if m.CommentVisible() {
		t.Error("c with no issue selected should not open overlay")
	}
	if cmd != nil {
		t.Errorf("c with no issue selected returned cmd: %v", cmd)
	}
}
