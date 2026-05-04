package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// DescriptionSubmittedMsg is published when the user confirms a new
// description body via Ctrl+S. The root model translates the value into
// an ADF doc and sends UpdateFields.
type DescriptionSubmittedMsg struct {
	IssueKey string
	Body     string
}

// Description is the `M` overlay: a multi-line textarea pre-filled with
// the current description (markdown form). On submit the body is sent as
// plain-text ADF — markdown structure (headings, lists, code blocks) will
// not be preserved on the server side, since ripjira does not yet
// implement a markdown→ADF round-trip. The overlay shows a warning so
// the user can opt out via Esc instead of clobbering formatting.
type Description struct {
	visible      bool
	issueKey     string
	textarea     textarea.Model
	closeBinding key.Binding
}

// NewDescription builds a hidden overlay.
func NewDescription(closeKey key.Binding) Description {
	ta := textarea.New()
	ta.Placeholder = "Plain text only — markdown will not round-trip"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetWidth(70)
	ta.SetHeight(12)
	return Description{textarea: ta, closeBinding: closeKey}
}

// Visible reports whether the overlay is shown.
func (d Description) Visible() bool { return d.visible }

// IssueKey returns the issue the overlay was opened for.
func (d Description) IssueKey() string { return d.issueKey }

// Value returns the textarea's current value (mostly for tests).
func (d Description) Value() string { return d.textarea.Value() }

// Show opens the overlay scoped to issueKey, prefilled with current.
func (d Description) Show(issueKey, current string) (Description, tea.Cmd) {
	d.issueKey = issueKey
	d.visible = true
	d.textarea.SetValue(current)
	cmd := d.textarea.Focus()
	return d, cmd
}

// Hide returns a copy of d with state cleared.
func (d Description) Hide() Description {
	d.visible = false
	d.issueKey = ""
	d.textarea.Reset()
	d.textarea.Blur()
	return d
}

// Update consumes input while visible.
func (d Description) Update(msg tea.Msg) (Description, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "ctrl+s" {
			body := strings.TrimRight(d.textarea.Value(), " \t\n")
			issueKey := d.issueKey
			hidden := d.Hide()
			return hidden, func() tea.Msg {
				return DescriptionSubmittedMsg{IssueKey: issueKey, Body: body}
			}
		}
		if key.Matches(k, d.closeBinding) {
			return d.Hide(), nil
		}
	}
	var cmd tea.Cmd
	d.textarea, cmd = d.textarea.Update(msg)
	return d, cmd
}

// View renders the overlay.
func (d Description) View(s styles.Styles) string {
	if !d.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Edit description · " + d.issueKey)
	warn := s.Error.Render("⚠ Plain text only — existing markdown structure will be flattened to paragraphs on submit.")
	hint := s.Muted.Render(
		"ctrl+s submit    " + d.closeBinding.Help().Key + " " + d.closeBinding.Help().Desc,
	)
	parts := []string{title, "", warn, "", d.textarea.View(), "", hint}
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
