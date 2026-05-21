package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

func newCreatedForTest() Created {
	return NewCreated(
		key.NewBinding(key.WithKeys("y")),
		key.NewBinding(key.WithKeys("Y")),
		key.NewBinding(key.WithKeys("o")), // openInApp
		key.NewBinding(key.WithKeys("O")), // browser
		key.NewBinding(key.WithKeys("esc")),
	)
}

func TestCreated_ShowMakesVisible(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1", URL: "https://j/PROJ-1"})
	if !c.Visible() {
		t.Fatal("overlay should be visible after Show")
	}
	v := c.View(epicTestStyles())
	if !strings.Contains(v, "Created PROJ-1") {
		t.Errorf("view missing key:\n%s", v)
	}
	if !strings.Contains(v, "o open") || !strings.Contains(v, "O browser") {
		t.Errorf("view missing hint text:\n%s", v)
	}
}

func TestCreated_YCopyKeyEmitsRequestAndStaysVisible(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1", URL: "https://j/PROJ-1"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !c2.Visible() {
		t.Fatal("y must not close the overlay")
	}
	if cmd == nil {
		t.Fatal("y must produce a copy cmd")
	}
	got, ok := cmd().(CreatedCopyRequestedMsg)
	if !ok {
		t.Fatalf("expected CreatedCopyRequestedMsg, got %T", cmd())
	}
	if got.Text != "PROJ-1" || got.Label != "key" {
		t.Errorf("copy request = %+v, want {PROJ-1, key}", got)
	}
}

func TestCreated_ShiftYCopyURL(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1", URL: "https://j/PROJ-1"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	if !c2.Visible() {
		t.Fatal("Y must not close")
	}
	got, ok := cmd().(CreatedCopyRequestedMsg)
	if !ok {
		t.Fatalf("expected CreatedCopyRequestedMsg, got %T", cmd())
	}
	if got.Text != "https://j/PROJ-1" || got.Label != "URL" {
		t.Errorf("copy request = %+v", got)
	}
}

func TestCreated_OEmitsOpenInAppAndCloses(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1", URL: "https://j/PROJ-1"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if c2.Visible() {
		t.Fatal("o must close the overlay")
	}
	msgs := drainBatch(cmd)
	var sawOpenInApp, sawDismiss bool
	for _, m := range msgs {
		switch v := m.(type) {
		case CreatedOpenInAppMsg:
			if v.Key == "PROJ-1" {
				sawOpenInApp = true
			}
		case CreatedDismissedMsg:
			if v.Key == "PROJ-1" {
				sawDismiss = true
			}
		}
	}
	if !sawOpenInApp || !sawDismiss {
		t.Errorf("o batch missing in-app=%v dismiss=%v", sawOpenInApp, sawDismiss)
	}
}

func TestCreated_ShiftOEmitsBrowserAndCloses(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1", URL: "https://j/PROJ-1"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	if c2.Visible() {
		t.Fatal("O must close the overlay")
	}
	msgs := drainBatch(cmd)
	var sawBrowser, sawDismiss bool
	for _, m := range msgs {
		switch v := m.(type) {
		case CreatedOpenRequestedMsg:
			if v.URL == "https://j/PROJ-1" {
				sawBrowser = true
			}
		case CreatedDismissedMsg:
			if v.Key == "PROJ-1" {
				sawDismiss = true
			}
		}
	}
	if !sawBrowser || !sawDismiss {
		t.Errorf("O batch missing browser=%v dismiss=%v", sawBrowser, sawDismiss)
	}
}

func TestCreated_OWithEmptyURLEmitsOpenInAppAndCloses(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1"}) // no URL
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if c2.Visible() {
		t.Fatal("o must close the overlay even with empty URL")
	}
	msgs := drainBatch(cmd)
	var sawOpenInApp, sawDismiss bool
	for _, m := range msgs {
		switch v := m.(type) {
		case CreatedOpenInAppMsg:
			if v.Key == "PROJ-1" {
				sawOpenInApp = true
			}
		case CreatedDismissedMsg:
			if v.Key == "PROJ-1" {
				sawDismiss = true
			}
		}
	}
	if !sawOpenInApp || !sawDismiss {
		t.Errorf("o (no URL) batch missing in-app=%v dismiss=%v", sawOpenInApp, sawDismiss)
	}
}

func TestCreated_ShiftOWithEmptyURLClosesWithDismiss(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-1"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	if c2.Visible() {
		t.Fatal("O with empty URL must still close")
	}
	msgs := drainBatch(cmd)
	for _, m := range msgs {
		if _, ok := m.(CreatedOpenRequestedMsg); ok {
			t.Fatalf("O must not emit open with empty URL")
		}
	}
}

func TestCreated_EscClosesAndDismisses(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-2", URL: "https://j/PROJ-2"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c2.Visible() {
		t.Fatal("esc must close")
	}
	got, ok := cmd().(CreatedDismissedMsg)
	if !ok || got.Key != "PROJ-2" {
		t.Errorf("dismiss msg = %+v ok=%v", got, ok)
	}
}

func TestCreated_EnterClosesAndDismisses(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-3"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c2.Visible() {
		t.Fatal("enter must close")
	}
	got, ok := cmd().(CreatedDismissedMsg)
	if !ok || got.Key != "PROJ-3" {
		t.Errorf("dismiss msg = %+v ok=%v", got, ok)
	}
}

func TestCreated_ForeignKeyIsSwallowed(t *testing.T) {
	c := newCreatedForTest()
	c = c.Show(jira.Issue{Key: "PROJ-4"})
	c2, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if !c2.Visible() {
		t.Fatal("foreign key must not close overlay")
	}
	if cmd != nil {
		t.Fatal("foreign key must not produce cmd")
	}
}

// drainBatch resolves a tea.BatchMsg into a flat slice of underlying messages.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	out := []tea.Msg{}
	switch v := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range v {
			out = append(out, drainBatch(c)...)
		}
	default:
		out = append(out, v)
	}
	return out
}
