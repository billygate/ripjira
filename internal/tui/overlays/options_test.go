package overlays

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

func newTestOptions(t *testing.T) Options {
	t.Helper()
	closeKey := key.NewBinding(key.WithKeys("esc"))
	return NewOptions(closeKey, "status", "priority", false)
}

func testStyles(t *testing.T) styles.Styles {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return styles.New(p)
}

func TestOptionsGroupings_IncludesParent(t *testing.T) {
	want := map[string]string{
		"status":   "Status",
		"priority": "Priority",
		"epic":     "Epic",
		"parent":   "Parent (epic)",
	}
	got := map[string]string{}
	for _, g := range optionsGroupings {
		got[g.Name] = g.Label
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groupings = %#v, want %#v", got, want)
	}
}

func TestOptions_HiddenByDefault(t *testing.T) {
	o := newTestOptions(t)
	if o.Visible() {
		t.Fatal("new overlay should be hidden")
	}
	if got := o.View(testStyles(t)); got != "" {
		t.Fatalf("hidden View() should be empty, got %q", got)
	}
}

func TestOptions_ShowSeedsCursors(t *testing.T) {
	o := newTestOptions(t).Show("priority", "updated", true)
	if !o.Visible() {
		t.Fatal("Show should make overlay visible")
	}
	if o.Grouping() != "priority" {
		t.Fatalf("Grouping = %q", o.Grouping())
	}
	if o.SortName() != "updated" {
		t.Fatalf("SortName = %q", o.SortName())
	}
	if !o.Desc() {
		t.Fatalf("Desc = %v", o.Desc())
	}
}

func TestOptions_TabSwitchesSection(t *testing.T) {
	o := newTestOptions(t).Show("status", "priority", false)
	o, _ = o.Update(tea.KeyMsg{Type: tea.KeyTab})
	// first section is Grouping (0); after tab move to Sorting (1)
	o2, _ := o.Update(tea.KeyMsg{Type: tea.KeyDown})
	if o2.SortName() == "priority" && o.SortName() != "priority" {
		// unreachable with current impl — keep as defensive sentinel
	}
	// Concrete check: after Tab, Down should move the sort cursor.
	if o2.SortName() == "priority" {
		t.Fatalf("after Tab+Down, SortName should have advanced; got %q", o2.SortName())
	}
}

func TestOptions_DownMovesCursorWithinSection(t *testing.T) {
	o := newTestOptions(t).Show("status", "priority", false)
	o, _ = o.Update(tea.KeyMsg{Type: tea.KeyDown})
	if o.Grouping() != "priority" {
		t.Fatalf("after down on grouping section, Grouping = %q want priority", o.Grouping())
	}
}

func TestOptions_DTogglesDirection(t *testing.T) {
	o := newTestOptions(t).Show("status", "priority", false)
	o, _ = o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !o.Desc() {
		t.Fatalf("after d, Desc = false, want true")
	}
}

func TestOptions_EnterEmitsAppliedAndHides(t *testing.T) {
	o := newTestOptions(t).Show("status", "priority", false)
	o, cmd := o.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if o.Visible() {
		t.Fatal("after enter overlay should be hidden")
	}
	if cmd == nil {
		t.Fatal("enter should return a non-nil cmd")
	}
	msg := cmd()
	applied, ok := msg.(OptionsAppliedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want OptionsAppliedMsg", msg)
	}
	if applied.Grouping != "status" || applied.Sort != "priority" || applied.Desc {
		t.Fatalf("applied = %+v", applied)
	}
}

func TestOptions_EscEmitsCancelledAndHides(t *testing.T) {
	o := newTestOptions(t).Show("status", "priority", false)
	o, cmd := o.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if o.Visible() {
		t.Fatal("after esc overlay should be hidden")
	}
	if cmd == nil {
		t.Fatal("esc should return cancelled cmd")
	}
	if _, ok := cmd().(OptionsCancelledMsg); !ok {
		t.Fatalf("esc msg type = %T, want OptionsCancelledMsg", cmd())
	}
}

func TestOptions_ViewMentionsSelectionMarkers(t *testing.T) {
	o := newTestOptions(t).Show("priority", "updated", true)
	out := o.View(testStyles(t))
	if !strings.Contains(out, "Grouping") || !strings.Contains(out, "Sorting") {
		t.Fatalf("View should label both sections; got:\n%s", out)
	}
	if !strings.Contains(out, "Priority") || !strings.Contains(out, "Updated") {
		t.Fatalf("View should list current selections; got:\n%s", out)
	}
}
