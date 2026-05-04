package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// LinkSubmittedMsg is published when the user confirms a new link. Type
// is the link relationship name (e.g. "Blocks") and TargetKey is the
// other issue. The owning issue is the one the overlay was opened for —
// it is treated as the inward side, so the link reads "<owning> <type>
// <target>" (e.g. "PROJ-1 Blocks PROJ-2").
type LinkSubmittedMsg struct {
	IssueKey  string
	Type      string
	TargetKey string
}

// Link is the `+` overlay: a single text input that accepts "<type>
// <target-key>" on one line. The format is intentionally compact —
// power-users type "Blocks PROJ-2" faster than tabbing between two
// fields. Validation is minimal: both halves must be non-empty.
type Link struct {
	visible      bool
	issueKey     string
	input        textinput.Model
	errMsg       string
	closeBinding key.Binding
}

// NewLink builds a hidden Link overlay. closeKey hides the overlay
// (typically Esc).
func NewLink(closeKey key.Binding) Link {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "Blocks PROJ-123"
	in.CharLimit = 0
	in.Width = 60
	return Link{input: in, closeBinding: closeKey}
}

// Visible reports whether the overlay is currently shown.
func (l Link) Visible() bool { return l.visible }

// IssueKey returns the issue the overlay was opened for.
func (l Link) IssueKey() string { return l.issueKey }

// Show opens the overlay scoped to issueKey.
func (l Link) Show(issueKey string) (Link, tea.Cmd) {
	l.issueKey = issueKey
	l.visible = true
	l.input.SetValue("")
	l.errMsg = ""
	cmd := l.input.Focus()
	return l, cmd
}

// Hide returns a copy of l with state cleared.
func (l Link) Hide() Link {
	l.visible = false
	l.issueKey = ""
	l.input.SetValue("")
	l.errMsg = ""
	l.input.Blur()
	return l
}

// Update consumes input while the overlay is visible.
func (l Link) Update(msg tea.Msg) (Link, tea.Cmd) {
	if !l.visible {
		return l, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyEnter:
			typ, target, ok := splitLinkInput(l.input.Value())
			if !ok {
				l.errMsg = "format: <type> <KEY> (e.g. \"Blocks PROJ-2\")"
				return l, nil
			}
			issueKey := l.issueKey
			hidden := l.Hide()
			return hidden, func() tea.Msg {
				return LinkSubmittedMsg{IssueKey: issueKey, Type: typ, TargetKey: target}
			}
		case tea.KeyTab, tea.KeyShiftTab:
			return l, nil
		}
		if key.Matches(k, l.closeBinding) {
			return l.Hide(), nil
		}
	}
	var cmd tea.Cmd
	l.input, cmd = l.input.Update(msg)
	return l, cmd
}

// View renders the overlay.
func (l Link) View(s styles.Styles) string {
	if !l.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Link " + l.issueKey + " to…")
	hint := s.Muted.Render(
		"enter submit    " + l.closeBinding.Help().Key + " " + l.closeBinding.Help().Desc +
			"    examples: \"Blocks PROJ-2\" · \"Relates PROJ-3\"",
	)
	parts := []string{title, "", l.input.View()}
	if l.errMsg != "" {
		parts = append(parts, s.Error.Render(l.errMsg))
	}
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// splitLinkInput parses "<type> <KEY>" into its two halves. The split is
// on the LAST whitespace so multi-word link types like "is blocked by"
// would still parse — though canonical Jira names are usually one word
// and the API expects the canonical name, so users typing inward-phrasing
// will get a 400 from the backend (and the error toast).
func splitLinkInput(raw string) (string, string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	idx := strings.LastIndexAny(s, " \t")
	if idx == -1 {
		return "", "", false
	}
	typ := strings.TrimSpace(s[:idx])
	key := strings.TrimSpace(s[idx+1:])
	if typ == "" || key == "" {
		return "", "", false
	}
	return typ, key, true
}
