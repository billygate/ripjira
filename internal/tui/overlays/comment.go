package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// CommentSubmittedMsg is published when the user confirms a new comment via
// Ctrl+S. The root model handles it by dispatching the network call and
// reacting to its result with a toast + optimistic insertion into the
// detail pane.
type CommentSubmittedMsg struct {
	IssueKey string
	Body     string
}

// commentMode is the overlay's three-state lifecycle: hidden, editing the
// textarea, or asking the user to confirm cancelling a non-empty draft.
type commentMode int

const (
	commentHidden commentMode = iota
	commentEditing
	commentConfirmCancel
)

// Comment is the `c` overlay: a multi-line textarea bound to an issue.
// Submission via Ctrl+S publishes CommentSubmittedMsg; the close key
// discards the draft (with a confirm step when the textarea is non-empty).
type Comment struct {
	mode         commentMode
	issueKey     string
	textarea     textarea.Model
	closeBinding key.Binding
}

// NewComment constructs a hidden Comment overlay. closeKey is the key that
// hides the overlay (typically `esc`).
func NewComment(closeKey key.Binding) Comment {
	ta := textarea.New()
	ta.Placeholder = "Write a comment…"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetWidth(60)
	ta.SetHeight(8)
	return Comment{
		mode:         commentHidden,
		textarea:     ta,
		closeBinding: closeKey,
	}
}

// Visible reports whether the overlay is currently shown.
func (c Comment) Visible() bool { return c.mode != commentHidden }

// Confirming reports whether the overlay is currently asking the user to
// confirm discarding a non-empty draft.
func (c Comment) Confirming() bool { return c.mode == commentConfirmCancel }

// IssueKey returns the issue key the overlay was opened for.
func (c Comment) IssueKey() string { return c.issueKey }

// Value returns the textarea's current value (mostly for tests).
func (c Comment) Value() string { return c.textarea.Value() }

// SetValue replaces the textarea's contents. Useful for tests that want to
// drive the submit path without simulating keystrokes.
func (c Comment) SetValue(s string) Comment {
	c.textarea.SetValue(s)
	return c
}

// Show binds c to the given issue key with an empty textarea + focus. The
// returned cmd starts the cursor blink; callers should pass it to the Bubble
// Tea runtime (or ignore it in tests).
func (c Comment) Show(issueKey string) (Comment, tea.Cmd) {
	c.issueKey = issueKey
	c.mode = commentEditing
	c.textarea.Reset()
	cmd := c.textarea.Focus()
	return c, cmd
}

// Hide returns a copy of c with the overlay closed and state cleared.
func (c Comment) Hide() Comment {
	c.mode = commentHidden
	c.issueKey = ""
	c.textarea.Reset()
	c.textarea.Blur()
	return c
}

// Update consumes input while the overlay is visible. While in editing mode
// most keys are forwarded to the textarea; Ctrl+S submits and the close key
// either hides immediately (empty draft) or steps into a confirm prompt.
// While in confirm mode, y/enter discards and n/close returns to editing.
func (c Comment) Update(msg tea.Msg) (Comment, tea.Cmd) {
	if c.mode == commentHidden {
		return c, nil
	}
	if c.mode == commentConfirmCancel {
		k, ok := msg.(tea.KeyMsg)
		if !ok {
			return c, nil
		}
		switch k.String() {
		case "y", "enter":
			return c.Hide(), nil
		case "n":
			c.mode = commentEditing
			return c, c.textarea.Focus()
		}
		if key.Matches(k, c.closeBinding) {
			c.mode = commentEditing
			return c, c.textarea.Focus()
		}
		return c, nil
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "ctrl+s" {
			body := strings.TrimRight(c.textarea.Value(), " \t\n")
			if body == "" {
				return c, nil
			}
			issueKey := c.issueKey
			hidden := c.Hide()
			return hidden, func() tea.Msg {
				return CommentSubmittedMsg{IssueKey: issueKey, Body: body}
			}
		}
		if key.Matches(k, c.closeBinding) {
			if strings.TrimSpace(c.textarea.Value()) == "" {
				return c.Hide(), nil
			}
			c.mode = commentConfirmCancel
			c.textarea.Blur()
			return c, nil
		}
	}

	var cmd tea.Cmd
	c.textarea, cmd = c.textarea.Update(msg)
	return c, cmd
}

// View renders the overlay. Returns "" when hidden.
func (c Comment) View(s styles.Styles) string {
	if c.mode == commentHidden {
		return ""
	}
	titleText := "Comment"
	if c.issueKey != "" {
		titleText = "Comment on " + c.issueKey
	}
	title := s.OverlayTitle.Render(titleText)
	if c.mode == commentConfirmCancel {
		body := "Discard draft? (y/n)"
		hint := s.Muted.Render("y discard    n keep editing")
		inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint)
		return s.OverlayBorder.Render(inner)
	}
	hint := s.Muted.Render(
		"ctrl+s submit    " + c.closeBinding.Help().Key + " " + c.closeBinding.Help().Desc,
	)
	parts := []string{title, "", c.textarea.View(), "", hint}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}
