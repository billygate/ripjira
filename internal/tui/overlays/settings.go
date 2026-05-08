package overlays

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// SettingsAppliedMsg is emitted on ctrl+s with the validated draft.
type SettingsAppliedMsg struct {
	NewCfg config.Config
}

// SettingsCancelledMsg is emitted on Esc.
type SettingsCancelledMsg struct{}

// Row indices in the editor.
const (
	rowTheme = iota
	rowIcons
	rowGrouping
	rowAutoRefresh
	rowEpicTypes
	rowCount
)

// Enum value lists. Keep stable order — the editor cycles in this order.
// The "catppuccin" alias is intentionally excluded so the user always sees
// the canonical "catppuccin-mocha" name.
var iconsChoices = []string{config.IconsUnicode, config.IconsASCII}
var groupingChoices = []string{
	config.GroupingStatus,
	config.GroupingPriority,
	config.GroupingEpic,
	config.GroupingParent,
}

func themeChoices() []string {
	out := []string{
		config.ThemeTokyoNight,
		config.ThemeCatppuccinMocha,
		config.ThemeGruvbox,
		config.ThemeNord,
		config.ThemeRosePine,
		config.ThemeDracula,
		config.ThemeSolarizedDark,
		config.ThemeSolarizedLight,
		config.ThemeEverforest,
		config.ThemeKanagawa,
		config.ThemeMonokai,
		config.ThemeOneDark,
	}
	sort.Strings(out)
	return out
}

// Settings is the ctrl+, overlay. It owns a draft Config, a cursor,
// and an optional textinput for the auto_refresh_seconds row. The epic
// types editor is a separate sub-overlay (EpicTypes) reachable from
// the row 4 entry.
type Settings struct {
	visible      bool
	closeBinding key.Binding

	draft  config.Config
	cursor int

	editing  bool
	input    textinput.Model
	rowError string

	epic EpicTypes
}

// NewSettings builds a hidden overlay. closeKey is held for parity with
// other overlays (the overlay also handles tea.KeyEsc directly).
func NewSettings(closeKey key.Binding) Settings {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 6
	in.Width = 6
	return Settings{
		closeBinding: closeKey,
		input:        in,
		epic:         NewEpicTypes(closeKey),
	}
}

func (o Settings) Visible() bool        { return o.visible }
func (o Settings) Cursor() int          { return o.cursor }
func (o Settings) Editing() bool        { return o.editing }
func (o Settings) Draft() config.Config { return o.draft }
func (o Settings) RowError() string     { return o.rowError }
func (o Settings) EpicTypesOpen() bool  { return o.epic.Visible() }

// SetInputForTest forces the textinput value (test helper).
func (o *Settings) SetInputForTest(v string) { o.input.SetValue(v) }

// Show seeds the overlay with `current` as the draft and resets cursor /
// editing state. Returns a copy.
func (o Settings) Show(current config.Config) Settings {
	o.visible = true
	o.draft = current
	o.cursor = 0
	o.editing = false
	o.rowError = ""
	o.input.Reset()
	o.input.Blur()
	o.epic = o.epic.Hide()
	return o
}

// Hide closes the overlay and any open sub-overlay.
func (o Settings) Hide() Settings {
	o.visible = false
	o.editing = false
	o.input.Reset()
	o.input.Blur()
	o.epic = o.epic.Hide()
	o.rowError = ""
	return o
}

// WithEpicTypes returns a copy with draft.EpicIssueTypes replaced. The
// root model calls this on EpicTypesAppliedMsg.
func (o Settings) WithEpicTypes(items []string) Settings {
	o.draft.EpicIssueTypes = append([]string(nil), items...)
	o.epic = o.epic.Hide()
	return o
}

// CloseEpicTypes closes the sub-overlay without changing the draft. Called
// on EpicTypesCancelledMsg.
func (o Settings) CloseEpicTypes() Settings {
	o.epic = o.epic.Hide()
	return o
}

// Update routes key events. While the sub-overlay is visible, key events
// flow into it; the root model interprets the resulting EpicTypes*Msg and
// calls WithEpicTypes / CloseEpicTypes.
func (o Settings) Update(msg tea.Msg) (Settings, tea.Cmd) {
	if !o.visible {
		return o, nil
	}
	if o.epic.Visible() {
		var cmd tea.Cmd
		o.epic, cmd = o.epic.Update(msg)
		return o, cmd
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	if o.editing {
		return o.updateEditing(k)
	}
	return o.updateBrowsing(k)
}

func (o Settings) updateBrowsing(k tea.KeyMsg) (Settings, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		applied := SettingsCancelledMsg{}
		return o.Hide(), func() tea.Msg { return applied }
	case tea.KeyCtrlS:
		if err := o.draft.Validate(); err != nil {
			o.rowError = err.Error()
			return o, nil
		}
		applied := SettingsAppliedMsg{NewCfg: o.draft}
		return o.Hide(), func() tea.Msg { return applied }
	case tea.KeyDown:
		if o.cursor < rowCount-1 {
			o.cursor++
			o.rowError = ""
		}
		return o, nil
	case tea.KeyUp:
		if o.cursor > 0 {
			o.cursor--
			o.rowError = ""
		}
		return o, nil
	case tea.KeyRight:
		o.cycle(+1)
		return o, nil
	case tea.KeyLeft:
		o.cycle(-1)
		return o, nil
	case tea.KeyEnter:
		switch o.cursor {
		case rowTheme, rowIcons, rowGrouping:
			o.cycle(+1)
		case rowAutoRefresh:
			o.editing = true
			o.input.SetValue(strconv.Itoa(o.draft.AutoRefreshSeconds))
			o.input.CursorEnd()
			o.input.Focus()
			o.rowError = ""
		case rowEpicTypes:
			o.epic = o.epic.Show(o.draft.EpicIssueTypes)
		}
		return o, nil
	}
	switch string(k.Runes) {
	case "j":
		if o.cursor < rowCount-1 {
			o.cursor++
		}
	case "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "h":
		o.cycle(-1)
	case "l":
		o.cycle(+1)
	}
	return o, nil
}

func (o Settings) updateEditing(k tea.KeyMsg) (Settings, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		o.editing = false
		o.input.Reset()
		o.input.Blur()
		o.rowError = ""
		return o, nil
	case tea.KeyEnter:
		v := strings.TrimSpace(o.input.Value())
		n, err := strconv.Atoi(v)
		if err != nil {
			o.rowError = fmt.Sprintf("not a number: %q", v)
			return o, nil
		}
		if n < 0 {
			o.rowError = "must be ≥ 0"
			return o, nil
		}
		o.draft.AutoRefreshSeconds = n
		o.editing = false
		o.input.Reset()
		o.input.Blur()
		o.rowError = ""
		return o, nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(k)
	return o, cmd
}

func (o *Settings) cycle(delta int) {
	switch o.cursor {
	case rowTheme:
		o.draft.Theme = cycleString(themeChoices(), o.draft.Theme, delta)
	case rowIcons:
		o.draft.Icons = cycleString(iconsChoices, o.draft.Icons, delta)
	case rowGrouping:
		o.draft.DefaultGrouping = cycleString(groupingChoices, o.draft.DefaultGrouping, delta)
	}
	o.rowError = ""
}

func cycleString(choices []string, current string, delta int) string {
	idx := 0
	for i, c := range choices {
		if c == current {
			idx = i
			break
		}
	}
	n := len(choices)
	idx = (idx + delta + n) % n
	return choices[idx]
}

// View renders the overlay (or the sub-overlay when it is open).
func (o Settings) View(s styles.Styles) string {
	if !o.visible {
		return ""
	}
	if o.epic.Visible() {
		return o.epic.View(s)
	}
	rows := []string{s.OverlayTitle.Render("Settings"), ""}
	rows = append(rows, o.row(s, rowTheme, "Theme", o.draft.Theme))
	rows = append(rows, o.row(s, rowIcons, "Icons", o.draft.Icons))
	rows = append(rows, o.row(s, rowGrouping, "Default grouping", o.draft.DefaultGrouping))
	if o.editing && o.cursor == rowAutoRefresh {
		rows = append(rows, o.row(s, rowAutoRefresh, "Auto refresh (s)", o.input.View()))
	} else {
		rows = append(rows, o.row(s, rowAutoRefresh, "Auto refresh (s)", strconv.Itoa(o.draft.AutoRefreshSeconds)))
	}
	rows = append(rows, o.row(s, rowEpicTypes, "Epic issue types", strings.Join(o.draft.EpicIssueTypes, ", ")))
	if o.rowError != "" {
		rows = append(rows, "", s.Muted.Render("error: "+o.rowError))
	}
	rows = append(rows, "", s.Muted.Render("↑/↓ row · ←/→ change · enter edit/open · ctrl+s save · esc cancel"))
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (o Settings) row(s styles.Styles, idx int, label, value string) string {
	prefix := "  "
	if idx == o.cursor {
		prefix = "▸ "
	}
	line := fmt.Sprintf("%s%-18s %s", prefix, label, value)
	if idx == o.cursor {
		return s.ListItemSelected.Render(line)
	}
	return s.ListItem.Render(line)
}
