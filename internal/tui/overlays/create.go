package overlays

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// CreateProjectChosenMsg is published when the user advances past Step 1 by
// pressing enter on a project. The root model is expected to dispatch the
// issue-types fetch for this project and reply with CreateIssueTypesMsg.
type CreateProjectChosenMsg struct {
	ProjectKey string
}

// CreateIssueTypesMsg carries the issue types available for ProjectKey. Stale
// messages (the user backed out of step 2 before the fetch resolved) are
// ignored by the overlay via a simple project-key match.
type CreateIssueTypesMsg struct {
	ProjectKey string
	IssueTypes []jira.IssueType
	Err        error
}

// CreateTypeChosenMsg is published when the user advances past Step 2 by
// pressing enter on an issue type. The root model handles it by fetching
// /createmeta for the (project, type) pair and replying with a
// CreateMetaLoadedMsg so Step 3 can render the dynamic form.
type CreateTypeChosenMsg struct {
	ProjectKey string
	IssueType  jira.IssueType
}

// CreateMetaLoadedMsg carries the createmeta result for ProjectKey + IssueTypeID
// back to the overlay. Stale messages (the user backed out before the fetch
// resolved) are dropped via a project+type match.
type CreateMetaLoadedMsg struct {
	ProjectKey  string
	IssueTypeID string
	Meta        jira.CreateMeta
	Err         error
}

// CreateCancelledMsg is published when the user closes the overlay from
// Step 1 without making a selection. Allows the root model to clear any
// "loading projects" state if it was tracking one.
type CreateCancelledMsg struct{}

// CreateOpenEditorMsg is published by the description row in the create
// wizard when the user requests the external editor. The root model
// dispatches editor.Open and routes the resulting editor.ClosedMsg back
// to the wizard via Create.HandleEditorClosed.
type CreateOpenEditorMsg struct {
	Summary string
	Body    string
	Title   string
	Token   int
}

// CreateSubmitRequestedMsg is published when the user presses Ctrl+S on a
// validated Step 3 form. The root model dispatches CreateIssue with Payload
// and replies with CreateSubmitDoneMsg.
type CreateSubmitRequestedMsg struct {
	ProjectKey  string
	IssueTypeID string
	Payload     jira.CreatePayload
}

// CreateSubmitDoneMsg carries the result of a CreateIssue network call back
// to the overlay. On success the overlay closes; on error it stays open and
// either binds per-field errors (400) or surfaces a generic message.
type CreateSubmitDoneMsg struct {
	ProjectKey  string
	IssueTypeID string
	Issue       jira.Issue
	Err         error
}

// createMode is the overlay's lifecycle: hidden, picking a project, or
// picking an issue type.
type createMode int

const (
	createHidden createMode = iota
	createStepProject
	createStepType
	createStepFields
)

// defaultIssueTypeName is the issue type to pre-select on Step 2 when it is
// present in the list. The spec calls out Task as the default.
const defaultIssueTypeName = "Task"

// projectVisibleRows is the height of the scroll window in the project
// picker. Picked to fit comfortably in a 24-row terminal alongside the
// title, input, hint, and overlay border.
const projectVisibleRows = 10

// Create is the `n` overlay shell: a two-step wizard for the issue-creation
// flow. Step 1 is a filterable project picker (defaulting to the user's
// configured default_project); Step 2 is a filterable issue-type picker
// (defaulting to Task). Subsequent tasks add a dynamic-fields step and
// submission.
type Create struct {
	mode createMode

	projects          []jira.Project
	defaultProjectKey string

	projectInput  textinput.Model
	projectCursor int
	projectScroll int

	selectedProject jira.Project

	issueTypes   []jira.IssueType
	typesLoading bool
	typesErr     error

	typeInput  textinput.Model
	typeCursor int

	selectedType jira.IssueType

	form        Form
	formLoading bool
	formErr     error
	formReady   bool

	submitting bool
	submitErr  string

	// pendingEditorToken increments each time the wizard dispatches the
	// external editor. Stale ClosedMsg results are dropped via token mismatch.
	pendingEditorToken int

	metaCache map[string]jira.CreateMeta

	closeBinding key.Binding

	parentKey            string
	subtaskMode          bool
	currentUserAccountID string
}

// metaCacheKey builds the key used to look up cached createmeta results.
// The NUL separator avoids collisions between project keys and type IDs that
// happen to share characters when concatenated.
func metaCacheKey(projectKey, issueTypeID string) string {
	return projectKey + "\x00" + issueTypeID
}

// NewCreate constructs a hidden Create overlay. closeKey is the key used to
// step backwards / close (typically `esc`). currentUserAccountID is used to
// pre-fill reporter fields via BuildForm; pass "" when not yet available.
func NewCreate(closeKey key.Binding, currentUserAccountID string) Create {
	pi := textinput.New()
	pi.Placeholder = "Filter projects…"
	pi.Prompt = "› "
	pi.CharLimit = 64
	pi.Width = 40

	ti := textinput.New()
	ti.Placeholder = "Filter issue types…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	ti.Width = 40

	return Create{
		projectInput:         pi,
		typeInput:            ti,
		closeBinding:         closeKey,
		metaCache:            make(map[string]jira.CreateMeta),
		currentUserAccountID: currentUserAccountID,
	}
}

// SetCurrentUserAccountID returns a copy of c with currentUserAccountID set.
// Call this after the bootstrap finishes and the account ID becomes available.
func (c Create) SetCurrentUserAccountID(id string) Create {
	c.currentUserAccountID = id
	return c
}

// Visible reports whether the overlay is currently shown.
func (c Create) Visible() bool { return c.mode != createHidden }

// Step reports the wizard step the overlay is currently on (1 for project,
// 2 for issue type, 3 for dynamic fields, 0 for hidden).
func (c Create) Step() int {
	switch c.mode {
	case createStepProject:
		return 1
	case createStepType:
		return 2
	case createStepFields:
		return 3
	}
	return 0
}

// Projects returns the currently-displayed (unfiltered) project list.
func (c Create) Projects() []jira.Project { return c.projects }

// IssueTypes returns the currently-displayed (unfiltered) issue type list.
func (c Create) IssueTypes() []jira.IssueType { return c.issueTypes }

// SelectedProject returns the project chosen on Step 1. Zero value before
// Step 1 has been confirmed.
func (c Create) SelectedProject() jira.Project { return c.selectedProject }

// SelectedProjectKey is a thin convenience over SelectedProject.Key.
func (c Create) SelectedProjectKey() string { return c.selectedProject.Key }

// FilteredProjects returns the projects matching the current Step 1 filter.
func (c Create) FilteredProjects() []jira.Project {
	return filterProjects(c.projects, c.projectInput.Value())
}

// FilteredIssueTypes returns the issue types matching the current Step 2
// filter. In subtask mode the result is additionally restricted to types
// where Subtask == true.
func (c Create) FilteredIssueTypes() []jira.IssueType {
	out := filterIssueTypes(c.issueTypes, c.typeInput.Value())
	if !c.subtaskMode {
		return out
	}
	subs := make([]jira.IssueType, 0, len(out))
	for _, it := range out {
		if it.Subtask {
			subs = append(subs, it)
		}
	}
	return subs
}

// IsSubtaskMode reports whether the overlay was opened via ShowAsSubtask.
func (c Create) IsSubtaskMode() bool { return c.subtaskMode }

// ParentKey returns the parent issue key for subtask mode, "" otherwise.
func (c Create) ParentKey() string { return c.parentKey }

// PayloadParentKey returns the ParentKey that will be carried into the
// next CreateSubmitRequestedMsg.
func (c Create) PayloadParentKey() string { return c.parentKey }

// ProjectCursor returns the cursor index into the (filtered) project list.
func (c Create) ProjectCursor() int { return c.projectCursor }

// ProjectScroll returns the index of the topmost visible row in the
// project picker window.
func (c Create) ProjectScroll() int { return c.projectScroll }

// ProjectInputValue returns the current Step-1 filter input.
func (c Create) ProjectInputValue() string { return c.projectInput.Value() }

// SetProjectInputForTest replaces the filter input value without going
// through the textinput's Update flow. Tests only.
func (c *Create) SetProjectInputForTest(v string) {
	c.projectInput.SetValue(v)
	c.projectCursor = 0
	c.projectScroll = 0
}

// TypeCursor returns the cursor index into the (filtered) issue type list.
func (c Create) TypeCursor() int { return c.typeCursor }

// TypesLoading reports whether the overlay is currently waiting for a
// CreateIssueTypesMsg. Step 2 renders a "Loading…" placeholder while true.
func (c Create) TypesLoading() bool { return c.typesLoading }

// TypesErr returns the most recent issue-type fetch error, if any.
func (c Create) TypesErr() error { return c.typesErr }

// Show binds c to the given project list and opens Step 1. The cursor is
// pre-positioned on defaultProjectKey when present in projects; otherwise
// at the first row. The returned cmd starts the textinput's cursor blink.
func (c Create) Show(projects []jira.Project, defaultProjectKey string) (Create, tea.Cmd) {
	c.mode = createStepProject
	c.projects = append([]jira.Project(nil), projects...)
	c.defaultProjectKey = defaultProjectKey
	c.projectInput.Reset()
	c.projectCursor = indexOfProjectKey(c.projects, defaultProjectKey)
	c.projectScroll = 0
	c.clampProjectScroll()
	c.selectedProject = jira.Project{}
	c.selectedType = jira.IssueType{}
	c.issueTypes = nil
	c.typeInput.Reset()
	c.typeCursor = 0
	c.typesLoading = false
	c.typesErr = nil
	c.typeInput.Blur()
	c.form = Form{}
	c.formLoading = false
	c.formErr = nil
	c.formReady = false
	c.submitting = false
	c.submitErr = ""
	cmd := c.projectInput.Focus()
	return c, cmd
}

// ShowAsSubtask opens the wizard at Step 2 (issue type picker) pre-filled
// with the parent issue's project. Issue types are filtered to those
// flagged Subtask=true. The eventual CreateSubmitRequestedMsg carries
// ParentKey = parent.Key so the server creates the issue as a child.
func (c Create) ShowAsSubtask(parent jira.Issue, projects []jira.Project) (Create, tea.Cmd) {
	projectKey := projectKeyFromIssue(parent.Key)
	var selected jira.Project
	for _, p := range projects {
		if p.Key == projectKey {
			selected = p
			break
		}
	}
	c.mode = createStepType
	c.projects = append([]jira.Project(nil), projects...)
	c.defaultProjectKey = projectKey
	c.selectedProject = selected
	c.subtaskMode = true
	c.parentKey = parent.Key
	c.issueTypes = nil
	c.typeInput.Reset()
	c.typeCursor = 0
	c.typesLoading = true
	c.typesErr = nil
	c.projectInput.Blur()
	c.form = Form{}
	c.formLoading = false
	c.formErr = nil
	c.formReady = false
	c.submitting = false
	c.submitErr = ""
	cmd := tea.Batch(
		c.typeInput.Focus(),
		func() tea.Msg { return CreateProjectChosenMsg{ProjectKey: projectKey} },
	)
	return c, cmd
}

// projectKeyFromIssue extracts "PROJ" from "PROJ-123". Returns "" when
// the input has no '-'.
func projectKeyFromIssue(key string) string {
	if i := strings.IndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return ""
}

// Hide returns a copy of c with the overlay closed and per-open state
// cleared.
func (c Create) Hide() Create {
	c.mode = createHidden
	c.projects = nil
	c.defaultProjectKey = ""
	c.projectInput.Reset()
	c.projectInput.Blur()
	c.projectCursor = 0
	c.selectedProject = jira.Project{}
	c.selectedType = jira.IssueType{}
	c.issueTypes = nil
	c.typeInput.Reset()
	c.typeInput.Blur()
	c.typeCursor = 0
	c.typesLoading = false
	c.typesErr = nil
	c.form = Form{}
	c.formLoading = false
	c.formErr = nil
	c.formReady = false
	c.submitting = false
	c.submitErr = ""
	c.parentKey = ""
	c.subtaskMode = false
	return c
}

// Update consumes events while the overlay is visible. Messages are routed
// to whichever step is active; CreateIssueTypesMsg is processed in any step
// because the network reply may arrive after the user has stepped back.
func (c Create) Update(msg tea.Msg) (Create, tea.Cmd) {
	if c.mode == createHidden {
		return c, nil
	}
	if m, ok := msg.(CreateIssueTypesMsg); ok {
		return c.handleIssueTypesMsg(m), nil
	}
	if m, ok := msg.(CreateMetaLoadedMsg); ok {
		return c.handleMetaLoadedMsg(m), nil
	}
	if m, ok := msg.(CreateSubmitDoneMsg); ok {
		return c.handleSubmitDoneMsg(m), nil
	}
	if m, ok := msg.(UserSearchRequestMsg); ok {
		// Re-emit so the root model can dispatch Client.SearchUsers and route
		// the result back as a UserSearchResultsMsg.
		return c, func() tea.Msg { return m }
	}
	if m, ok := msg.(UserSearchResultsMsg); ok {
		for i := range c.form.Fields {
			if c.form.Fields[i].Meta.ID == m.FieldID {
				c.form.Fields[i].handleSearchResults(m)
				break
			}
		}
		return c, nil
	}
	switch c.mode {
	case createStepProject:
		return c.updateProjectStep(msg)
	case createStepType:
		return c.updateTypeStep(msg)
	case createStepFields:
		return c.updateFieldsStep(msg)
	}
	return c, nil
}

// updateProjectStep handles input while Step 1 is active.
func (c Create) updateProjectStep(msg tea.Msg) (Create, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		c.projectInput, cmd = c.projectInput.Update(msg)
		return c, cmd
	}
	if key.Matches(k, c.closeBinding) {
		hidden := c.Hide()
		return hidden, func() tea.Msg { return CreateCancelledMsg{} }
	}
	switch k.String() {
	case "up", "ctrl+p":
		if c.projectCursor > 0 {
			c.projectCursor--
		}
		c.clampProjectScroll()
		return c, nil
	case "down", "ctrl+n":
		if c.projectCursor < len(c.FilteredProjects())-1 {
			c.projectCursor++
		}
		c.clampProjectScroll()
		return c, nil
	case "/":
		c.projectInput.SetValue("")
		c.projectCursor = 0
		c.projectScroll = 0
		return c, nil
	case "enter":
		filtered := c.FilteredProjects()
		if len(filtered) == 0 {
			return c, nil
		}
		if c.projectCursor >= len(filtered) {
			c.projectCursor = len(filtered) - 1
		}
		c.selectedProject = filtered[c.projectCursor]
		c.mode = createStepType
		c.issueTypes = nil
		c.typeInput.Reset()
		c.typeCursor = 0
		c.typesLoading = true
		c.typesErr = nil
		c.projectInput.Blur()
		focus := c.typeInput.Focus()
		key := c.selectedProject.Key
		emit := func() tea.Msg { return CreateProjectChosenMsg{ProjectKey: key} }
		if focus == nil {
			return c, emit
		}
		return c, tea.Batch(focus, emit)
	}
	old := c.projectInput.Value()
	var cmd tea.Cmd
	c.projectInput, cmd = c.projectInput.Update(msg)
	if c.projectInput.Value() != old {
		c.projectCursor = 0
	}
	c.clampProjectScroll()
	return c, cmd
}

// updateTypeStep handles input while Step 2 is active.
func (c Create) updateTypeStep(msg tea.Msg) (Create, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		c.typeInput, cmd = c.typeInput.Update(msg)
		return c, cmd
	}
	if key.Matches(k, c.closeBinding) {
		c.mode = createStepProject
		c.issueTypes = nil
		c.typeInput.Reset()
		c.typeInput.Blur()
		c.typeCursor = 0
		c.typesLoading = false
		c.typesErr = nil
		c.selectedProject = jira.Project{}
		return c, c.projectInput.Focus()
	}
	switch k.String() {
	case "up", "ctrl+p":
		if c.typeCursor > 0 {
			c.typeCursor--
		}
		return c, nil
	case "down", "ctrl+n":
		if c.typeCursor < len(c.FilteredIssueTypes())-1 {
			c.typeCursor++
		}
		return c, nil
	case "enter":
		filtered := c.FilteredIssueTypes()
		if len(filtered) == 0 {
			return c, nil
		}
		if c.typeCursor >= len(filtered) {
			c.typeCursor = len(filtered) - 1
		}
		sel := filtered[c.typeCursor]
		projectKey := c.selectedProject.Key
		c.selectedType = sel
		c.mode = createStepFields
		c.typeInput.Blur()
		if meta, hit := c.metaCache[metaCacheKey(projectKey, sel.ID)]; hit {
			c.form = BuildForm(meta, FormDefaults{CurrentUserAccountID: c.currentUserAccountID})
			c.formLoading = false
			c.formErr = nil
			c.formReady = true
			return c, nil
		}
		c.form = Form{}
		c.formLoading = true
		c.formErr = nil
		c.formReady = false
		return c, func() tea.Msg {
			return CreateTypeChosenMsg{ProjectKey: projectKey, IssueType: sel}
		}
	}
	old := c.typeInput.Value()
	var cmd tea.Cmd
	c.typeInput, cmd = c.typeInput.Update(msg)
	if c.typeInput.Value() != old {
		c.typeCursor = 0
	}
	return c, cmd
}

// formSummaryValue returns the current value of the wizard's summary field,
// or "" if no such field exists yet. The external editor seeds its H1 with
// this value so users edit a single coherent buffer.
func (c Create) formSummaryValue() string {
	for _, f := range c.form.Fields {
		if f.Meta.ID == "summary" {
			return f.Value()
		}
	}
	return ""
}

// updateFieldsStep handles input while Step 3 is active. Esc steps back to
// the type picker (preserving the previously chosen project); Ctrl+S submits
// the form (after validation); other keys are forwarded to the form so its
// Tab/Shift+Tab navigation works.
func (c Create) updateFieldsStep(msg tea.Msg) (Create, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if ok && key.Matches(k, c.closeBinding) {
		if c.submitting {
			return c, nil
		}
		c.mode = createStepType
		c.form = Form{}
		c.formLoading = false
		c.formErr = nil
		c.formReady = false
		c.submitting = false
		c.submitErr = ""
		c.selectedType = jira.IssueType{}
		return c, c.typeInput.Focus()
	}
	if ok && k.String() == "ctrl+s" {
		return c.trySubmit()
	}
	if !c.formReady || c.submitting {
		return c, nil
	}
	// For user fields, route key events through UpdateUser so the dropdown
	// navigation and debounce search work correctly. Tab/Shift+Tab are still
	// handled by Form.Update (focus cycling), so only intercept other keys.
	if k, ok := msg.(tea.KeyMsg); ok && k.Type != tea.KeyTab && k.Type != tea.KeyShiftTab {
		if fi := c.form.Focus(); fi >= 0 && fi < len(c.form.Fields) {
			focused := &c.form.Fields[fi]
			if focused.Kind == FieldKindUser {
				var cmd tea.Cmd
				*focused, cmd = focused.UpdateUser(k)
				return c, cmd
			}
		}
	}
	if req, ok := msg.(openExternalEditorRequestMsg); ok {
		c.pendingEditorToken++
		title := "New issue"
		if c.selectedType.Name != "" {
			title = "New " + c.selectedType.Name
		}
		summary := c.formSummaryValue()
		body := req.body
		token := c.pendingEditorToken
		return c, func() tea.Msg {
			return CreateOpenEditorMsg{
				Summary: summary,
				Body:    body,
				Title:   title,
				Token:   token,
			}
		}
	}
	updated, cmd := c.form.Update(msg)
	c.form = updated
	return c, cmd
}

// trySubmit validates the current form. If validation fails, errors are
// bound to fields and no command is dispatched. On success the overlay
// flips into the submitting state and emits a CreateSubmitRequestedMsg
// carrying the assembled payload.
func (c Create) trySubmit() (Create, tea.Cmd) {
	if !c.formReady || c.submitting {
		return c, nil
	}
	form, errs := c.form.Validate()
	c.form = form
	if len(errs) > 0 {
		c.submitErr = ""
		return c, nil
	}
	c.submitErr = ""
	c.submitting = true
	projectKey := c.selectedProject.Key
	issueTypeID := c.selectedType.ID
	payload := c.form.BuildPayload(projectKey, issueTypeID)
	payload.ParentKey = c.parentKey
	return c, func() tea.Msg {
		return CreateSubmitRequestedMsg{
			ProjectKey:  projectKey,
			IssueTypeID: issueTypeID,
			Payload:     payload,
		}
	}
}

// handleSubmitDoneMsg consumes a CreateSubmitDoneMsg. Stale messages whose
// project+type do not match the current selection are dropped. On success
// the overlay closes (the root model handles list-prepend and toast). On
// error the overlay stays visible and either binds per-field messages from
// a 400 errors map or surfaces a generic submit error string.
func (c Create) handleSubmitDoneMsg(m CreateSubmitDoneMsg) Create {
	if m.ProjectKey != c.selectedProject.Key || m.IssueTypeID != c.selectedType.ID {
		return c
	}
	if !c.submitting {
		return c
	}
	c.submitting = false
	if m.Err == nil {
		return c.Hide()
	}
	fieldErrs, generic := parseSubmitError(m.Err)
	if len(fieldErrs) > 0 {
		for i, fld := range c.form.Fields {
			if msg, ok := fieldErrs[fld.Meta.ID]; ok {
				c.form.Fields[i] = fld.SetError(msg)
			}
		}
	}
	if generic == "" && len(fieldErrs) == 0 {
		generic = m.Err.Error()
	}
	c.submitErr = generic
	return c
}

// parseSubmitError teases a CreateIssue error apart into per-field
// messages and a generic top-level message. Jira validation errors come
// back as `{"errorMessages":[…],"errors":{"summary":"…"}}`; other errors
// fall through to a generic string.
func parseSubmitError(err error) (map[string]string, string) {
	fields, generic, ok := jira.FieldErrors(err)
	if !ok {
		return nil, ""
	}
	return fields, generic
}

// Submitting reports whether the overlay is currently waiting for a
// CreateSubmitDoneMsg. The Step 3 view shows a "Submitting…" hint while
// true and ignores most keystrokes.
func (c Create) Submitting() bool { return c.submitting }

// SubmitError returns the most recent generic submit error (top-level
// errorMessages or a non-400 error string), or "" when no error is set.
func (c Create) SubmitError() string { return c.submitErr }

// handleMetaLoadedMsg processes a CreateMetaLoadedMsg. Stale messages (the
// user backed out before the fetch returned) are dropped from the live form
// but their successful payload is still cached so a re-open of the same
// project+type will hit the cache.
func (c Create) handleMetaLoadedMsg(m CreateMetaLoadedMsg) Create {
	if m.Err == nil {
		if c.metaCache == nil {
			c.metaCache = make(map[string]jira.CreateMeta)
		}
		c.metaCache[metaCacheKey(m.ProjectKey, m.IssueTypeID)] = m.Meta
	}
	if m.ProjectKey != c.selectedProject.Key || m.IssueTypeID != c.selectedType.ID {
		return c
	}
	c.formLoading = false
	if m.Err != nil {
		c.formErr = m.Err
		c.form = Form{}
		c.formReady = false
		return c
	}
	c.formErr = nil
	c.form = BuildForm(m.Meta, FormDefaults{CurrentUserAccountID: c.currentUserAccountID})
	c.formReady = true
	return c
}

// HandleEditorClosed consumes the external-editor result. Stale tokens are
// dropped silently. On cancel/error, the wizard does not touch the form.
// On success, the form's summary field is overwritten when the parser
// yielded a non-empty H1; the description field's body is always set.
//
// The app-level dispatcher imports the editor package; this method takes
// summary/body/cancelled/err/token by value to avoid an import cycle.
func (c Create) HandleEditorClosed(token int, cancelled bool, summary, body string, err error) (Create, error) {
	if token != c.pendingEditorToken {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if cancelled {
		return c, nil
	}
	for i := range c.form.Fields {
		switch c.form.Fields[i].Meta.ID {
		case "summary":
			if summary != "" {
				c.form.Fields[i] = c.form.Fields[i].SetValue(summary)
			}
		case "description":
			c.form.Fields[i] = c.form.Fields[i].SetExternalBody(body)
		}
	}
	return c, nil
}

// Form returns the dynamic-fields form built from createmeta. It is the
// zero value until CreateMetaLoadedMsg arrives for the current selection.
func (c Create) Form() Form { return c.form }

// FormReady reports whether the fields step has received its createmeta
// reply (and is therefore showing live form widgets rather than a loading
// placeholder).
func (c Create) FormReady() bool { return c.formReady }

// FormLoading reports whether the overlay is waiting for createmeta.
func (c Create) FormLoading() bool { return c.formLoading }

// FormErr returns the most recent createmeta fetch error, if any.
func (c Create) FormErr() error { return c.formErr }

// FormFields returns the current form's fields. Exposed for tests that
// assert on the rendered form state.
func (c Create) FormFields() []Field { return c.form.Fields }

// SelectedIssueType returns the issue type chosen on Step 2.
func (c Create) SelectedIssueType() jira.IssueType { return c.selectedType }

// handleIssueTypesMsg processes a CreateIssueTypesMsg. Replies for a project
// other than the one currently selected (the user backed out and picked a
// different project before the fetch returned) are dropped.
func (c Create) handleIssueTypesMsg(m CreateIssueTypesMsg) Create {
	if m.ProjectKey != c.selectedProject.Key {
		return c
	}
	c.typesLoading = false
	if m.Err != nil {
		c.typesErr = m.Err
		c.issueTypes = nil
		c.typeCursor = 0
		return c
	}
	c.typesErr = nil
	c.issueTypes = append([]jira.IssueType(nil), m.IssueTypes...)
	c.typeCursor = indexOfIssueTypeName(c.issueTypes, defaultIssueTypeName)
	return c
}

// View renders the overlay. Returns "" when hidden so the caller can skip
// layout work.
func (c Create) View(s styles.Styles) string {
	if c.mode == createHidden {
		return ""
	}
	switch c.mode {
	case createStepProject:
		return c.viewProjectStep(s)
	case createStepType:
		return c.viewTypeStep(s)
	case createStepFields:
		return c.viewFieldsStep(s)
	}
	return ""
}

func (c Create) viewFieldsStep(s styles.Styles) string {
	header := "Create issue — Step 3/3: fields"
	if c.selectedProject.Key != "" {
		typeName := c.selectedType.Name
		if typeName == "" {
			typeName = c.selectedType.ID
		}
		header = "Create " + c.selectedProject.Key + " (" + typeName + ") — Step 3/3: fields"
	}
	title := s.OverlayTitle.Render(header)
	var body string
	switch {
	case c.formErr != nil:
		body = s.Error.Render("Error: " + c.formErr.Error())
	case c.formLoading || !c.formReady:
		body = s.Muted.Render("Loading fields…")
	default:
		body = c.form.View(s)
	}
	hintText := "ctrl+s submit    tab next field    shift+tab prev    " +
		c.closeBinding.Help().Key + " back"
	if c.submitting {
		hintText = "Submitting…"
	}
	hint := s.Muted.Render(hintText)
	parts := []string{title, "", body}
	if c.submitErr != "" {
		parts = append(parts, "", s.Error.Render(c.submitErr))
	}
	parts = append(parts, "", hint)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}

func (c Create) viewProjectStep(s styles.Styles) string {
	title := s.OverlayTitle.Render("Create issue — Step 1/2: project")
	rows := c.renderProjectRows(s)
	hint := s.Muted.Render(
		"type to filter    /  reset    enter next    " + c.closeBinding.Help().Key + " " + c.closeBinding.Help().Desc,
	)
	parts := []string{title, "", c.projectInput.View(), "", rows, "", hint}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}

func (c Create) viewTypeStep(s styles.Styles) string {
	titleText := "Create issue — Step 2/2: type"
	if c.selectedProject.Key != "" {
		titleText = "Create " + c.selectedProject.Key + " — Step 2/2: type"
	}
	title := s.OverlayTitle.Render(titleText)
	var body string
	switch {
	case c.typesErr != nil:
		body = s.Error.Render("Error: " + c.typesErr.Error())
	case c.typesLoading && len(c.issueTypes) == 0:
		body = s.Muted.Render("Loading issue types…")
	default:
		body = c.renderTypeRows(s)
	}
	hint := s.Muted.Render(
		"enter next    " + c.closeBinding.Help().Key + " back",
	)
	parts := []string{title, "", c.typeInput.View(), "", body, "", hint}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}

func (c Create) renderProjectRows(s styles.Styles) string {
	filtered := c.FilteredProjects()
	if len(filtered) == 0 {
		return s.Muted.Render("(no projects match)")
	}
	start := c.projectScroll
	end := start + projectVisibleRows
	if end > len(filtered) {
		end = len(filtered)
	}
	rows := make([]string, 0, end-start+2)
	if start > 0 {
		rows = append(rows, s.Muted.Render(fmt.Sprintf("↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		p := filtered[i]
		label := p.Key
		if p.Name != "" {
			label += "  " + p.Name
		}
		if i == c.projectCursor {
			label = s.ListItemSelected.Render(label)
		} else {
			label = s.ListItem.Render(label)
		}
		rows = append(rows, label)
	}
	if end < len(filtered) {
		rows = append(rows, s.Muted.Render(fmt.Sprintf("↓ %d more", len(filtered)-end)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (c Create) renderTypeRows(s styles.Styles) string {
	filtered := c.FilteredIssueTypes()
	if len(filtered) == 0 {
		return s.Muted.Render("(no issue types match)")
	}
	rows := make([]string, 0, len(filtered))
	for i, it := range filtered {
		label := it.Name
		if label == "" {
			label = it.ID
		}
		if i == c.typeCursor {
			label = s.ListItemSelected.Render(label)
		} else {
			label = s.ListItem.Render(label)
		}
		rows = append(rows, label)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func filterProjects(projects []jira.Project, q string) []jira.Project {
	q = strings.TrimSpace(q)
	if q == "" {
		return append([]jira.Project(nil), projects...)
	}
	needle := strings.ToLower(q)
	out := make([]jira.Project, 0, len(projects))
	for _, p := range projects {
		if strings.Contains(strings.ToLower(p.Key), needle) ||
			strings.Contains(strings.ToLower(p.Name), needle) {
			out = append(out, p)
		}
	}
	return out
}

func filterIssueTypes(types []jira.IssueType, q string) []jira.IssueType {
	q = strings.TrimSpace(q)
	if q == "" {
		return append([]jira.IssueType(nil), types...)
	}
	needle := strings.ToLower(q)
	out := make([]jira.IssueType, 0, len(types))
	for _, t := range types {
		if strings.Contains(strings.ToLower(t.Name), needle) {
			out = append(out, t)
		}
	}
	return out
}

func indexOfProjectKey(projects []jira.Project, key string) int {
	if key == "" {
		return 0
	}
	for i, p := range projects {
		if p.Key == key {
			return i
		}
	}
	return 0
}

func indexOfIssueTypeName(types []jira.IssueType, name string) int {
	for i, t := range types {
		if strings.EqualFold(t.Name, name) {
			return i
		}
	}
	return 0
}

// clampProjectScroll keeps c.projectScroll so c.projectCursor is visible
// in a window of projectVisibleRows rows over the filtered list.
func (c *Create) clampProjectScroll() {
	n := len(c.FilteredProjects())
	if n == 0 {
		c.projectScroll = 0
		return
	}
	if c.projectScroll < 0 {
		c.projectScroll = 0
	}
	maxScroll := n - projectVisibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if c.projectScroll > maxScroll {
		c.projectScroll = maxScroll
	}
	if c.projectCursor < c.projectScroll {
		c.projectScroll = c.projectCursor
	}
	if c.projectCursor >= c.projectScroll+projectVisibleRows {
		c.projectScroll = c.projectCursor - projectVisibleRows + 1
	}
}
