package styles

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/themes"
)

func fg(s lipgloss.Style) string {
	if c, ok := s.GetForeground().(lipgloss.Color); ok {
		return string(c)
	}
	return ""
}

func bg(s lipgloss.Style) string {
	if c, ok := s.GetBackground().(lipgloss.Color); ok {
		return string(c)
	}
	return ""
}

func borderFg(s lipgloss.Style) string {
	if c, ok := s.GetBorderTopForeground().(lipgloss.Color); ok {
		return string(c)
	}
	return ""
}

func TestNew_TokyoNightForegrounds(t *testing.T) {
	p := themes.TokyoNight()
	s := New(p)

	if s.Palette == nil {
		t.Fatal("Styles.Palette is nil")
	}

	cases := []struct {
		label string
		got   string
		want  string
	}{
		{"App.fg", fg(s.App), string(p.Fg())},
		{"App.bg", bg(s.App), string(p.Bg())},
		{"TopBar.fg", fg(s.TopBar), string(p.Accent())},
		{"HintBar.fg", fg(s.HintBar), string(p.Muted())},
		{"PaneBorder.borderFg", borderFg(s.PaneBorder), string(p.Muted())},
		{"PaneBorderFocused.borderFg", borderFg(s.PaneBorderFocused), string(p.Accent())},
		{"PaneTitle.fg", fg(s.PaneTitle), string(p.Accent())},
		{"ListItem.fg", fg(s.ListItem), string(p.Fg())},
		{"ListItemSelected.fg", fg(s.ListItemSelected), string(p.Bg())},
		{"ListItemSelected.bg", bg(s.ListItemSelected), string(p.Accent())},
		{"GroupHeader.fg", fg(s.GroupHeader), string(p.Cyan())},
		{"SectionHeader.fg", fg(s.SectionHeader), string(p.Accent())},
		{"Description.fg", fg(s.Description), string(p.Fg())},
		{"Muted.fg", fg(s.Muted), string(p.Muted())},
		{"Accent.fg", fg(s.Accent), string(p.Accent())},
		{"Error.fg", fg(s.Error), string(p.Red())},
		{"Toast.bg", bg(s.Toast), string(p.Yellow())},
		{"OverlayBorder.borderFg", borderFg(s.OverlayBorder), string(p.Accent())},
		{"OverlayTitle.fg", fg(s.OverlayTitle), string(p.Magenta())},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.label, c.got, c.want)
		}
		if c.got == "" {
			t.Errorf("%s: foreground/background not set", c.label)
		}
	}
}

func TestNew_PriorityStyle(t *testing.T) {
	p := themes.TokyoNight()
	s := New(p)

	for _, name := range []string{"Highest", "High", "Medium", "Low", "Lowest", "weird"} {
		got := fg(s.Priority(name))
		want := string(p.Priority(name))
		if got != want {
			t.Errorf("Priority(%q).fg = %q, want %q", name, got, want)
		}
	}
}

func TestNew_StatusStyle(t *testing.T) {
	p := themes.TokyoNight()
	s := New(p)

	for _, cat := range []string{"new", "indeterminate", "done", "unknown"} {
		got := fg(s.Status(cat))
		want := string(p.Status(cat))
		if got != want {
			t.Errorf("Status(%q).fg = %q, want %q", cat, got, want)
		}
	}
}

// TestNoHexLiterals enforces the spec rule that styles never reference raw
// hex colors — every color must come from the palette. Acts as a guardrail
// against regressions when new styles are added.
func TestNoHexLiterals(t *testing.T) {
	src, err := os.ReadFile("styles.go")
	if err != nil {
		t.Fatalf("read styles.go: %v", err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		// Skip comments — they may discuss hex colors descriptively.
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.Contains(line, "#") {
			t.Errorf("styles.go:%d contains hex literal: %s", i+1, strings.TrimSpace(line))
		}
	}
}
