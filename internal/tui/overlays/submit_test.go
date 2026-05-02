package overlays

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

// readyForSubmit drives the create overlay through Step 1 → 3 with sampleMeta
// loaded and returns it parked on Step 3. Required fields are filled with
// throw-away values so default Validate() passes; tests that care about
// validation override field 0 (summary) before calling trySubmit.
func readyForSubmit(t *testing.T, fillRequired bool) Create {
	t.Helper()
	c := NewCreate(closeBinding(), "")
	c, _ = c.Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	if !c.FormReady() {
		t.Fatalf("readyForSubmit: form not ready")
	}
	if fillRequired {
		c.form.Fields[0] = c.form.Fields[0].SetValue("New issue summary")
	}
	return c
}

func TestCreate_Submit_RequiredEmptyBlocksAndBindsError(t *testing.T) {
	c := readyForSubmit(t, false)
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		t.Errorf("validation failure should not dispatch cmd, got %T", cmd())
	}
	if updated.Submitting() {
		t.Error("validation failure should not flip submitting=true")
	}
	if !updated.Visible() || updated.Step() != 3 {
		t.Errorf("overlay should stay on Step 3, got Step=%d Visible=%v",
			updated.Step(), updated.Visible())
	}
	if got := updated.Form().Fields[0].Error(); got == "" {
		t.Errorf("summary field should have a bound error after blocked submit, got %q", got)
	}
	view := stripANSI(updated.View(newStyles(t)))
	if !strings.Contains(view, "required") {
		t.Errorf("Step 3 view missing required-error marker after blocked submit\n%s", view)
	}
}

func TestCreate_Submit_ValidFormEmitsRequest(t *testing.T) {
	c := readyForSubmit(t, true)
	c.form.Fields[1] = c.form.Fields[1].SetValue("body line one")

	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("valid form should dispatch a CreateSubmitRequestedMsg cmd")
	}
	if !updated.Submitting() {
		t.Error("valid submit should flip submitting=true")
	}

	req, ok := cmd().(CreateSubmitRequestedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want CreateSubmitRequestedMsg", cmd())
	}
	if req.ProjectKey != "BETA" {
		t.Errorf("ProjectKey = %q, want BETA", req.ProjectKey)
	}
	if req.IssueTypeID == "" {
		t.Errorf("IssueTypeID should be populated, got %q", req.IssueTypeID)
	}
	if req.Payload.Summary != "New issue summary" {
		t.Errorf("Payload.Summary = %q, want %q", req.Payload.Summary, "New issue summary")
	}
	if req.Payload.Description != "body line one" {
		t.Errorf("Payload.Description = %q, want %q", req.Payload.Description, "body line one")
	}
	if req.Payload.ProjectKey != "BETA" || req.Payload.IssueTypeID == "" {
		t.Errorf("Payload project/type missing: %+v", req.Payload)
	}
}

func TestCreate_Submit_DoubleSubmitNoop(t *testing.T) {
	c := readyForSubmit(t, true)
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("setup: first submit should dispatch")
	}
	if !c.Submitting() {
		t.Fatal("setup: first submit should flip submitting=true")
	}
	updated, cmd2 := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd2 != nil {
		t.Errorf("second submit while already submitting should not dispatch, got %T", cmd2())
	}
	if !updated.Submitting() {
		t.Error("second submit must keep submitting=true")
	}
}

func TestCreate_SubmitDone_SuccessHidesOverlay(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !c.Submitting() {
		t.Fatal("setup: submit should flip submitting=true")
	}
	projectKey := c.SelectedProjectKey()
	typeID := c.SelectedIssueType().ID

	created := jira.Issue{Key: "BETA-203", Summary: "New issue summary"}
	updated, _ := c.Update(CreateSubmitDoneMsg{
		ProjectKey:  projectKey,
		IssueTypeID: typeID,
		Issue:       created,
	})
	if updated.Visible() {
		t.Error("success done msg should hide the overlay")
	}
	if updated.Submitting() {
		t.Error("success done msg should clear submitting state")
	}
}

func TestCreate_SubmitDone_FieldErrorBindsToField(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	projectKey := c.SelectedProjectKey()
	typeID := c.SelectedIssueType().ID

	httpErr := &jira.HTTPError{
		StatusCode: 400,
		Status:     "400 Bad Request",
		Body: []byte(`{"errorMessages":[],"errors":` +
			`{"summary":"summary cannot be empty",` +
			`"duedate":"date is in the past"}}`),
		Method: "POST",
		URL:    "https://example.atlassian.net/rest/api/3/issue",
	}
	updated, _ := c.Update(CreateSubmitDoneMsg{
		ProjectKey:  projectKey,
		IssueTypeID: typeID,
		Err:         httpErr,
	})
	if !updated.Visible() {
		t.Fatal("400 should keep overlay visible so the user can fix")
	}
	if updated.Submitting() {
		t.Error("400 should clear submitting state")
	}
	if got := updated.Form().Fields[0].Error(); !strings.Contains(got, "summary cannot be empty") {
		t.Errorf("summary field error = %q, want server message", got)
	}
	// duedate is the last form field (index 6 in sampleMeta after BuildForm).
	dueIdx := -1
	for i, f := range updated.Form().Fields {
		if f.Meta.ID == "duedate" {
			dueIdx = i
			break
		}
	}
	if dueIdx == -1 {
		t.Fatal("duedate field not found in form")
	}
	if got := updated.Form().Fields[dueIdx].Error(); !strings.Contains(got, "date is in the past") {
		t.Errorf("duedate field error = %q, want server message", got)
	}
	view := stripANSI(updated.View(newStyles(t)))
	if !strings.Contains(view, "summary cannot be empty") {
		t.Errorf("Step 3 view missing per-field server error\n%s", view)
	}
}

func TestCreate_SubmitDone_TopLevelErrorMessagesShownGenerically(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	httpErr := &jira.HTTPError{
		StatusCode: 400,
		Status:     "400 Bad Request",
		Body:       []byte(`{"errorMessages":["something blew up"],"errors":{}}`),
	}
	updated, _ := c.Update(CreateSubmitDoneMsg{
		ProjectKey:  c.SelectedProjectKey(),
		IssueTypeID: c.SelectedIssueType().ID,
		Err:         httpErr,
	})
	if !updated.Visible() {
		t.Fatal("400 should keep overlay visible")
	}
	if got := updated.SubmitError(); !strings.Contains(got, "something blew up") {
		t.Errorf("SubmitError = %q, want top-level error message", got)
	}
	view := stripANSI(updated.View(newStyles(t)))
	if !strings.Contains(view, "something blew up") {
		t.Errorf("Step 3 view missing generic server error\n%s", view)
	}
}

func TestCreate_SubmitDone_NonHTTPErrorShownGenerically(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	updated, _ := c.Update(CreateSubmitDoneMsg{
		ProjectKey:  c.SelectedProjectKey(),
		IssueTypeID: c.SelectedIssueType().ID,
		Err:         errors.New("network timeout"),
	})
	if !updated.Visible() {
		t.Fatal("network errors should keep the overlay visible")
	}
	if got := updated.SubmitError(); !strings.Contains(got, "network timeout") {
		t.Errorf("SubmitError = %q, want raw error string", got)
	}
}

func TestCreate_SubmitDone_StaleResponseDropped(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	updated, _ := c.Update(CreateSubmitDoneMsg{
		ProjectKey:  "OTHER",
		IssueTypeID: "9999",
		Issue:       jira.Issue{Key: "OTHER-1"},
	})
	if !updated.Visible() {
		t.Error("stale done msg must not affect overlay visibility")
	}
	if !updated.Submitting() {
		t.Error("stale done msg must not clear submitting state")
	}
}

func TestCreate_Submit_EscBlockedWhileSubmitting(t *testing.T) {
	c := readyForSubmit(t, true)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !c.Submitting() {
		t.Fatal("setup: submit should flip submitting=true")
	}
	updated, _ := c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Step() != 3 {
		t.Errorf("esc while submitting should be ignored, Step = %d", updated.Step())
	}
	if !updated.Submitting() {
		t.Error("esc while submitting must not clear submitting state")
	}
}
