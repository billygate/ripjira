package overlays

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// GoToIssueMsg is emitted by Goto when the user submits a syntactically
// valid issue key. The root model handles it by loading the issue and
// switching to the Recent sub-view.
type GoToIssueMsg struct{ Key string }

// GoToInvalidMsg is emitted by Goto when the user submits a malformed
// key. The root model surfaces it as a toast; the overlay stays open
// so the user can correct the input.
type GoToInvalidMsg struct{ Input string }

// Goto is the `o` overlay: a single-line text input that loads any
// issue by key. It carries no network logic — it validates format,
// normalises whitespace/case, and publishes messages.
type Goto struct {
	visible      bool
	input        textinput.Model
	submitBinds  key.Binding
	closeBinding key.Binding
}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)

func NewGoto(submitKey, closeKey key.Binding) Goto {
	ti := textinput.New()
	ti.Placeholder = "ABC-123"
	ti.Prompt = ""
	ti.CharLimit = 32
	ti.Width = 24
	return Goto{
		input:        ti,
		submitBinds:  submitKey,
		closeBinding: closeKey,
	}
}

func (g Goto) Visible() bool { return g.visible }
func (g Goto) Value() string { return g.input.Value() }

func (g Goto) Show() (Goto, tea.Cmd) {
	g.visible = true
	g.input.SetValue("")
	cmd := g.input.Focus()
	return g, cmd
}

func (g Goto) Hide() Goto {
	g.visible = false
	g.input.Blur()
	g.input.SetValue("")
	return g
}

func (g Goto) Update(msg tea.Msg) (Goto, tea.Cmd) {
	if !g.visible {
		return g, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(k, g.submitBinds) {
			raw := g.input.Value()
			normalised := normaliseKey(raw)
			if keyPattern.MatchString(normalised) {
				hidden := g.Hide()
				return hidden, func() tea.Msg { return GoToIssueMsg{Key: normalised} }
			}
			return g, func() tea.Msg { return GoToInvalidMsg{Input: raw} }
		}
		if key.Matches(k, g.closeBinding) {
			return g.Hide(), nil
		}
	}
	var cmd tea.Cmd
	g.input, cmd = g.input.Update(msg)
	return g, cmd
}

func (g Goto) View(s styles.Styles) string {
	if !g.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Go to issue")
	hints := s.Muted.Render(
		g.submitBinds.Help().Key + " " + g.submitBinds.Help().Desc + "    " +
			g.closeBinding.Help().Key + " " + g.closeBinding.Help().Desc,
	)
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", g.input.View(), "", hints)
	return s.OverlayBorder.Render(body)
}

func normaliseKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'<>`)
	s = strings.ToUpper(s)
	return s
}
