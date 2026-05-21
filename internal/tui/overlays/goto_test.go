package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func newGotoForTest() Goto {
	return NewGoto(
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	)
}

func typeRunesGoto(g Goto, s string) Goto {
	for _, r := range s {
		g, _ = g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return g
}

func TestGoto_HiddenIgnoresKeys(t *testing.T) {
	g := newGotoForTest()
	if g.Visible() {
		t.Fatal("zero-value overlay should be hidden")
	}
	g2, cmd := g.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if g2.Visible() {
		t.Fatal("hidden overlay must stay hidden on key input")
	}
	if cmd != nil {
		t.Fatal("hidden overlay must not produce cmds")
	}
}

func TestGoto_ShowFocusesAndClearsInput(t *testing.T) {
	g, _ := newGotoForTest().Show()
	if !g.Visible() {
		t.Fatal("Show must make the overlay visible")
	}
	g = typeRunesGoto(g, "leftover")
	g = g.Hide()
	g, _ = g.Show()
	if got := g.Value(); got != "" {
		t.Fatalf("Show must reset the input, got %q", got)
	}
}

func TestGoto_SubmitValidKeyEmits(t *testing.T) {
	g, _ := newGotoForTest().Show()
	g = typeRunesGoto(g, "abc-123")
	g2, cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on valid key must emit a cmd")
	}
	got, ok := cmd().(GoToIssueMsg)
	if !ok {
		t.Fatalf("expected GoToIssueMsg, got %T", cmd())
	}
	if got.Key != "ABC-123" {
		t.Errorf("normalised key = %q, want ABC-123", got.Key)
	}
	if g2.Visible() {
		t.Fatal("valid submit must hide the overlay")
	}
}

func TestGoto_SubmitInvalidKeyStaysOpenAndToasts(t *testing.T) {
	g, _ := newGotoForTest().Show()
	g = typeRunesGoto(g, "not-a-key")
	g2, cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !g2.Visible() {
		t.Fatal("invalid submit must keep the overlay open")
	}
	if cmd == nil {
		t.Fatal("invalid submit must emit an error toast cmd")
	}
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(GoToInvalidMsg); ok {
			return
		}
	}
	t.Fatalf("invalid submit must emit GoToInvalidMsg")
}

func TestGoto_EscHidesWithoutEmitting(t *testing.T) {
	g, _ := newGotoForTest().Show()
	g = typeRunesGoto(g, "ABC-123")
	g2, cmd := g.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g2.Visible() {
		t.Fatal("esc must hide the overlay")
	}
	if cmd != nil {
		for _, m := range drainBatch(cmd) {
			if _, ok := m.(GoToIssueMsg); ok {
				t.Fatalf("esc must not emit GoToIssueMsg")
			}
		}
	}
}

func TestGoto_NormalisationStripsAndUppercases(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"abc-1", "ABC-1"},
		{"  abc-1  ", "ABC-1"},
		{"<ABC-1>", "ABC-1"},
		{`"ABC-1"`, "ABC-1"},
	}
	for _, tc := range cases {
		g, _ := newGotoForTest().Show()
		g = typeRunesGoto(g, tc.in)
		_, cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%q: no cmd", tc.in)
		}
		got, ok := cmd().(GoToIssueMsg)
		if !ok {
			t.Fatalf("%q: not a GoToIssueMsg (%T)", tc.in, cmd())
		}
		if got.Key != tc.out {
			t.Errorf("%q -> %q, want %q", tc.in, got.Key, tc.out)
		}
	}
}

func TestGoto_ViewContainsTitleAndHints(t *testing.T) {
	g, _ := newGotoForTest().Show()
	v := g.View(epicTestStyles())
	for _, want := range []string{"Go to issue", "enter", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}
