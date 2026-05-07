package overlays

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
)

func newSettings(c config.Config) Settings {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	return NewSettings(closeKey).Show(c)
}

func defaultCfg() config.Config {
	return config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic"},
	}
}

func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestSettingsCursorMoves(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyDown))
	if o.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", o.Cursor())
	}
}

func TestSettingsCycleTheme(t *testing.T) {
	o := newSettings(defaultCfg())
	start := o.Draft().Theme
	o, _ = o.Update(keyType(tea.KeyRight))
	if o.Draft().Theme == start {
		t.Fatal("right arrow on Theme row did not change value")
	}
	for i := 0; i < 10; i++ {
		o, _ = o.Update(keyType(tea.KeyRight))
		if o.Draft().Theme == start {
			return
		}
	}
	t.Fatal("Theme cycling did not wrap back to start within 10 steps")
}

func TestSettingsCycleIcons(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyRight))
	if o.Draft().Icons != config.IconsASCII {
		t.Fatalf("Icons = %q, want ascii", o.Draft().Icons)
	}
	o, _ = o.Update(keyType(tea.KeyRight))
	if o.Draft().Icons != config.IconsUnicode {
		t.Fatalf("Icons after wrap = %q, want unicode", o.Draft().Icons)
	}
}

func TestSettingsCycleDefaultGrouping(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyRight))
	if o.Draft().DefaultGrouping == config.GroupingStatus {
		t.Fatal("right arrow on grouping row did not change value")
	}
}

func TestSettingsEditAutoRefresh(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyDown))
	o, _ = o.Update(keyType(tea.KeyEnter))
	if !o.Editing() {
		t.Fatal("should be in editing mode")
	}
	o.SetInputForTest("30")
	o, _ = o.Update(keyType(tea.KeyEnter))
	if o.Draft().AutoRefreshSeconds != 30 {
		t.Fatalf("AutoRefreshSeconds = %d, want 30", o.Draft().AutoRefreshSeconds)
	}
	if o.Editing() {
		t.Fatal("should have exited editing mode")
	}
}

func TestSettingsAutoRefreshRejectsNegative(t *testing.T) {
	o := newSettings(defaultCfg())
	for i := 0; i < 3; i++ {
		o, _ = o.Update(keyType(tea.KeyDown))
	}
	o, _ = o.Update(keyType(tea.KeyEnter))
	o.SetInputForTest("-1")
	o, _ = o.Update(keyType(tea.KeyEnter))
	if o.Draft().AutoRefreshSeconds != 60 {
		t.Fatalf("AutoRefreshSeconds = %d, want 60 (unchanged)", o.Draft().AutoRefreshSeconds)
	}
	if o.RowError() == "" {
		t.Fatal("expected row error to be set")
	}
}

func TestSettingsAutoRefreshRejectsNonNumeric(t *testing.T) {
	o := newSettings(defaultCfg())
	for i := 0; i < 3; i++ {
		o, _ = o.Update(keyType(tea.KeyDown))
	}
	o, _ = o.Update(keyType(tea.KeyEnter))
	o.SetInputForTest("abc")
	o, _ = o.Update(keyType(tea.KeyEnter))
	if o.Draft().AutoRefreshSeconds != 60 {
		t.Fatal("AutoRefreshSeconds changed on invalid input")
	}
	if o.RowError() == "" {
		t.Fatal("expected row error")
	}
}

func TestSettingsCtrlSEmitsApplied(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyRight))
	expectedTheme := o.Draft().Theme
	_, cmd := o.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("ctrl+s must produce a cmd")
	}
	msg := cmd()
	applied, ok := msg.(SettingsAppliedMsg)
	if !ok {
		t.Fatalf("expected SettingsAppliedMsg, got %T", msg)
	}
	if applied.NewCfg.Theme != expectedTheme {
		t.Fatalf("Applied.NewCfg.Theme = %q, want %q", applied.NewCfg.Theme, expectedTheme)
	}
}

func TestSettingsEscEmitsCancelled(t *testing.T) {
	o := newSettings(defaultCfg())
	_, cmd := o.Update(keyType(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must produce a cmd")
	}
	if _, ok := cmd().(SettingsCancelledMsg); !ok {
		t.Fatalf("expected SettingsCancelledMsg, got %T", cmd())
	}
}

func TestSettingsEpicTypesOpensSubOverlay(t *testing.T) {
	o := newSettings(defaultCfg())
	for i := 0; i < 4; i++ {
		o, _ = o.Update(keyType(tea.KeyDown))
	}
	o, cmd := o.Update(keyType(tea.KeyEnter))
	if !o.EpicTypesOpen() {
		t.Fatal("epic sub-overlay should be open")
	}
	_ = cmd
}

func TestSettingsApplyEpicTypes(t *testing.T) {
	o := newSettings(defaultCfg())
	o = o.WithEpicTypes([]string{"Theme", "Initiative"})
	if got := o.Draft().EpicIssueTypes; !reflect.DeepEqual(got, []string{"Theme", "Initiative"}) {
		t.Fatalf("EpicIssueTypes = %v", got)
	}
}

func TestSettingsShowResetsState(t *testing.T) {
	o := newSettings(defaultCfg())
	o, _ = o.Update(keyType(tea.KeyDown))
	o = o.Show(defaultCfg())
	if o.Cursor() != 0 {
		t.Fatalf("cursor = %d after Show, want 0", o.Cursor())
	}
}
