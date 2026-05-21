package overlays

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// FieldKind classifies a createmeta field into one of the widget shapes the
// dynamic-form step renders. The mapping from Jira schema → kind lives in
// detectFieldKind and matches the spec table in §5 of the design doc.
type FieldKind int

const (
	// FieldKindUnknown means the field was emitted by createmeta but its
	// schema does not map to any supported widget. Such fields are skipped
	// by BuildForm and surfaced to the user via Form.Warnings.
	FieldKindUnknown FieldKind = iota
	// FieldKindString is a single-line free-text input (summary, etc.).
	FieldKindString
	// FieldKindADF is a multi-line textarea whose contents are converted to
	// ADF at submit time. Used for description.
	FieldKindADF
	// FieldKindOption is a single-select cycling the field's allowedValues.
	// Covers Jira schema types option, priority and issuetype.
	FieldKindOption
	// FieldKindUser is a text input with a user picker dropdown. The visible
	// value is the display name; the submitted value is the accountID stored
	// via SetUserSelection. Typed text without a committed pick does not submit.
	FieldKindUser
	// FieldKindMultiOption is a multi-select cycling allowedValues with
	// space toggling the current row. Maps to schema type=array,items=option.
	FieldKindMultiOption
	// FieldKindNumber is a free-text input restricted to numeric content.
	FieldKindNumber
	// FieldKindDate is a free-text input expecting YYYY-MM-DD.
	FieldKindDate
	// FieldKindExternalADF is description-only: no in-app textarea. Focusing
	// the row shows a placeholder; the wizard wires Enter / `e` to launch
	// the user's $EDITOR (Task 8 plumbs that through).
	FieldKindExternalADF
)

// dateMask is the date layout the form accepts and the placeholder shown in
// the field. Jira datetime fields are also rendered with this mask — only
// the date part is collected, which is the documented MVP simplification.
const dateMask = "YYYY-MM-DD"

// userDropdown holds the transient state for the FieldKindUser picker dropdown.
// It is zero-valued and harmless for all other field kinds.
type userDropdown struct {
	open      bool
	cursor    int
	results   []jira.User
	loading   bool
	lastQuery string
	token     int
}

// optionPickerVisibleRows is the height of the scroll window in the popup
// option picker rendered for Option/MultiOption fields when the user
// activates them. Picked to match the project picker on Step 1.
const optionPickerVisibleRows = 8

// Field is a single createmeta field paired with widget state. Fields are
// values, copied on Update like other Bubble Tea sub-models in this package.
// The Kind field selects which of text/area/cursor/selected is meaningful;
// callers should not poke at the unrelated members.
type Field struct {
	Meta jira.FieldMeta
	Kind FieldKind

	text     textinput.Model
	area     textarea.Model
	cursor   int             // option / multi-option row cursor
	selected map[string]bool // multi-option selected option IDs

	// FieldKindUser only. accountID holds the value submitted to Jira;
	// the visible textinput shows displayName. Zero means no committed pick.
	accountID string
	dropdown  userDropdown

	// Option/MultiOption popup picker state. Inline rows would explode the
	// wizard height for fields with many allowed values (Quarter etc.), so
	// the field collapses to a one-line summary and only renders the full
	// list while pickerOpen is true.
	pickerOpen   bool
	pickerCursor int
	pickerScroll int
	pickerFilter textinput.Model

	// body holds the markdown body for FieldKindExternalADF, populated by
	// the external editor flow. Always empty for other kinds.
	body string

	focused bool
	err     string // last validation/server error bound to this field
}

// newField constructs a Field of the given kind from FieldMeta and pre-wires
// any embedded widgets so they are immediately usable.
func newField(meta jira.FieldMeta, kind FieldKind) Field {
	f := Field{Meta: meta, Kind: kind}
	switch kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 256
		ti.Width = 48
		switch kind {
		case FieldKindUser:
			ti.Placeholder = "search by name or email"
		case FieldKindNumber:
			ti.Placeholder = "0"
		case FieldKindDate:
			ti.Placeholder = dateMask
		}
		f.text = ti
	case FieldKindADF:
		ta := textarea.New()
		ta.Prompt = ""
		ta.ShowLineNumbers = false
		ta.SetWidth(60)
		ta.SetHeight(6)
		f.area = ta
	case FieldKindMultiOption:
		f.selected = map[string]bool{}
	case FieldKindExternalADF:
		// no embedded widget; body is plain string
	}
	if kind == FieldKindOption || kind == FieldKindMultiOption {
		pf := textinput.New()
		pf.Prompt = "› "
		pf.Placeholder = "filter…"
		pf.CharLimit = 64
		pf.Width = 32
		f.pickerFilter = pf
	}
	return f
}

// detectFieldKind maps the wire schema (per /createmeta) onto a FieldKind.
// Returns FieldKindUnknown for anything not covered by the spec table; the
// caller is expected to skip the field and emit a warning.
func detectFieldKind(meta jira.FieldMeta) FieldKind {
	switch meta.SchemaType {
	case "string":
		if meta.ID == "description" {
			return FieldKindExternalADF
		}
		return FieldKindString
	case "option", "priority", "issuetype":
		return FieldKindOption
	case "user":
		return FieldKindUser
	case "array":
		if meta.SchemaItems == "option" {
			return FieldKindMultiOption
		}
		return FieldKindUnknown
	case "number":
		return FieldKindNumber
	case "date", "datetime":
		return FieldKindDate
	}
	return FieldKindUnknown
}

// Focused reports whether keyboard input is currently routed to f.
func (f Field) Focused() bool { return f.focused }

// Focus puts the field in the focused state and, where applicable, focuses
// the embedded textinput/textarea so its cursor blinks. The returned cmd
// drives the cursor blink.
func (f Field) Focus() (Field, tea.Cmd) {
	f.focused = true
	switch f.Kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		return f, f.text.Focus()
	case FieldKindADF:
		return f, f.area.Focus()
	}
	return f, nil
}

// Blur drops the focused state. If the option-picker popup was open, it is
// closed too — focus moving away has the same semantic as cancelling the
// picker.
func (f Field) Blur() Field {
	f.focused = false
	switch f.Kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		f.text.Blur()
	case FieldKindADF:
		f.area.Blur()
	}
	if f.pickerOpen {
		f = f.closePicker()
	}
	return f
}

// Value returns the field's current string value (mostly for tests / debug).
// Multi-select returns a comma-separated list of selected IDs in input order.
func (f Field) Value() string {
	switch f.Kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		return f.text.Value()
	case FieldKindADF:
		return f.area.Value()
	case FieldKindExternalADF:
		return f.body
	case FieldKindOption:
		if f.cursor < 0 || f.cursor >= len(f.Meta.AllowedValues) {
			return ""
		}
		return f.Meta.AllowedValues[f.cursor].ID
	case FieldKindMultiOption:
		ids := make([]string, 0, len(f.selected))
		for _, opt := range f.Meta.AllowedValues {
			if f.selected[opt.ID] {
				ids = append(ids, opt.ID)
			}
		}
		return strings.Join(ids, ",")
	}
	return ""
}

// SetValue overrides the field's stored value. Useful for tests and for
// re-opening the form with a draft.
func (f Field) SetValue(s string) Field {
	switch f.Kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		f.text.SetValue(s)
	case FieldKindADF:
		f.area.SetValue(s)
	case FieldKindExternalADF:
		f.body = s
	case FieldKindOption:
		for i, opt := range f.Meta.AllowedValues {
			if opt.ID == s {
				f.cursor = i
				return f
			}
		}
	case FieldKindMultiOption:
		f.selected = map[string]bool{}
		for id := range strings.SplitSeq(s, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			f.selected[id] = true
		}
	}
	return f
}

// SetError attaches a per-field error string. Cleared on next successful
// validation. Empty s clears the error.
func (f Field) SetError(s string) Field {
	f.err = s
	return f
}

// Error returns the field's bound error message, if any.
func (f Field) Error() string { return f.err }

// Update routes a Bubble Tea message to the field's widget. The form's
// Update handles Tab/Shift+Tab before delegating here, so f never sees
// focus-changing keys.
func (f Field) Update(msg tea.Msg) (Field, tea.Cmd) {
	if !f.focused {
		return f, nil
	}
	switch f.Kind {
	case FieldKindString, FieldKindUser, FieldKindNumber, FieldKindDate:
		var cmd tea.Cmd
		f.text, cmd = f.text.Update(msg)
		return f, cmd
	case FieldKindADF:
		var cmd tea.Cmd
		f.area, cmd = f.area.Update(msg)
		return f, cmd
	case FieldKindOption, FieldKindMultiOption:
		k, ok := msg.(tea.KeyMsg)
		if !ok {
			return f, nil
		}
		if !f.pickerOpen {
			if k.String() == "enter" {
				f = f.openPicker()
			}
			return f, nil
		}
		return f.updatePicker(k), nil
	case FieldKindExternalADF:
		k, ok := msg.(tea.KeyMsg)
		if !ok {
			return f, nil
		}
		if k.String() == "enter" || k.String() == "e" {
			body := f.body
			return f, func() tea.Msg {
				return OpenExternalEditorRequestMsg{Body: body}
			}
		}
		return f, nil
	}
	return f, nil
}

// View renders a label + control for the field. Required fields get a red
// asterisk; bound errors are shown below the control.
func (f Field) View(s styles.Styles) string {
	label := f.Meta.Name
	if label == "" {
		label = f.Meta.ID
	}
	if f.Meta.Required {
		label += " " + s.Error.Render("*")
	}
	if f.focused {
		label = s.Accent.Render(label)
	} else {
		label = s.Muted.Render(label)
	}

	body := f.viewControl(s)
	parts := []string{label, body}
	if f.err != "" {
		parts = append(parts, s.Error.Render(f.err))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (f Field) viewControl(s styles.Styles) string {
	switch f.Kind {
	case FieldKindString, FieldKindNumber, FieldKindDate:
		return f.text.View()
	case FieldKindUser:
		base := f.text.View()
		if f.dropdown.open {
			var rows []string
			switch {
			case f.dropdown.loading:
				rows = append(rows, s.Muted.Render("  …"))
			case len(f.dropdown.results) == 0:
				rows = append(rows, s.Muted.Render("  (no matches)"))
			default:
				for i, u := range f.dropdown.results {
					row := "  " + u.DisplayName
					if u.Email != "" {
						row += s.Muted.Render("  <" + u.Email + ">")
					}
					if i == f.dropdown.cursor {
						row = s.ListItemSelected.Render(row)
					} else {
						row = s.ListItem.Render(row)
					}
					rows = append(rows, row)
				}
			}
			base = lipgloss.JoinVertical(lipgloss.Left, append([]string{base}, rows...)...)
		}
		return base
	case FieldKindADF:
		return f.area.View()
	case FieldKindExternalADF:
		return externalADFPreview(f.body)
	case FieldKindOption:
		return f.viewOptionCompact(s, false)
	case FieldKindMultiOption:
		return f.viewOptionCompact(s, true)
	}
	return ""
}

// viewOptionCompact renders a single-line summary of the current selection.
// The full list lives in the popup picker (see PickerView) so the wizard
// height stays bounded for fields with many allowedValues.
func (f Field) viewOptionCompact(s styles.Styles, multi bool) string {
	if len(f.Meta.AllowedValues) == 0 {
		return s.Muted.Render("(no options)")
	}
	var summary string
	switch {
	case !multi:
		if f.cursor >= 0 && f.cursor < len(f.Meta.AllowedValues) {
			summary = "● " + f.Meta.AllowedValues[f.cursor].Name
		}
	default:
		names := make([]string, 0, len(f.selected))
		for _, opt := range f.Meta.AllowedValues {
			if f.selected[opt.ID] {
				names = append(names, opt.Name)
			}
		}
		if len(names) > 0 {
			summary = "[" + strconv.Itoa(len(names)) + "] " + strings.Join(names, ", ")
		}
	}
	if f.focused {
		hint := s.Muted.Render("· enter to pick")
		if summary == "" {
			return hint
		}
		return s.ListItemSelected.Render(summary) + " " + hint
	}
	if summary == "" {
		return ""
	}
	return s.ListItem.Render(summary)
}

// openPicker returns f with the popup picker activated. The picker filter is
// reset, the cursor is seeded from the current selection (single) or the
// first row (multi), and the filter input is focused so typing filters.
func (f Field) openPicker() Field {
	f.pickerOpen = true
	f.pickerFilter.Reset()
	f.pickerFilter.Focus() //nolint:errcheck
	switch f.Kind {
	case FieldKindOption:
		f.pickerCursor = f.cursor
	case FieldKindMultiOption:
		f.pickerCursor = 0
	}
	f.pickerScroll = 0
	f.clampPickerScroll(len(f.filteredAllowed()))
	return f
}

// closePicker hides the popup. For single-option fields, the picker's
// committed cursor is folded into f.cursor on enter (see updatePicker); esc
// reverts to the previously committed value.
func (f Field) closePicker() Field {
	f.pickerOpen = false
	f.pickerFilter.Blur()
	f.pickerFilter.Reset()
	f.pickerCursor = 0
	f.pickerScroll = 0
	return f
}

// PickerOpen reports whether the option/multi-option popup is currently
// shown for this field. Always false for other kinds.
func (f Field) PickerOpen() bool { return f.pickerOpen }

// filteredAllowed returns the AllowedValues whose Name matches the current
// picker filter (case-insensitive substring). Returns the full slice when
// the filter is empty.
func (f Field) filteredAllowed() []jira.FieldOption {
	q := strings.TrimSpace(f.pickerFilter.Value())
	if q == "" {
		return f.Meta.AllowedValues
	}
	needle := strings.ToLower(q)
	out := make([]jira.FieldOption, 0, len(f.Meta.AllowedValues))
	for _, opt := range f.Meta.AllowedValues {
		if strings.Contains(strings.ToLower(opt.Name), needle) {
			out = append(out, opt)
		}
	}
	return out
}

// updatePicker handles a keystroke while the popup is open. Up/Down move
// the picker cursor; Space toggles selection for multi-option; Enter commits
// (single) or closes (multi); Esc closes without changing the committed
// selection. All other keys are forwarded to the filter input.
func (f Field) updatePicker(k tea.KeyMsg) Field {
	switch k.String() {
	case "esc":
		return f.closePicker()
	case "up", "ctrl+p":
		if f.pickerCursor > 0 {
			f.pickerCursor--
		}
		f.clampPickerScroll(len(f.filteredAllowed()))
		return f
	case "down", "ctrl+n":
		if f.pickerCursor < len(f.filteredAllowed())-1 {
			f.pickerCursor++
		}
		f.clampPickerScroll(len(f.filteredAllowed()))
		return f
	case "enter":
		filtered := f.filteredAllowed()
		if len(filtered) == 0 {
			return f
		}
		if f.pickerCursor >= len(filtered) {
			f.pickerCursor = len(filtered) - 1
		}
		opt := filtered[f.pickerCursor]
		if f.Kind == FieldKindOption {
			for i, o := range f.Meta.AllowedValues {
				if o.ID == opt.ID {
					f.cursor = i
					break
				}
			}
			return f.closePicker()
		}
		// Multi: enter toggles the current row and stays open so the user
		// can pick several without re-opening; a second close (esc) commits.
		if f.selected == nil {
			f.selected = map[string]bool{}
		}
		f.selected[opt.ID] = !f.selected[opt.ID]
		return f
	case " ", "space":
		if f.Kind != FieldKindMultiOption {
			return f
		}
		filtered := f.filteredAllowed()
		if len(filtered) == 0 || f.pickerCursor >= len(filtered) {
			return f
		}
		id := filtered[f.pickerCursor].ID
		if f.selected == nil {
			f.selected = map[string]bool{}
		}
		f.selected[id] = !f.selected[id]
		return f
	}
	old := f.pickerFilter.Value()
	f.pickerFilter, _ = f.pickerFilter.Update(k)
	if f.pickerFilter.Value() != old {
		f.pickerCursor = 0
		f.pickerScroll = 0
	}
	return f
}

// clampPickerScroll keeps pickerScroll so pickerCursor stays inside an
// optionPickerVisibleRows-row window over the filtered list. Called after
// every cursor or filter mutation.
func (f *Field) clampPickerScroll(n int) {
	if n == 0 {
		f.pickerScroll = 0
		return
	}
	if f.pickerScroll < 0 {
		f.pickerScroll = 0
	}
	maxScroll := n - optionPickerVisibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if f.pickerScroll > maxScroll {
		f.pickerScroll = maxScroll
	}
	if f.pickerCursor < f.pickerScroll {
		f.pickerScroll = f.pickerCursor
	}
	if f.pickerCursor >= f.pickerScroll+optionPickerVisibleRows {
		f.pickerScroll = f.pickerCursor - optionPickerVisibleRows + 1
	}
}

// PickerView renders the popup picker box for the focused option/multi-
// option field. Returns "" when no picker is open. The caller composes the
// returned string over the wizard view (see Create.viewFieldsStep).
func (f Field) PickerView(s styles.Styles) string {
	if !f.pickerOpen {
		return ""
	}
	multi := f.Kind == FieldKindMultiOption
	title := f.Meta.Name
	if title == "" {
		title = f.Meta.ID
	}
	header := s.OverlayTitle.Render(title)
	filtered := f.filteredAllowed()
	start := f.pickerScroll
	end := start + optionPickerVisibleRows
	if end > len(filtered) {
		end = len(filtered)
	}
	rows := make([]string, 0, end-start+2)
	if len(filtered) == 0 {
		rows = append(rows, s.Muted.Render("(no matches)"))
	} else {
		if start > 0 {
			rows = append(rows, s.Muted.Render("↑ "+strconv.Itoa(start)+" more"))
		}
		for i := start; i < end; i++ {
			opt := filtered[i]
			var marker string
			switch {
			case multi && f.selected[opt.ID]:
				marker = "[x] "
			case multi:
				marker = "[ ] "
			case f.cursor < len(f.Meta.AllowedValues) && f.Meta.AllowedValues[f.cursor].ID == opt.ID:
				marker = "● "
			default:
				marker = "○ "
			}
			label := marker + opt.Name
			if i == f.pickerCursor {
				rows = append(rows, s.ListItemSelected.Render(label))
			} else {
				rows = append(rows, s.ListItem.Render(label))
			}
		}
		if end < len(filtered) {
			rows = append(rows, s.Muted.Render("↓ "+strconv.Itoa(len(filtered)-end)+" more"))
		}
	}
	var hintText string
	if multi {
		hintText = "↑/↓ nav  space/enter toggle  esc done"
	} else {
		hintText = "↑/↓ nav  enter pick  esc cancel"
	}
	hint := s.Muted.Render(hintText)
	parts := []string{header, "", f.pickerFilter.View(), "", lipgloss.JoinVertical(lipgloss.Left, rows...), "", hint}
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// externalADFPreview returns a one-line preview of an external-ADF body
// for the placeholder row. Empty body shows the prompt; multi-line bodies
// show the first non-empty line, ellipsised at 50 runes, suffixed with
// " · N lines" when N > 1.
func externalADFPreview(body string) string {
	if strings.TrimSpace(body) == "" {
		return "(empty — Enter to edit)"
	}
	lines := strings.Split(body, "\n")
	first := ""
	count := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			if first == "" {
				first = strings.TrimSpace(l)
			}
			count++
		}
	}
	if first == "" {
		return "(empty — Enter to edit)"
	}
	const maxRune = 50
	rs := []rune(first)
	if len(rs) > maxRune {
		first = string(rs[:maxRune-1]) + "…"
	}
	if count > 1 {
		first += "  · " + strconv.Itoa(count) + " lines"
	}
	return first
}

// SetExternalBody overrides the body of a FieldKindExternalADF field. No-op
// for other kinds.
func (f Field) SetExternalBody(s string) Field {
	if f.Kind == FieldKindExternalADF {
		f.body = s
	}
	return f
}

// ExternalBody returns the stored markdown body. "" for non-external kinds.
func (f Field) ExternalBody() string { return f.body }

// Validate returns a non-empty error string when the field's current value
// fails its constraints (required, numeric, date format). An empty return
// means the field is OK.
func (f Field) Validate() string {
	val := strings.TrimSpace(f.Value())
	if f.Meta.Required && val == "" {
		return "required"
	}
	if val == "" {
		return ""
	}
	switch f.Kind {
	case FieldKindNumber:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return "must be a number"
		}
	case FieldKindDate:
		if _, err := time.Parse("2006-01-02", val); err != nil {
			return "must be " + dateMask
		}
	}
	return ""
}

// SetUserSelection commits a user pick: stores the accountID and sets the
// visible textinput to the display name. Closes the dropdown.
func (f *Field) SetUserSelection(u jira.User) {
	f.accountID = u.AccountID
	f.text.SetValue(u.DisplayName)
	f.dropdown.open = false
	f.dropdown.results = nil
}

// UserAccountID returns the committed accountID for FieldKindUser, or "".
func (f Field) UserAccountID() string { return f.accountID }

// IsMultiSelected reports whether optionID is currently checked in a
// multi-option field. Always false for other kinds. Exposed so callers
// outside this package (e.g. usage trackers) can read the committed
// selection without poking at the unexported map.
func (f Field) IsMultiSelected(optionID string) bool {
	if f.Kind != FieldKindMultiOption || f.selected == nil {
		return false
	}
	return f.selected[optionID]
}

// OnTextChanged is called by the form Update path after the textinput value
// mutates. Clears the stored accountID — any edit invalidates the previous
// pick (the user will select a new row from the dropdown).
func (f *Field) OnTextChanged() {
	if f.accountID != "" {
		f.accountID = ""
	}
}

// OpenExternalEditorRequestMsg is emitted by a focused FieldKindExternalADF
// row when the user requests the external editor. The root model forwards
// it back to the visible Create overlay, which combines it with the form's
// current summary value before emitting CreateOpenEditorMsg.
//
// It is exported so the root model's Update can route the message — Field
// emits messages but the wizard's updateFieldsStep is what understands them
// in context, so the message must travel through tea's event loop and back.
type OpenExternalEditorRequestMsg struct {
	Body string
}

// UserSearchRequestMsg is dispatched by a debounce timer; the create
// overlay translates it into a Client.SearchUsers call.
type UserSearchRequestMsg struct {
	FieldID string
	Query   string
	Token   int
}

// UserSearchResultsMsg carries SearchUsers output back to the form.
// Stale tokens are dropped silently (cancel-on-supersede).
type UserSearchResultsMsg struct {
	FieldID string
	Token   int
	Users   []jira.User
	Err     error
}

// UpdateUser handles key events on a user field: typing rearms the
// debounce timer; ↑/↓ navigate the dropdown; enter commits the cursor.
// Other dropdown-related navigation (esc to close picker, etc.) is
// intentionally minimal — esc is owned by the form's outer flow.
func (f Field) UpdateUser(msg tea.Msg) (Field, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	if f.dropdown.open && len(f.dropdown.results) > 0 {
		switch k.String() {
		case "up", "ctrl+p":
			if f.dropdown.cursor > 0 {
				f.dropdown.cursor--
			}
			return f, nil
		case "down", "ctrl+n":
			if f.dropdown.cursor < len(f.dropdown.results)-1 {
				f.dropdown.cursor++
			}
			return f, nil
		case "enter":
			if f.dropdown.cursor < len(f.dropdown.results) {
				u := f.dropdown.results[f.dropdown.cursor]
				f.SetUserSelection(u)
			}
			return f, nil
		}
	}
	// Ensure the textinput processes the keystroke regardless of its focus
	// state — UpdateUser is only called when the field itself is focused.
	if !f.text.Focused() {
		f.text.Focus() //nolint:errcheck
	}
	var cmd tea.Cmd
	f.text, cmd = f.text.Update(msg)
	f.OnTextChanged()
	q := strings.TrimSpace(f.text.Value())
	if q == "" {
		f.dropdown.open = false
		f.dropdown.results = nil
		return f, cmd
	}
	f.dropdown.open = true
	f.dropdown.token++
	token := f.dropdown.token
	fieldID := f.Meta.ID
	debounce := tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return UserSearchRequestMsg{FieldID: fieldID, Query: q, Token: token}
	})
	return f, tea.Batch(cmd, debounce)
}

// handleSearchResults swaps the dropdown's results in if the token
// matches; otherwise drops them.
func (f *Field) handleSearchResults(msg UserSearchResultsMsg) {
	if msg.Token != f.dropdown.token {
		return
	}
	f.dropdown.results = msg.Users
	f.dropdown.cursor = 0
	f.dropdown.loading = false
}

// SubmitValue returns the value to submit for this field, using accountID for
// FieldKindUser (only set after an explicit pick via SetUserSelection).
func (f Field) SubmitValue() (any, bool) { return f.PayloadValue() }

// PayloadValue returns the JSON-shaped value to submit for this field.
// Empty/zero values return (nil, false) so callers can omit the field from
// the request body. The shapes follow Jira's REST v3 expectations.
func (f Field) PayloadValue() (any, bool) {
	val := strings.TrimSpace(f.Value())
	if val == "" {
		return nil, false
	}
	switch f.Kind {
	case FieldKindString, FieldKindADF:
		if f.Kind == FieldKindADF {
			return val, true // create.go converts the description string itself
		}
		return val, true
	case FieldKindExternalADF:
		return val, true
	case FieldKindOption:
		return map[string]string{"id": val}, true
	case FieldKindUser:
		if f.accountID == "" {
			return nil, false
		}
		return map[string]string{"accountId": f.accountID}, true
	case FieldKindMultiOption:
		ids := make([]map[string]string, 0)
		for _, opt := range f.Meta.AllowedValues {
			if f.selected[opt.ID] {
				ids = append(ids, map[string]string{"id": opt.ID})
			}
		}
		if len(ids) == 0 {
			return nil, false
		}
		return ids, true
	case FieldKindNumber:
		n, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, false
		}
		return n, true
	case FieldKindDate:
		return val, true
	}
	return nil, false
}

// Form is the dynamic-fields step of the create overlay. It holds the live
// fields produced by BuildForm plus a focused-row cursor and the list of
// schema warnings (unknown-type field names) for footer rendering.
type Form struct {
	Fields   []Field
	warnings []string
	focus    int
}

// FormDefaults seeds BuildForm with values that depend on the runtime
// (current user, etc.) rather than the static createmeta response.
type FormDefaults struct {
	CurrentUserAccountID string

	// DefaultPriorityName, when non-empty, pre-selects the priority field's
	// option whose Name matches (case-insensitive). If no such option is
	// found in the createmeta allowedValues, the default cursor is left
	// untouched.
	DefaultPriorityName string

	// OptionUsage carries the historic per-option usage counts for the
	// active (project, issue-type) pair, keyed by fieldID → optionID →
	// count. BuildForm uses it to stable-sort each Option/MultiOption
	// field's allowed values by count desc, and to seed the cursor on
	// single-option fields to the most-used choice. nil/empty means
	// "no history" and the schema order is preserved.
	OptionUsage map[string]map[string]int
}

// sortAllowedByUsage reorders opts so high-count entries come first. The
// sort is stable; entries with the same count keep their schema-relative
// order so the user's habit doesn't shuffle equal-priority items around
// between sessions.
func sortAllowedByUsage(opts []jira.FieldOption, counts map[string]int) {
	sort.SliceStable(opts, func(i, j int) bool {
		return counts[opts[i].ID] > counts[opts[j].ID]
	})
}

// reorderFields pulls summary then description to the front while preserving
// the relative order of everything else. The payload sent to Jira is keyed by
// field ID, so reordering only affects rendering.
func reorderFields(in []Field) []Field {
	if len(in) < 2 {
		return in
	}
	out := make([]Field, 0, len(in))
	rest := make([]Field, 0, len(in))
	var sum, desc *Field
	for i := range in {
		switch in[i].Meta.ID {
		case "summary":
			f := in[i]
			sum = &f
		case "description":
			f := in[i]
			desc = &f
		default:
			rest = append(rest, in[i])
		}
	}
	if sum != nil {
		out = append(out, *sum)
	}
	if desc != nil {
		out = append(out, *desc)
	}
	out = append(out, rest...)
	return out
}

// BuildForm classifies meta.Fields, drops any with FieldKindUnknown, focuses
// the first remaining field, and returns the warnings list for the footer.
func BuildForm(meta jira.CreateMeta, defaults FormDefaults) Form {
	fields := make([]Field, 0, len(meta.Fields))
	warnings := make([]string, 0)
	for _, fm := range meta.Fields {
		kind := detectFieldKind(fm)
		if kind == FieldKindUnknown {
			label := fm.Name
			if label == "" {
				label = fm.ID
			}
			warnings = append(warnings, label)
			continue
		}
		f := newField(fm, kind)
		if (kind == FieldKindOption || kind == FieldKindMultiOption) && len(f.Meta.AllowedValues) > 1 {
			counts := defaults.OptionUsage[fm.ID]
			if len(counts) > 0 {
				sortAllowedByUsage(f.Meta.AllowedValues, counts)
			}
			if kind == FieldKindOption && len(counts) > 0 {
				// Seed the cursor on the most-used option (now at index 0
				// after the sort). Avoids forcing the user to re-pick the
				// same thing each create.
				f.cursor = 0
			}
		}
		fields = append(fields, f)
	}
	fields = reorderFields(fields)
	form := Form{Fields: fields, warnings: warnings}
	if len(fields) > 0 {
		focused, _ := fields[0].Focus()
		form.Fields[0] = focused
	}

	if defaults.CurrentUserAccountID != "" {
		for i := range form.Fields {
			f := &form.Fields[i]
			if f.Kind == FieldKindUser && f.Meta.ID == "reporter" {
				f.SetUserSelection(jira.User{
					AccountID:   defaults.CurrentUserAccountID,
					DisplayName: "(me)",
				})
				break
			}
		}
	}

	if want := strings.TrimSpace(defaults.DefaultPriorityName); want != "" {
		for i := range form.Fields {
			f := &form.Fields[i]
			if f.Kind != FieldKindOption || f.Meta.ID != "priority" {
				continue
			}
			for j, opt := range f.Meta.AllowedValues {
				if strings.EqualFold(opt.Name, want) {
					f.cursor = j
					break
				}
			}
			break
		}
	}

	return form
}

// Focus returns the index of the currently-focused field, or -1 if the form
// has no focusable fields.
func (f Form) Focus() int {
	if len(f.Fields) == 0 {
		return -1
	}
	return f.focus
}

// Warnings returns the labels of fields that were skipped because their
// schema did not map to a supported widget.
func (f Form) Warnings() []string {
	return append([]string(nil), f.warnings...)
}

// FocusNext moves focus to the next field, wrapping at the end. The returned
// cmd drives the new field's cursor blink (if it has one).
func (f Form) FocusNext() (Form, tea.Cmd) {
	return f.focusDelta(1)
}

// FocusPrev moves focus to the previous field, wrapping at the start.
func (f Form) FocusPrev() (Form, tea.Cmd) {
	return f.focusDelta(-1)
}

func (f Form) focusDelta(delta int) (Form, tea.Cmd) {
	n := len(f.Fields)
	if n == 0 {
		return f, nil
	}
	f.Fields[f.focus] = f.Fields[f.focus].Blur()
	f.focus = ((f.focus+delta)%n + n) % n
	focused, cmd := f.Fields[f.focus].Focus()
	f.Fields[f.focus] = focused
	return f, cmd
}

// Update consumes a tea.Msg and routes it through the form. Tab/Shift+Tab
// move focus between fields; everything else goes to the focused field.
// While a focused field has an open picker popup, Tab/Shift+Tab are routed
// to the field too so they don't accidentally leak focus out from under
// the user's selection.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	pickerOpen := len(f.Fields) > 0 && f.Fields[f.focus].PickerOpen()
	if k, ok := msg.(tea.KeyMsg); ok && !pickerOpen {
		switch k.Type {
		case tea.KeyTab:
			return f.FocusNext()
		case tea.KeyShiftTab:
			return f.FocusPrev()
		}
	}
	if len(f.Fields) == 0 {
		return f, nil
	}
	updated, cmd := f.Fields[f.focus].Update(msg)
	f.Fields[f.focus] = updated
	return f, cmd
}

// View renders the fields stacked vertically with a footer listing any
// warnings about skipped fields.
func (f Form) View(s styles.Styles) string {
	if len(f.Fields) == 0 {
		return s.Muted.Render("(no fields)")
	}
	rows := make([]string, 0, len(f.Fields)+1)
	for _, fld := range f.Fields {
		rows = append(rows, fld.View(s))
	}
	if len(f.warnings) > 0 {
		rows = append(rows,
			s.Muted.Render("Skipped unsupported fields: "+strings.Join(f.warnings, ", ")),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// Validate returns a map of field ID → error message for fields that fail
// validation. The returned map is empty when the form is valid. Errors are
// also bound to the corresponding fields so View can render them inline;
// the returned Form must be installed back on the overlay.
func (f Form) Validate() (Form, map[string]string) {
	out := map[string]string{}
	for i, fld := range f.Fields {
		err := fld.Validate()
		f.Fields[i] = fld.SetError(err)
		if err != "" {
			out[fld.Meta.ID] = err
		}
	}
	return f, out
}

// PayloadValues returns the per-field map ready to be merged into the
// CreatePayload's Fields. Standard fields with first-class slots on
// CreatePayload (summary, description, priority, assignee, labels) are
// extracted by BuildPayload — PayloadValues returns *all* non-empty fields
// keyed by Jira field ID and is the lower-level building block.
func (f Form) PayloadValues() map[string]any {
	out := map[string]any{}
	for _, fld := range f.Fields {
		v, ok := fld.PayloadValue()
		if !ok {
			continue
		}
		out[fld.Meta.ID] = v
	}
	return out
}

// BuildPayload assembles a jira.CreatePayload from the form. Project key and
// issue type id are caller-supplied because they live on the overlay shell,
// not in the form. Standard fields are unpacked into typed slots; everything
// else is forwarded under Fields verbatim.
func (f Form) BuildPayload(projectKey, issueTypeID string) jira.CreatePayload {
	p := jira.CreatePayload{ProjectKey: projectKey, IssueTypeID: issueTypeID}
	extra := map[string]any{}
	for _, fld := range f.Fields {
		v, ok := fld.PayloadValue()
		if !ok {
			continue
		}
		switch fld.Meta.ID {
		case "summary":
			if s, ok := v.(string); ok {
				p.Summary = s
			}
		case "description":
			if s, ok := v.(string); ok {
				p.Description = s
			}
		case "priority":
			if m, ok := v.(map[string]string); ok {
				p.Priority = m["id"]
			}
		case "assignee":
			if m, ok := v.(map[string]string); ok {
				p.Assignee = m["accountId"]
			}
		case "labels":
			// labels are not currently produced (array<string> is unknown),
			// but accept a string-slice if a future kind emits one.
			if ss, ok := v.([]string); ok {
				p.Labels = ss
			}
		default:
			extra[fld.Meta.ID] = v
		}
	}
	if len(extra) > 0 {
		p.Fields = extra
	}
	return p
}

// describeFieldKind returns a short human label for FieldKind. Useful for
// debug rendering and tests.
func describeFieldKind(k FieldKind) string {
	switch k {
	case FieldKindString:
		return "string"
	case FieldKindADF:
		return "adf"
	case FieldKindOption:
		return "option"
	case FieldKindUser:
		return "user"
	case FieldKindMultiOption:
		return "multi-option"
	case FieldKindNumber:
		return "number"
	case FieldKindDate:
		return "date"
	case FieldKindExternalADF:
		return "external-adf"
	}
	return "unknown"
}

// String implements fmt.Stringer for FieldKind so test assertions can print
// readable diagnostics.
func (k FieldKind) String() string { return describeFieldKind(k) }
