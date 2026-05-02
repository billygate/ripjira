package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	m.Run()
}

func newStyles(t *testing.T) styles.Styles {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return styles.New(p)
}

func sampleColumns() []HelpColumn {
	return []HelpColumn{
		{
			Title: "Navigation",
			Bindings: []key.Binding{
				key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up")),
				key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down")),
			},
		},
		{
			Title: "Actions",
			Bindings: []key.Binding{
				key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
				key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
			},
		},
	}
}

func closeBinding() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
}

func TestHelp_HiddenByDefault(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding())
	if h.Visible() {
		t.Error("Help should start hidden")
	}
	if got := h.View(newStyles(t)); got != "" {
		t.Errorf("hidden View should be empty, got %q", got)
	}
}

func TestHelp_ShowAndHide(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding())
	h = h.Show()
	if !h.Visible() {
		t.Fatal("Show did not flip visible")
	}
	h = h.Hide()
	if h.Visible() {
		t.Fatal("Hide did not flip visible")
	}
}

func TestHelp_RendersAllBindings(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding()).Show()
	view := stripANSI(h.View(newStyles(t)))

	wantTitles := []string{"Navigation", "Actions", "Keymap"}
	for _, w := range wantTitles {
		if !strings.Contains(view, w) {
			t.Errorf("help view missing title %q\noutput:\n%s", w, view)
		}
	}

	for _, col := range sampleColumns() {
		for _, b := range col.Bindings {
			h := b.Help()
			if !strings.Contains(view, h.Key) {
				t.Errorf("help view missing key %q\noutput:\n%s", h.Key, view)
			}
			if !strings.Contains(view, h.Desc) {
				t.Errorf("help view missing desc %q\noutput:\n%s", h.Desc, view)
			}
		}
	}
}

func TestHelp_CloseKeyHides(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding()).Show()
	updated, _ := h.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("esc should hide the overlay")
	}
}

func TestHelp_OtherKeysSwallowedWhileVisible(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding()).Show()
	updated, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !updated.Visible() {
		t.Error("non-close key should not hide the overlay")
	}
	if cmd != nil {
		t.Errorf("non-close key returned cmd: %v", cmd)
	}
}

func TestHelp_UpdateNoopWhileHidden(t *testing.T) {
	h := NewHelp(sampleColumns(), closeBinding())
	updated, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("hidden overlay should stay hidden")
	}
	if cmd != nil {
		t.Errorf("hidden update returned cmd: %v", cmd)
	}
}

// stripANSI removes ANSI CSI sequences so assertions can target plain text.
func stripANSI(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	in := []byte(s)
	for i := 0; i < len(in); i++ {
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '[' {
			j := i + 2
			for j < len(in) {
				c := in[j]
				if (c >= 0x40 && c <= 0x7e) || c == 'm' {
					j++
					break
				}
				j++
			}
			i = j - 1
			continue
		}
		out.WriteByte(in[i])
	}
	return out.String()
}
