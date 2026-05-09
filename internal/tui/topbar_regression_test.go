package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

// Regression: showing a toast must not push the frame past m.height. When it
// did, bubbletea's standard renderer dropped the topmost row (the program-name
// and tab strip) until the toast TTL expired. The fix renders toasts as a
// top-centre overlay rather than a vertical layout row.
func TestTopBar_ToastDoesNotEvictTopBar(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	mm2, _ := mm.(Model).Update(ToastMsg{Text: "saved", Level: ToastInfo})
	out := mm2.(Model).View()
	lines := strings.Split(out, "\n")
	if got := len(lines); got != 50 {
		t.Fatalf("with toast: outLines=%d, want 50 (frame would push topbar off screen)", got)
	}
	if !strings.Contains(stripANSI(lines[0]), "RJ>") {
		t.Errorf("topbar missing from line 0; first line: %q", stripANSI(lines[0]))
	}
	if !strings.Contains(stripANSI(out), "saved") {
		t.Errorf("toast text missing from frame")
	}
}

func TestTopBar_ManyIssuesDoesNotEvictTopBar(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	mod := mm.(Model)
	issues := make([]jira.Issue, 100)
	for i := range issues {
		issues[i] = jira.Issue{Key: "BILLING-" + itoa(10000+i), Summary: "summary"}
	}
	mod.list.SetIssues(issues)
	out := mod.View()
	lines := strings.Split(out, "\n")
	if got := len(lines); got != 50 {
		t.Fatalf("with paginated list: outLines=%d, want 50", got)
	}
	if !strings.Contains(stripANSI(lines[0]), "RJ>") {
		t.Errorf("topbar missing from line 0; first line: %q", stripANSI(lines[0]))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
