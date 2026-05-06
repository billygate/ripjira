package tui

import (
	"testing"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/themes"
)

func BenchmarkView_TypicalFrame(b *testing.B) {
	m := newBenchModel(b)
	m, _ = sendSize(m, 160, 50)
	m.list.SetIssues(benchIssues(40))
	m.detail.SetIssue(&jira.Issue{
		Key:      "PROJ-1",
		Summary:  "alpha",
		Priority: jira.Priority{Name: "High"},
		Status:   jira.Status{Name: "In Progress"},
	})
	m.statusText = "refreshing…"
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

func newBenchModel(tb testing.TB) Model {
	tb.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		tb.Fatalf("load tokyonight: %v", err)
	}
	return New(p)
}

func benchIssues(n int) []jira.Issue {
	out := make([]jira.Issue, n)
	for i := range out {
		out[i] = jira.Issue{
			Key:      benchKey(i + 1),
			Summary:  "row",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "Medium"},
		}
	}
	return out
}

func benchKey(i int) string {
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return "PROJ-" + string(digits)
}
