package overlays

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

func epicTestStyles() styles.Styles {
	p, err := themes.ByName("tokyonight")
	if err != nil {
		panic(err)
	}
	return styles.New(p)
}

func TestEpicPicker_EnterDispatchesPicked(t *testing.T) {
	epics := []jira.Issue{
		{Key: "BILLING-100", Summary: "Setup deploy"},
		{Key: "BILLING-200", Summary: "Migrate billing"},
	}
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics(epics)

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a Cmd")
	}
	msg, ok := cmd().(EpicPickedMsg)
	if !ok {
		t.Fatalf("got %T", cmd())
	}
	if msg.IssueKey != "BILLING-1" || msg.ParentKey != "BILLING-200" {
		t.Fatalf("picked: %#v", msg)
	}
}

func TestEpicPicker_DetachRowAppearsWhenCurrentSet(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "BILLING-100")
	p = p.SetEpics([]jira.Issue{{Key: "BILLING-100", Summary: "Setup deploy"}})

	view := p.View(epicTestStyles())
	if !strings.Contains(view, "No epic") {
		t.Fatalf("detach row missing; view:\n%s", view)
	}
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd().(EpicPickedMsg)
	if msg.ParentKey != "" {
		t.Fatalf("expected detach (empty ParentKey), got %q", msg.ParentKey)
	}
}

func TestEpicPicker_DetachRowHiddenWhenNoCurrent(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics([]jira.Issue{{Key: "BILLING-100", Summary: "Setup deploy"}})

	view := p.View(epicTestStyles())
	if strings.Contains(view, "No epic") {
		t.Fatalf("detach row should be hidden when no current parent; view:\n%s", view)
	}
}

func TestEpicPicker_FilterMatchesKeyOrSummary(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics([]jira.Issue{
		{Key: "BILLING-100", Summary: "Setup deploy"},
		{Key: "BILLING-200", Summary: "Migrate billing"},
	})
	for _, r := range "depl" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := p.View(epicTestStyles())
	if !strings.Contains(view, "BILLING-100") || strings.Contains(view, "BILLING-200") {
		t.Fatalf("filter should keep BILLING-100, drop BILLING-200; view:\n%s", view)
	}
}

func TestEpicPicker_FilterAcceptsJK(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics([]jira.Issue{
		{Key: "BILLING-100", Summary: "Backend work"},
		{Key: "BILLING-200", Summary: "Frontend"},
	})
	for _, r := range "Back" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if p.filter != "Back" {
		t.Fatalf("filter = %q, want %q (j/k must not be hijacked once typing has started)", p.filter, "Back")
	}
	view := p.View(epicTestStyles())
	if !strings.Contains(view, "BILLING-100") || strings.Contains(view, "BILLING-200") {
		t.Fatalf("filter should keep BILLING-100 (Backend), drop BILLING-200; view:\n%s", view)
	}
}

func TestEpicPicker_JKNavigateWhenFilterEmpty(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics([]jira.Issue{
		{Key: "BILLING-100", Summary: "A"},
		{Key: "BILLING-200", Summary: "B"},
	})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.cursor != 1 {
		t.Fatalf("j with empty filter should move cursor down: cursor = %d", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if p.cursor != 0 {
		t.Fatalf("k with empty filter should move cursor up: cursor = %d", p.cursor)
	}
	if p.filter != "" {
		t.Fatalf("filter should remain empty: %q", p.filter)
	}
}

func TestEpicPicker_EscDispatchesCancelled(t *testing.T) {
	p := NewEpic()
	p = p.Show("BILLING-1", "")
	p = p.SetEpics([]jira.Issue{{Key: "BILLING-100", Summary: "x"}})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a Cmd")
	}
	if _, ok := cmd().(EpicCancelledMsg); !ok {
		t.Fatalf("expected EpicCancelledMsg, got %T", cmd())
	}
}
