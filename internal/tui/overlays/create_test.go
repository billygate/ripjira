package overlays

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

func sampleProjects() []jira.Project {
	return []jira.Project{
		{ID: "1", Key: "ALPHA", Name: "Alpha"},
		{ID: "2", Key: "BETA", Name: "Beta Project"},
		{ID: "3", Key: "GAMMA", Name: "Gamma Things"},
	}
}

func sampleIssueTypes() []jira.IssueType {
	return []jira.IssueType{
		{ID: "10001", Name: "Bug"},
		{ID: "10002", Name: "Task"},
		{ID: "10003", Name: "Story"},
	}
}

func TestCreate_HiddenByDefault(t *testing.T) {
	c := NewCreate(closeBinding(), "")
	if c.Visible() {
		t.Error("Create should start hidden")
	}
	if c.Step() != 0 {
		t.Errorf("hidden Step = %d, want 0", c.Step())
	}
	if got := c.View(newStyles(t)); got != "" {
		t.Errorf("hidden View should be empty, got %q", got)
	}
}

func TestCreate_ShowDefaultProjectPreselected(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	if !c.Visible() {
		t.Fatal("Show did not flip visible")
	}
	if c.Step() != 1 {
		t.Errorf("Step = %d, want 1", c.Step())
	}
	if c.ProjectCursor() != 1 {
		t.Errorf("default project cursor = %d, want 1 (BETA)", c.ProjectCursor())
	}
	view := stripANSI(c.View(newStyles(t)))
	for _, want := range []string{"Step 1/2", "ALPHA", "BETA", "GAMMA"} {
		if !strings.Contains(view, want) {
			t.Errorf("Step 1 view missing %q\n%s", want, view)
		}
	}
}

func TestCreate_ShowUnknownDefaultFallsBackToFirst(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "ZULU")
	if c.ProjectCursor() != 0 {
		t.Errorf("unknown default → cursor = %d, want 0", c.ProjectCursor())
	}
}

func TestCreate_FilterNarrowsProjectsAndResetsCursor(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	if c.ProjectCursor() != 1 {
		t.Fatalf("setup: cursor = %d, want 1", c.ProjectCursor())
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	filtered := c.FilteredProjects()
	if len(filtered) != 1 || filtered[0].Key != "GAMMA" {
		t.Fatalf("after typing 'g', filtered = %+v", filtered)
	}
	if c.ProjectCursor() != 0 {
		t.Errorf("cursor after filter = %d, want 0", c.ProjectCursor())
	}
}

func TestCreate_NavigationOnStep1(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "ALPHA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.ProjectCursor() != 1 {
		t.Errorf("after down cursor = %d, want 1", c.ProjectCursor())
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.ProjectCursor() != 2 {
		t.Errorf("clamped down cursor = %d, want 2", c.ProjectCursor())
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.ProjectCursor() != 1 {
		t.Errorf("after up cursor = %d, want 1", c.ProjectCursor())
	}
}

func TestCreate_EnterAdvancesToStep2AndEmits(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if c.Step() != 2 {
		t.Errorf("after enter Step = %d, want 2", c.Step())
	}
	if c.SelectedProjectKey() != "BETA" {
		t.Errorf("selected project = %q, want BETA", c.SelectedProjectKey())
	}
	if !c.TypesLoading() {
		t.Error("Step 2 should start in loading state")
	}
	chosen, ok := findProjectChosenMsg(execCmd(cmd))
	if !ok {
		t.Fatalf("enter on Step 1 did not emit CreateProjectChosenMsg")
	}
	if chosen.ProjectKey != "BETA" {
		t.Errorf("chosen.ProjectKey = %q, want BETA", chosen.ProjectKey)
	}
}

func TestCreate_EnterEmptyFilterIsNoop(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "ALPHA")
	for _, r := range "zzz" {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(c.FilteredProjects()) != 0 {
		t.Fatalf("setup: filtered = %+v", c.FilteredProjects())
	}
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c.Step() != 1 {
		t.Errorf("enter with no matches advanced to Step %d", c.Step())
	}
	if cmd != nil {
		t.Errorf("enter with no matches returned cmd: %v", cmd)
	}
}

func TestCreate_IssueTypesMsgPopulatesStep2WithTaskDefault(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	if c.TypesLoading() {
		t.Error("loading should clear after types arrive")
	}
	if got := len(c.IssueTypes()); got != 3 {
		t.Errorf("issue types len = %d, want 3", got)
	}
	if c.TypeCursor() != 1 {
		t.Errorf("default type cursor = %d, want 1 (Task)", c.TypeCursor())
	}
	view := stripANSI(c.View(newStyles(t)))
	for _, want := range []string{"BETA", "Step 2/2", "Bug", "Task", "Story"} {
		if !strings.Contains(view, want) {
			t.Errorf("Step 2 view missing %q\n%s", want, view)
		}
	}
}

func TestCreate_IssueTypesMsgWithoutTaskFallsBackToFirst(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: []jira.IssueType{{ID: "1", Name: "Bug"}, {ID: "2", Name: "Story"}},
	})
	if c.TypeCursor() != 0 {
		t.Errorf("no-Task fallback cursor = %d, want 0", c.TypeCursor())
	}
}

func TestCreate_IssueTypesMsgErrorSurfaces(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	netErr := errors.New("create-meta boom")
	c, _ = c.Update(CreateIssueTypesMsg{ProjectKey: "BETA", Err: netErr})
	if c.TypesErr() == nil || !strings.Contains(c.TypesErr().Error(), "boom") {
		t.Errorf("err = %v, want to contain 'boom'", c.TypesErr())
	}
	view := stripANSI(c.View(newStyles(t)))
	if !strings.Contains(view, "boom") {
		t.Errorf("Step 2 view missing error\n%s", view)
	}
}

func TestCreate_IssueTypesMsgIgnoredForOtherProject(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "ALPHA",
		IssueTypes: sampleIssueTypes(),
	})
	if got := len(c.IssueTypes()); got != 0 {
		t.Errorf("stale msg applied: %+v", c.IssueTypes())
	}
	if !c.TypesLoading() {
		t.Error("loading should remain true after stale msg")
	}
}

func TestCreate_FilterTypesAndEnterPublishesType(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S', 't', 'o'}})
	filtered := c.FilteredIssueTypes()
	if len(filtered) != 1 || filtered[0].Name != "Story" {
		t.Fatalf("filtered types = %+v", filtered)
	}
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c.Visible() != true {
		t.Error("enter on Step 2 should NOT hide the overlay shell — fields step is next")
	}
	chosen, ok := cmd().(CreateTypeChosenMsg)
	if !ok {
		t.Fatalf("enter on Step 2 did not emit CreateTypeChosenMsg")
	}
	if chosen.ProjectKey != "BETA" {
		t.Errorf("chosen.ProjectKey = %q, want BETA", chosen.ProjectKey)
	}
	if chosen.IssueType.Name != "Story" {
		t.Errorf("chosen.IssueType.Name = %q, want Story", chosen.IssueType.Name)
	}
}

func TestCreate_EnterEmptyTypeFilterIsNoop(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	for _, r := range "zzz" {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(c.FilteredIssueTypes()) != 0 {
		t.Fatalf("setup: filtered = %+v", c.FilteredIssueTypes())
	}
	_, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("enter with no matches returned cmd: %v", cmd)
	}
}

func TestCreate_EscOnStep2GoesBackToStep1(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	if c.Step() != 2 {
		t.Fatalf("setup: Step = %d", c.Step())
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c.Step() != 1 {
		t.Errorf("esc on Step 2 → Step %d, want 1", c.Step())
	}
	if !c.Visible() {
		t.Error("esc on Step 2 should NOT close the overlay")
	}
	if c.SelectedProjectKey() != "" {
		t.Errorf("after going back, selected = %q, want empty", c.SelectedProjectKey())
	}
	if c.TypesLoading() || c.TypesErr() != nil {
		t.Error("Step 2 state should be cleared after going back")
	}
}

func TestCreate_EscOnStep1ClosesAndEmitsCancel(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("esc on Step 1 should close the overlay")
	}
	if _, ok := cmd().(CreateCancelledMsg); !ok {
		t.Errorf("esc on Step 1 did not emit CreateCancelledMsg, got %T", cmd())
	}
}

func TestCreate_HideClearsState(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c = c.Hide()
	if c.Visible() {
		t.Error("Hide should make overlay invisible")
	}
	if c.SelectedProjectKey() != "" {
		t.Errorf("Hide should clear selected project, got %q", c.SelectedProjectKey())
	}
	if len(c.Projects()) != 0 {
		t.Errorf("Hide should clear projects, got %+v", c.Projects())
	}
}

func TestCreate_HiddenUpdateNoop(t *testing.T) {
	c := NewCreate(closeBinding(), "")
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("hidden overlay should stay hidden")
	}
	if cmd != nil {
		t.Errorf("hidden update returned cmd: %v", cmd)
	}
}

func TestCreate_TypeStepLoadingView(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := stripANSI(c.View(newStyles(t)))
	if !strings.Contains(view, "Loading issue types") {
		t.Errorf("Step 2 loading view missing placeholder\n%s", view)
	}
}

func TestShowAsSubtask_OpensAtTypeStep(t *testing.T) {
	parent := jira.Issue{Key: "PROJ-100"}
	projects := []jira.Project{{Key: "PROJ"}, {Key: "OTHER"}}
	c, _ := NewCreate(closeBinding(), "").ShowAsSubtask(parent, projects)
	if c.Step() != 2 {
		t.Errorf("step = %d, want 2", c.Step())
	}
	if c.SelectedProjectKey() != "PROJ" {
		t.Errorf("project = %q, want PROJ", c.SelectedProjectKey())
	}
	if !c.IsSubtaskMode() {
		t.Errorf("expected subtask mode")
	}
	if c.ParentKey() != "PROJ-100" {
		t.Errorf("parent = %q, want PROJ-100", c.ParentKey())
	}
}

func TestShowAsSubtask_FiltersToSubtaskTypes(t *testing.T) {
	parent := jira.Issue{Key: "PROJ-100"}
	c, _ := NewCreate(closeBinding(), "").ShowAsSubtask(parent, []jira.Project{{Key: "PROJ"}})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "PROJ",
		IssueTypes: []jira.IssueType{
			{ID: "10100", Name: "Task", Subtask: false},
			{ID: "10103", Name: "Sub-task", Subtask: true},
			{ID: "10104", Name: "Sub-bug", Subtask: true},
		},
	})
	got := c.FilteredIssueTypes()
	if len(got) != 2 {
		t.Fatalf("filtered count = %d, want 2", len(got))
	}
	for _, it := range got {
		if !it.Subtask {
			t.Errorf("non-subtask leaked: %+v", it)
		}
	}
}

func TestShowAsSubtask_PayloadCarriesParentKey(t *testing.T) {
	parent := jira.Issue{Key: "PROJ-100"}
	c, _ := NewCreate(closeBinding(), "").ShowAsSubtask(parent, []jira.Project{{Key: "PROJ"}})
	if got := c.PayloadParentKey(); got != "PROJ-100" {
		t.Errorf("payload parent = %q, want PROJ-100", got)
	}
}

func TestCreate_ProjectPickerScrollsCursorIntoView(t *testing.T) {
	projs := make([]jira.Project, 30)
	for i := range projs {
		projs[i] = jira.Project{Key: fmt.Sprintf("P%02d", i)}
	}
	c, _ := NewCreate(closeBinding(), "").Show(projs, "")
	if got := c.ProjectScroll(); got != 0 {
		t.Fatalf("initial scroll = %d, want 0", got)
	}
	// Press down 12 times — cursor moves to row 12, which is past the
	// 10-row window, so the window should scroll.
	for i := 0; i < 12; i++ {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if c.ProjectCursor() != 12 {
		t.Fatalf("cursor = %d, want 12", c.ProjectCursor())
	}
	if c.ProjectScroll() < 3 || c.ProjectScroll() > 12 {
		t.Errorf("scroll = %d should keep cursor (12) inside window of 10", c.ProjectScroll())
	}
}

func TestCreate_ProjectPickerSlashResetsFilter(t *testing.T) {
	projs := []jira.Project{{Key: "ALPHA"}, {Key: "BETA"}}
	c, _ := NewCreate(closeBinding(), "").Show(projs, "")
	c.SetProjectInputForTest("alp")
	if got := len(c.FilteredProjects()); got != 1 {
		t.Fatalf("filtered = %d, want 1", got)
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if c.ProjectInputValue() != "" {
		t.Errorf("input value = %q, want empty", c.ProjectInputValue())
	}
	if got := len(c.FilteredProjects()); got != 2 {
		t.Errorf("filtered after / = %d, want 2", got)
	}
}

func TestCreate_UserResultsForwardedToField(t *testing.T) {
	// Build a CreateMeta with a single user field, then drive the overlay
	// through the wizard so the form is populated.
	userMeta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "assignee", Name: "Assignee", SchemaType: "user"},
	}}
	c, _ := advanceToFields(t, NewCreate(closeBinding(), ""), "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        userMeta,
	})
	if len(c.form.Fields) == 0 {
		t.Fatal("form has no fields after meta loaded")
	}
	token := c.form.Fields[0].dropdown.token
	msg := UserSearchResultsMsg{
		FieldID: "assignee",
		Token:   token,
		Users:   []jira.User{{AccountID: "a1", DisplayName: "Alice"}},
	}
	c, _ = c.Update(msg)
	if len(c.form.Fields[0].dropdown.results) != 1 {
		t.Fatalf("results not forwarded: %+v", c.form.Fields[0].dropdown.results)
	}
}

func TestCreate_UserSearchRequestReemitted(t *testing.T) {
	// The overlay must be visible for Update to process messages.
	c, _ := advanceToFields(t, NewCreate(closeBinding(), ""), "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	req := UserSearchRequestMsg{FieldID: "assignee", Query: "alice", Token: 1}
	_, cmd := c.Update(req)
	if cmd == nil {
		t.Fatal("expected a command to be returned for UserSearchRequestMsg")
	}
	got := cmd()
	if _, ok := got.(UserSearchRequestMsg); !ok {
		t.Fatalf("expected UserSearchRequestMsg to be re-emitted, got %T", got)
	}
}

// newWizardOnFieldsStepWithDescription returns a Create overlay that is on
// Step 3 (fields) with a form built from a meta containing summary and
// description, with focus placed on the description row.
func newWizardOnFieldsStepWithDescription(t *testing.T) Create {
	t.Helper()
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "summary", Name: "Summary", Required: true, SchemaType: "string"},
		{ID: "description", Name: "Description", SchemaType: "string"},
	}}
	c, _ := advanceToFields(t, NewCreate(closeBinding(), ""), "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        meta,
	})
	if !c.FormReady() {
		t.Fatal("newWizardOnFieldsStepWithDescription: form not ready")
	}
	// Move focus to description (index 1 after reorder: summary=0, description=1).
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab})
	return c
}

func TestCreate_OpenEditorMsgFromDescription(t *testing.T) {
	c := newWizardOnFieldsStepWithDescription(t)
	// Enter on the description row emits openExternalEditorRequestMsg as a cmd.
	// The app calls that cmd and routes the result back into the wizard, which
	// then emits the public CreateOpenEditorMsg.
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from Enter on description row")
	}
	innerMsg := cmd()
	// Feed the inner message back — wizard intercepts and promotes it.
	_, cmd = c.Update(innerMsg)
	if cmd == nil {
		t.Fatal("expected cmd from wizard after openExternalEditorRequestMsg")
	}
	msg := cmd()
	if _, ok := msg.(CreateOpenEditorMsg); !ok {
		t.Fatalf("got %T, want CreateOpenEditorMsg", msg)
	}
}

func TestCreate_HandleEditorClosed_AppliesBoth(t *testing.T) {
	c := newWizardOnFieldsStepWithDescription(t)
	c.pendingEditorToken = 1

	c, err := c.HandleEditorClosed(1, false, "Wizard summary", "Body lines", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, f := range c.form.Fields {
		switch f.Meta.ID {
		case "summary":
			if f.Value() != "Wizard summary" {
				t.Errorf("summary: %q", f.Value())
			}
		case "description":
			if f.ExternalBody() != "Body lines" {
				t.Errorf("body: %q", f.ExternalBody())
			}
		}
	}
}

func TestCreate_HandleEditorClosed_StaleTokenIgnored(t *testing.T) {
	c := newWizardOnFieldsStepWithDescription(t)
	c.pendingEditorToken = 5
	c, err := c.HandleEditorClosed(4, false, "Stale", "Stale body", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, f := range c.form.Fields {
		if f.Meta.ID == "description" && f.ExternalBody() != "" {
			t.Errorf("description should be untouched, got %q", f.ExternalBody())
		}
	}
}

// findProjectChosenMsg unwraps a (possibly batched) tea.Msg looking for a
// CreateProjectChosenMsg leaf. Mirrors findSearchRequestMsg.
func findProjectChosenMsg(msg tea.Msg) (CreateProjectChosenMsg, bool) {
	switch v := msg.(type) {
	case CreateProjectChosenMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if r, ok := findProjectChosenMsg(c()); ok {
				return r, true
			}
		}
	}
	return CreateProjectChosenMsg{}, false
}
