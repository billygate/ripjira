package overlays

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestComment_HiddenByDefault(t *testing.T) {
	c := NewComment(closeBinding())
	if c.Visible() {
		t.Error("Comment should start hidden")
	}
	if got := c.View(newStyles(t)); got != "" {
		t.Errorf("hidden View should be empty, got %q", got)
	}
}

func TestComment_ShowAndHide(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	if !c.Visible() {
		t.Fatal("Show did not flip visible")
	}
	if c.IssueKey() != "PROJ-1" {
		t.Errorf("IssueKey = %q, want PROJ-1", c.IssueKey())
	}
	if c.Confirming() {
		t.Error("fresh Show should not be in confirm mode")
	}
	c = c.Hide()
	if c.Visible() {
		t.Fatal("Hide did not flip visible")
	}
	if c.IssueKey() != "" {
		t.Errorf("Hide should clear IssueKey, got %q", c.IssueKey())
	}
}

func TestComment_Show_ResetsBetweenInvocations(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("partial draft")
	c = c.Hide()

	c, _ = c.Show("PROJ-2")
	if c.Value() != "" {
		t.Errorf("after re-Show value = %q, want empty (textarea reset)", c.Value())
	}
	if c.IssueKey() != "PROJ-2" {
		t.Errorf("IssueKey = %q, want PROJ-2", c.IssueKey())
	}
}

func TestComment_TypingForwardsToTextarea(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}})
	if !strings.Contains(c.Value(), "hi") {
		t.Errorf("textarea value = %q, want it to contain 'hi'", c.Value())
	}
}

func TestComment_CtrlSWithEmptyDraftIsNoop(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !updated.Visible() {
		t.Error("Ctrl+S on empty draft should not hide overlay")
	}
	if cmd != nil {
		t.Errorf("Ctrl+S on empty draft returned cmd: %v", cmd)
	}
}

func TestComment_CtrlSPublishesAndHides(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-7")
	c = c.SetValue("looks good to me")

	hidden, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if hidden.Visible() {
		t.Error("Ctrl+S should hide overlay after submit")
	}
	if cmd == nil {
		t.Fatal("Ctrl+S should return a cmd")
	}
	msg := cmd()
	sub, ok := msg.(CommentSubmittedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want CommentSubmittedMsg", msg)
	}
	if sub.IssueKey != "PROJ-7" {
		t.Errorf("submitted IssueKey = %q, want PROJ-7", sub.IssueKey)
	}
	if sub.Body != "looks good to me" {
		t.Errorf("submitted Body = %q, want 'looks good to me'", sub.Body)
	}
}

func TestComment_CtrlSTrimsTrailingWhitespace(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-7")
	c = c.SetValue("body with newline\n\n  ")

	_, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Ctrl+S should return a cmd for non-whitespace draft")
	}
	sub := cmd().(CommentSubmittedMsg)
	if sub.Body != "body with newline" {
		t.Errorf("submitted Body = %q, want trimmed 'body with newline'", sub.Body)
	}
}

func TestComment_EscWithEmptyDraftHides(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	updated, _ := c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("esc on empty draft should hide immediately")
	}
}

func TestComment_EscWithDraftEntersConfirm(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("half-written thought")

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !c.Visible() {
		t.Fatal("esc on non-empty draft should NOT hide overlay yet")
	}
	if !c.Confirming() {
		t.Fatal("esc on non-empty draft should switch to confirm mode")
	}
	view := stripANSI(c.View(newStyles(t)))
	if !strings.Contains(view, "Discard draft?") {
		t.Errorf("confirm view missing prompt, got:\n%s", view)
	}
}

func TestComment_ConfirmYDiscards(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("draft text")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if c.Visible() {
		t.Error("y in confirm should hide overlay")
	}
	if c.Value() != "" {
		t.Errorf("after discard, value = %q, want empty", c.Value())
	}
}

func TestComment_ConfirmEnterDiscards(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("draft text")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c.Visible() {
		t.Error("enter in confirm should discard and hide")
	}
}

func TestComment_ConfirmNReturnsToEditing(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("draft text")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !c.Confirming() {
		t.Fatal("setup: should be in confirm mode")
	}

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !c.Visible() {
		t.Error("n in confirm should keep overlay open")
	}
	if c.Confirming() {
		t.Error("n in confirm should return to editing mode")
	}
	if c.Value() != "draft text" {
		t.Errorf("draft preserved value = %q, want 'draft text'", c.Value())
	}
}

func TestComment_ConfirmEscReturnsToEditing(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-1")
	c = c.SetValue("draft text")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !c.Visible() {
		t.Error("esc in confirm should keep overlay open")
	}
	if c.Confirming() {
		t.Error("esc in confirm should return to editing mode")
	}
}

func TestComment_UpdateNoopWhileHidden(t *testing.T) {
	c := NewComment(closeBinding())
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("hidden overlay should stay hidden")
	}
	if cmd != nil {
		t.Errorf("hidden update returned cmd: %v", cmd)
	}
}

func TestComment_RendersIssueKey(t *testing.T) {
	c, _ := NewComment(closeBinding()).Show("PROJ-42")
	view := stripANSI(c.View(newStyles(t)))
	for _, want := range []string{"PROJ-42", "ctrl+s", "submit"} {
		if !strings.Contains(view, want) {
			t.Errorf("editing view missing %q\n%s", want, view)
		}
	}
}
