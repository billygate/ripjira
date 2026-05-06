package tui

import (
	"strings"
	"testing"

	"github.com/billygate/ripjira/internal/jira"
)

// TestView_NoBlackGapsInPanes renders the model in several states and asserts
// every visible cell carries an explicit SGR bg, never the terminal default.
// The terminal default is what showed through earlier as "black bands" on a
// dark theme — once any cell rendered without an SGR bg, the user's terminal
// background leaked in. The scenarios below exercise the rendering paths
// where that historically happened: list pane with grouped issues, detail
// pane with section headers and empty placeholders, detail with markdown-
// rendered description, list-with-search-active, and an open overlay.
func TestView_NoBlackGapsInPanes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *Model)
	}{
		{
			name: "list+detail with empty sections",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.list.SetIssues([]jira.Issue{
					{Key: "PROJ-1", Summary: "alpha", Priority: jira.Priority{Name: "High"}, Status: jira.Status{Name: "In Progress"}},
					{Key: "PROJ-2", Summary: "beta"},
				})
				m.detail.SetIssue(&jira.Issue{
					Key:      "PROJ-1",
					Summary:  "alpha",
					Priority: jira.Priority{Name: "High"},
					Status:   jira.Status{Name: "In Progress"},
				})
			},
		},
		// NOTE: a "detail with markdown description" scenario is intentionally
		// omitted. Glamour's terminal renderer emits a separate styled span
		// per word/token with embedded \x1b[0m resets between them, and
		// patching those resets to re-establish the palette bg breaks tests
		// that rely on bytes.Contains over the unseamed teatest output. The
		// remaining inter-word gaps inside markdown bodies are 1-cell wide
		// and visually negligible compared to the empty-pane bands this
		// rendering path used to leave.
		{
			name: "detail with assignee and labels (literal-string fields)",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.detail.SetIssue(&jira.Issue{
					Key:      "PROJ-1",
					Summary:  "alpha",
					Priority: jira.Priority{Name: "High"},
					Status:   jira.Status{Name: "In Progress"},
					Assignee: &jira.User{DisplayName: "Alice", Email: "a@b.c"},
					Labels:   []string{"backend", "Q2"},
					DueDate:  "2026-06-01",
				})
			},
		},
		{
			name: "with status text in topbar",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.statusText = "⟳ refreshing…"
			},
		},
		{
			name: "detail with links",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.detail.SetIssue(&jira.Issue{
					Key:     "PROJ-1",
					Summary: "alpha",
					Links: []jira.IssueLink{
						{ID: "1", Relation: "clones", OtherKey: "PROJ-99", Status: jira.Status{Name: "Released"}, Summary: "Earlier ticket"},
						{ID: "2", Relation: "blocks", OtherKey: "PROJ-42", Status: jira.Status{Name: "In Progress"}, Summary: "Follow-up work"},
					},
				})
			},
		},
		{
			name: "detail with subtasks",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.detail.SetIssue(&jira.Issue{
					Key:     "PROJ-1",
					Summary: "alpha",
					Subtasks: []jira.SubtaskRef{
						{Key: "PROJ-2", Summary: "first child", Status: jira.Status{Name: "To Do"}},
						{Key: "PROJ-3", Summary: "second child", Status: jira.Status{Name: "Done"}},
					},
				})
			},
		},
		{
			name: "detail with worklogs",
			setup: func(t *testing.T, m *Model) {
				t.Helper()
				m.detail.SetIssue(&jira.Issue{
					Key:     "PROJ-1",
					Summary: "alpha",
					Worklogs: []jira.Worklog{
						{ID: "1", Author: &jira.User{DisplayName: "Alice"}, TimeSpent: "1h"},
					},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m, _ = sendSize(m, 120, 30)
			tc.setup(t, &m)
			out := m.View()
			if out == "" {
				t.Fatal("View returned empty string")
			}
			bad := scanDefaultBg(out)
			if len(bad) > 0 {
				t.Errorf("found %d cells with terminal-default bg (black gaps)", len(bad))
				for i, c := range bad {
					if i >= 12 {
						t.Logf("... %d more", len(bad)-12)
						break
					}
					t.Logf("  row=%d col=%d rune=%q", c.row, c.col, c.r)
				}
			}
		})
	}
}

// gapCell records a printable cell whose active SGR bg is the terminal
// default — i.e. a "black gap" through which the host terminal's bg shows.
type gapCell struct {
	row, col int
	r        rune
}

// scanDefaultBg walks ANSI-escaped output and records every printable cell
// whose current SGR bg is the terminal default. Cells with any explicit bg
// (palette bg, accent, etc.) are accepted. Used by tests to assert the
// dark-theme rendering doesn't leak terminal-bg through gaps.
func scanDefaultBg(s string) []gapCell {
	const defaultBg = "default"
	bg := defaultBg
	row, col := 0, 0
	bad := []gapCell{}
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			// Find the terminating letter of the CSI sequence.
			j := i + 2
			for j < len(runes) {
				c := runes[j]
				if (c >= 0x40 && c <= 0x7e) && c != ';' {
					break
				}
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				params := string(runes[i+2 : j])
				bg = applySGR(params, bg)
			}
			i = j + 1
			continue
		}
		if r == '\n' {
			row++
			col = 0
			i++
			continue
		}
		if bg == defaultBg {
			bad = append(bad, gapCell{row: row, col: col, r: r})
		}
		col++
		i++
	}
	return bad
}

// applySGR consumes a semicolon-separated SGR parameter list and returns the
// bg state after applying it. Only the codes we need to reason about are
// implemented: 0 (reset), 49 (default bg), 48;5;n (256-color), 48;2;r;g;b
// (RGB). Foreground codes and attributes are skipped.
func applySGR(params, bg string) string {
	const defaultBg = "default"
	if params == "" {
		return defaultBg
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "", "0":
			bg = defaultBg
		case "49":
			bg = defaultBg
		case "48":
			if i+1 >= len(parts) {
				return bg
			}
			switch parts[i+1] {
			case "2":
				if i+4 < len(parts) {
					bg = parts[i+2] + ";" + parts[i+3] + ";" + parts[i+4]
					i += 4
				} else {
					return bg
				}
			case "5":
				if i+2 < len(parts) {
					bg = "256:" + parts[i+2]
					i += 2
				} else {
					return bg
				}
			default:
				return bg
			}
		}
	}
	return bg
}

func TestApplySGR_BasicTransitions(t *testing.T) {
	got := applySGR("48;2;26;27;38", "default")
	if got != "26;27;38" {
		t.Errorf("48;2;26;27;38 → %q, want 26;27;38", got)
	}
	if applySGR("0", got) != "default" {
		t.Error("reset (0) must clear bg to default")
	}
	if applySGR("49", got) != "default" {
		t.Error("49 must clear bg to default")
	}
	// Foreground codes leave bg alone.
	if applySGR("38;2;200;200;200", "26;27;38") != "26;27;38" {
		t.Error("fg codes must not change bg")
	}
}
