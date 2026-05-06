package overlays

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

func TestDetectFieldKind_SchemaTable(t *testing.T) {
	cases := []struct {
		name string
		meta jira.FieldMeta
		want FieldKind
	}{
		{"summary string", jira.FieldMeta{ID: "summary", SchemaType: "string"}, FieldKindString},
		{"description ADF", jira.FieldMeta{ID: "description", SchemaType: "string"}, FieldKindADF},
		{"option", jira.FieldMeta{ID: "customfield_1", SchemaType: "option"}, FieldKindOption},
		{"priority", jira.FieldMeta{ID: "priority", SchemaType: "priority"}, FieldKindOption},
		{"issuetype", jira.FieldMeta{ID: "issuetype", SchemaType: "issuetype"}, FieldKindOption},
		{"user", jira.FieldMeta{ID: "assignee", SchemaType: "user"}, FieldKindUser},
		{"array<option>", jira.FieldMeta{ID: "components", SchemaType: "array", SchemaItems: "option"}, FieldKindMultiOption},
		{"array<string>", jira.FieldMeta{ID: "labels", SchemaType: "array", SchemaItems: "string"}, FieldKindUnknown},
		{"number", jira.FieldMeta{ID: "estimate", SchemaType: "number"}, FieldKindNumber},
		{"date", jira.FieldMeta{ID: "duedate", SchemaType: "date"}, FieldKindDate},
		{"datetime", jira.FieldMeta{ID: "starttime", SchemaType: "datetime"}, FieldKindDate},
		{"unknown", jira.FieldMeta{ID: "weird", SchemaType: "attachment"}, FieldKindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectFieldKind(c.meta)
			if got != c.want {
				t.Errorf("detectFieldKind(%+v) = %s, want %s", c.meta, got, c.want)
			}
		})
	}
}

func sampleMeta() jira.CreateMeta {
	return jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "summary", Name: "Summary", Required: true, SchemaType: "string"},
		{ID: "description", Name: "Description", SchemaType: "string"},
		{ID: "priority", Name: "Priority", SchemaType: "priority", AllowedValues: []jira.FieldOption{
			{ID: "1", Name: "High"},
			{ID: "2", Name: "Medium"},
			{ID: "3", Name: "Low"},
		}},
		{ID: "assignee", Name: "Assignee", SchemaType: "user"},
		{ID: "components", Name: "Components", SchemaType: "array", SchemaItems: "option", AllowedValues: []jira.FieldOption{
			{ID: "10", Name: "core"},
			{ID: "11", Name: "ui"},
			{ID: "12", Name: "api"},
		}},
		{ID: "estimate", Name: "Estimate", SchemaType: "number"},
		{ID: "duedate", Name: "Due", SchemaType: "date"},
		{ID: "labels", Name: "Labels", SchemaType: "array", SchemaItems: "string"}, // unknown
		{ID: "attachments", Name: "Attachments", SchemaType: "attachment"},         // unknown
	}}
}

func TestBuildForm_DropsUnknownFieldsWithWarnings(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	if got, want := len(form.Fields), 7; got != want {
		t.Fatalf("Fields len = %d, want %d", got, want)
	}
	warns := form.Warnings()
	if len(warns) != 2 {
		t.Fatalf("Warnings = %v, want 2 entries", warns)
	}
	joined := strings.Join(warns, "|")
	for _, want := range []string{"Labels", "Attachments"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Warnings missing %q (got %v)", want, warns)
		}
	}
}

func TestBuildForm_FocusesFirstField(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	if form.Focus() != 0 {
		t.Errorf("Focus = %d, want 0", form.Focus())
	}
	if !form.Fields[0].Focused() {
		t.Error("first field should be focused after BuildForm")
	}
	for i := 1; i < len(form.Fields); i++ {
		if form.Fields[i].Focused() {
			t.Errorf("field %d should not be focused", i)
		}
	}
}

func TestBuildForm_KindsMatchSchemaTable(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	want := []FieldKind{
		FieldKindString,
		FieldKindADF,
		FieldKindOption,
		FieldKindUser,
		FieldKindMultiOption,
		FieldKindNumber,
		FieldKindDate,
	}
	if len(form.Fields) != len(want) {
		t.Fatalf("Fields len = %d, want %d", len(form.Fields), len(want))
	}
	for i, w := range want {
		if got := form.Fields[i].Kind; got != w {
			t.Errorf("Fields[%d].Kind = %s, want %s", i, got, w)
		}
	}
}

func TestForm_TabAndShiftTabNavigate(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.Focus() != 1 {
		t.Errorf("after tab Focus = %d, want 1", form.Focus())
	}
	if !form.Fields[1].Focused() || form.Fields[0].Focused() {
		t.Errorf("after tab focus state incorrect: 0=%v 1=%v",
			form.Fields[0].Focused(), form.Fields[1].Focused())
	}

	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.Focus() != 0 {
		t.Errorf("after shift-tab Focus = %d, want 0", form.Focus())
	}

	// Shift-tab from 0 wraps to last.
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if want := len(form.Fields) - 1; form.Focus() != want {
		t.Errorf("after shift-tab from first Focus = %d, want %d (wrap)",
			form.Focus(), want)
	}

	// Tab from last wraps to 0.
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.Focus() != 0 {
		t.Errorf("after tab from last Focus = %d, want 0 (wrap)", form.Focus())
	}
}

func TestForm_TextInputForwardsToFocusedField(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if got := form.Fields[0].Value(); got != "hi" {
		t.Errorf("focused string field value = %q, want %q", got, "hi")
	}
	if form.Fields[1].Value() != "" {
		t.Errorf("non-focused field should not have received input: %q", form.Fields[1].Value())
	}
}

func TestField_OptionUpDownCyclesAllowedValues(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	for form.Focus() != 2 { // priority
		var c tea.Cmd
		form, c = form.Update(tea.KeyMsg{Type: tea.KeyTab})
		_ = c
	}
	if form.Fields[2].Value() != "1" {
		t.Errorf("option default value = %q, want id=1", form.Fields[2].Value())
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.Fields[2].Value() != "2" {
		t.Errorf("after down Value = %q, want 2", form.Fields[2].Value())
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown}) // clamped
	if form.Fields[2].Value() != "3" {
		t.Errorf("after clamped down Value = %q, want 3", form.Fields[2].Value())
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyUp})
	if form.Fields[2].Value() != "2" {
		t.Errorf("after up Value = %q, want 2", form.Fields[2].Value())
	}
}

func TestBuildForm_ReordersSummaryAndDescriptionFirst(t *testing.T) {
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "priority", Name: "Priority", SchemaType: "priority", AllowedValues: []jira.FieldOption{{ID: "1", Name: "High"}}},
		{ID: "assignee", Name: "Assignee", SchemaType: "user"},
		{ID: "summary", Name: "Summary", SchemaType: "string"},
		{ID: "description", Name: "Description", SchemaType: "string"},
		{ID: "duedate", Name: "Due", SchemaType: "date"},
	}}
	form := BuildForm(meta, FormDefaults{})
	wantOrder := []string{"summary", "description", "priority", "assignee", "duedate"}
	if len(form.Fields) != len(wantOrder) {
		t.Fatalf("fields len = %d, want %d", len(form.Fields), len(wantOrder))
	}
	for i, id := range wantOrder {
		if got := form.Fields[i].Meta.ID; got != id {
			t.Errorf("Fields[%d].ID = %q, want %q", i, got, id)
		}
	}
	if !form.Fields[0].Focused() {
		t.Error("first field (summary) should be focused")
	}
}

func TestBuildForm_ReorderPreservesWhenSummaryAbsent(t *testing.T) {
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "priority", Name: "Priority", SchemaType: "priority", AllowedValues: []jira.FieldOption{{ID: "1", Name: "High"}}},
		{ID: "assignee", Name: "Assignee", SchemaType: "user"},
	}}
	form := BuildForm(meta, FormDefaults{})
	if len(form.Fields) != 2 {
		t.Fatalf("fields len = %d, want 2", len(form.Fields))
	}
	if form.Fields[0].Meta.ID != "priority" || form.Fields[1].Meta.ID != "assignee" {
		t.Errorf("order = [%s, %s], want [priority, assignee]",
			form.Fields[0].Meta.ID, form.Fields[1].Meta.ID)
	}
}

func TestField_OptionViewMarksSelectedRegardlessOfFocus(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	// Move cursor to priority (focused).
	for form.Focus() != 2 {
		form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown}) // value -> "2" (Medium)

	st := epicTestStyles()
	focusedView := form.Fields[2].View(st)
	if !strings.Contains(focusedView, "● Medium") {
		t.Errorf("focused option view missing ● marker on Medium:\n%s", focusedView)
	}
	if !strings.Contains(focusedView, "○ High") || !strings.Contains(focusedView, "○ Low") {
		t.Errorf("focused option view missing ○ on unselected rows:\n%s", focusedView)
	}

	// Tab away so priority is no longer focused.
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.Fields[2].Focused() {
		t.Fatal("priority field should not be focused after tab")
	}
	unfocused := form.Fields[2].View(st)
	if !strings.Contains(unfocused, "● Medium") {
		t.Errorf("unfocused option view should still mark Medium with ●:\n%s", unfocused)
	}
}

func TestField_MultiOptionToggleWithSpace(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	for form.Focus() != 4 { // components
		form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	v, ok := form.Fields[4].PayloadValue()
	if !ok {
		t.Fatal("multi-option payload should be present after toggling 2 rows")
	}
	arr, ok := v.([]map[string]string)
	if !ok {
		t.Fatalf("multi-option payload type = %T, want []map[string]string", v)
	}
	if len(arr) != 2 {
		t.Fatalf("multi-option payload len = %d, want 2 (got %v)", len(arr), arr)
	}
	gotIDs := []string{arr[0]["id"], arr[1]["id"]}
	wantIDs := []string{"10", "12"}
	if gotIDs[0] != wantIDs[0] || gotIDs[1] != wantIDs[1] {
		t.Errorf("multi-option payload ids = %v, want %v", gotIDs, wantIDs)
	}
}

func TestField_Validate_RequiredEmptyAndTypeErrors(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	form, errs := form.Validate()
	if msg, ok := errs["summary"]; !ok || msg == "" {
		t.Errorf("expected required-error on summary, got %v", errs)
	}
	if form.Fields[0].Error() == "" {
		t.Error("Validate should bind error to the summary field")
	}

	// Set summary, leave bad number, leave bad date.
	form.Fields[0] = form.Fields[0].SetValue("Hello")
	form.Fields[5] = form.Fields[5].SetValue("not a number")
	form.Fields[6] = form.Fields[6].SetValue("nope")

	form, errs = form.Validate()
	if _, ok := errs["summary"]; ok {
		t.Errorf("summary should now validate: %v", errs)
	}
	if msg, ok := errs["estimate"]; !ok || !strings.Contains(msg, "number") {
		t.Errorf("estimate validate err = %q, want number error", msg)
	}
	if msg, ok := errs["duedate"]; !ok || !strings.Contains(msg, "YYYY") {
		t.Errorf("duedate validate err = %q, want mask error", msg)
	}

	// Fix them, then assert clean.
	form.Fields[5] = form.Fields[5].SetValue("3.5")
	form.Fields[6] = form.Fields[6].SetValue("2026-04-30")
	form, errs = form.Validate()
	if len(errs) != 0 {
		t.Errorf("expected clean validation, got %v", errs)
	}
}

func TestField_PayloadValue_Shapes(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})

	// Fill in all kinds with valid values.
	form.Fields[0] = form.Fields[0].SetValue("Title")                                         // string
	form.Fields[1] = form.Fields[1].SetValue("Body text")                                     // adf
	form.Fields[2] = form.Fields[2].SetValue("2")                                             // option (priority id)
	form.Fields[3].SetUserSelection(jira.User{AccountID: "acct-1", DisplayName: "Test User"}) // user
	// components: select id 11
	form.Fields[4] = form.Fields[4].SetValue("11")
	form.Fields[5] = form.Fields[5].SetValue("4.25")
	form.Fields[6] = form.Fields[6].SetValue("2026-05-01")

	if v, _ := form.Fields[0].PayloadValue(); v != "Title" {
		t.Errorf("string payload = %v", v)
	}
	if v, _ := form.Fields[1].PayloadValue(); v != "Body text" {
		t.Errorf("adf payload = %v (string passthrough expected; ADF wrap done by client)", v)
	}
	if v, _ := form.Fields[2].PayloadValue(); !mapHasKV(v, "id", "2") {
		t.Errorf("option payload = %v, want {id:2}", v)
	}
	if v, _ := form.Fields[3].PayloadValue(); !mapHasKV(v, "accountId", "acct-1") {
		t.Errorf("user payload = %v, want {accountId:acct-1}", v)
	}
	if v, _ := form.Fields[5].PayloadValue(); v != 4.25 {
		t.Errorf("number payload = %v, want 4.25", v)
	}
	if v, _ := form.Fields[6].PayloadValue(); v != "2026-05-01" {
		t.Errorf("date payload = %v", v)
	}
}

func mapHasKV(v any, key, want string) bool {
	m, ok := v.(map[string]string)
	if !ok {
		return false
	}
	return m[key] == want
}

func TestForm_BuildPayload_ExtractsStandardFields(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	form.Fields[0] = form.Fields[0].SetValue("New issue")
	form.Fields[1] = form.Fields[1].SetValue("Body")
	form.Fields[2] = form.Fields[2].SetValue("3") // priority Low
	form.Fields[3].SetUserSelection(jira.User{AccountID: "acct-9", DisplayName: "Test User"})
	form.Fields[4] = form.Fields[4].SetValue("11")
	form.Fields[5] = form.Fields[5].SetValue("8")
	form.Fields[6] = form.Fields[6].SetValue("2026-12-31")

	p := form.BuildPayload("PROJ", "10001")
	if p.ProjectKey != "PROJ" || p.IssueTypeID != "10001" {
		t.Errorf("project/type missing: %+v", p)
	}
	if p.Summary != "New issue" {
		t.Errorf("Summary = %q", p.Summary)
	}
	if p.Description != "Body" {
		t.Errorf("Description = %q", p.Description)
	}
	if p.Priority != "3" {
		t.Errorf("Priority = %q, want 3", p.Priority)
	}
	if p.Assignee != "acct-9" {
		t.Errorf("Assignee = %q", p.Assignee)
	}
	// extras
	if p.Fields == nil {
		t.Fatal("Fields map missing")
	}
	if v, ok := p.Fields["components"]; !ok {
		t.Errorf("components missing from extras: %+v", p.Fields)
	} else {
		arr, _ := v.([]map[string]string)
		if len(arr) != 1 || arr[0]["id"] != "11" {
			t.Errorf("components extras = %v", arr)
		}
	}
	if v, ok := p.Fields["estimate"]; !ok || v != 8.0 {
		t.Errorf("estimate extras = %v", v)
	}
	if v, ok := p.Fields["duedate"]; !ok || v != "2026-12-31" {
		t.Errorf("duedate extras = %v", v)
	}
}

func TestForm_View_RendersLabelsAndOptions(t *testing.T) {
	form := BuildForm(sampleMeta(), FormDefaults{})
	view := stripANSI(form.View(newStyles(t)))
	for _, want := range []string{
		"Summary", "*",
		"Description",
		"Priority", "High", "Medium", "Low",
		"Assignee",
		"Components", "core", "ui", "api",
		"Estimate",
		"Due",
		"Skipped unsupported fields",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("form view missing %q\n%s", want, view)
		}
	}
}

func TestForm_Empty_View(t *testing.T) {
	form := BuildForm(jira.CreateMeta{}, FormDefaults{})
	if got := form.Focus(); got != -1 {
		t.Errorf("empty form Focus = %d, want -1", got)
	}
	view := stripANSI(form.View(newStyles(t)))
	if !strings.Contains(view, "no fields") {
		t.Errorf("empty form view should say 'no fields': %s", view)
	}
}

// --- Create overlay step 3 integration ---

func TestCreate_EnterStep2AdvancesToStep3WithLoading(t *testing.T) {
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 1 → 2
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 2 → 3
	if c.Step() != 3 {
		t.Errorf("after enter Step = %d, want 3", c.Step())
	}
	if !c.FormLoading() {
		t.Error("Step 3 should start in loading state")
	}
	if c.FormReady() {
		t.Error("FormReady should be false until CreateMetaLoadedMsg")
	}
	chosen, ok := cmd().(CreateTypeChosenMsg)
	if !ok {
		t.Fatalf("enter on Step 2 did not emit CreateTypeChosenMsg")
	}
	if chosen.IssueType.Name != "Task" {
		t.Errorf("chosen.IssueType.Name = %q, want Task", chosen.IssueType.Name)
	}
}

func TestCreate_MetaLoadedRendersForm(t *testing.T) {
	c := openStep3(t)
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	if c.FormLoading() {
		t.Error("FormLoading should clear after meta arrives")
	}
	if !c.FormReady() {
		t.Error("FormReady should flip true after meta arrives")
	}
	if got := len(c.Form().Fields); got != 7 {
		t.Errorf("form fields = %d, want 7", got)
	}
	view := stripANSI(c.View(newStyles(t)))
	for _, want := range []string{"Step 3/3", "Summary", "Components", "core"} {
		if !strings.Contains(view, want) {
			t.Errorf("Step 3 view missing %q\n%s", want, view)
		}
	}
}

func TestCreate_MetaLoadedIgnoredForOtherSelection(t *testing.T) {
	c := openStep3(t)
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "OTHER",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	if c.FormReady() {
		t.Error("stale meta msg should not flip FormReady")
	}
}

func TestCreate_MetaLoadedErrorSurfaces(t *testing.T) {
	c := openStep3(t)
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Err:         errBoom("createmeta"),
	})
	if c.FormErr() == nil {
		t.Fatal("FormErr should be populated after error reply")
	}
	view := stripANSI(c.View(newStyles(t)))
	if !strings.Contains(view, "createmeta") {
		t.Errorf("Step 3 view missing error: %s", view)
	}
}

func TestCreate_EscOnStep3GoesBackToStep2(t *testing.T) {
	c := openStep3(t)
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c.Step() != 2 {
		t.Errorf("esc on Step 3 → Step %d, want 2", c.Step())
	}
	if c.FormReady() || c.FormLoading() {
		t.Error("Step 3 state should be cleared after going back")
	}
}

func openStep3(t *testing.T) Create {
	t.Helper()
	c, _ := NewCreate(closeBinding(), "").Show(sampleProjects(), "BETA")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: "BETA",
		IssueTypes: sampleIssueTypes(),
	})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c.Step() != 3 {
		t.Fatalf("openStep3 setup: Step = %d", c.Step())
	}
	return c
}

type errString string

func (e errString) Error() string { return string(e) }

func errBoom(s string) error { return errString(s) }

func TestField_UserCommitStoresAccountID(t *testing.T) {
	f := newField(jira.FieldMeta{ID: "assignee", Name: "Assignee"}, FieldKindUser)
	f.SetUserSelection(jira.User{AccountID: "acc-123", DisplayName: "Sample U."})
	if got := f.UserAccountID(); got != "acc-123" {
		t.Fatalf("UserAccountID = %q, want acc-123", got)
	}
	if got := f.text.Value(); got != "Sample U." {
		t.Fatalf("text value = %q, want Sample U.", got)
	}
	val, ok := f.SubmitValue()
	if !ok {
		t.Fatal("SubmitValue should return ok=true after selection")
	}
	m, isMap := val.(map[string]string)
	if !isMap || m["accountId"] != "acc-123" {
		t.Fatalf("SubmitValue = %v, want {accountId: acc-123}", val)
	}
}

func TestField_UserEditAfterCommitClearsAccountID(t *testing.T) {
	f := newField(jira.FieldMeta{ID: "assignee", Name: "Assignee"}, FieldKindUser)
	f.SetUserSelection(jira.User{AccountID: "acc-123", DisplayName: "Sample U."})
	f.text.SetValue("Sample")
	f.OnTextChanged()
	if f.UserAccountID() != "" {
		t.Fatalf("UserAccountID should be cleared after edit, got %q", f.UserAccountID())
	}
	if _, ok := f.SubmitValue(); ok {
		t.Fatal("SubmitValue should report unset after edit")
	}
}

func TestBuildForm_ReporterAutoFilled(t *testing.T) {
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "summary", Name: "Summary", SchemaType: "string"},
		{ID: "reporter", Name: "Reporter", SchemaType: "user"},
	}}
	f := BuildForm(meta, FormDefaults{CurrentUserAccountID: "acc-me"})

	var rep *Field
	for i := range f.Fields {
		if f.Fields[i].Meta.ID == "reporter" {
			rep = &f.Fields[i]
			break
		}
	}
	if rep == nil {
		t.Fatal("reporter field not built")
	}
	if rep.UserAccountID() != "acc-me" {
		t.Fatalf("reporter accountID = %q, want acc-me", rep.UserAccountID())
	}
	if got := rep.text.Value(); got != "(me)" {
		t.Fatalf("reporter text = %q, want (me)", got)
	}
}

func TestBuildForm_ReporterDefaultEmpty(t *testing.T) {
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "reporter", Name: "Reporter", SchemaType: "user"},
	}}
	f := BuildForm(meta, FormDefaults{})
	rep := &f.Fields[0]
	if rep.UserAccountID() != "" {
		t.Fatalf("reporter should be empty when default missing, got %q", rep.UserAccountID())
	}
	if rep.text.Value() != "" {
		t.Fatalf("reporter text should be empty, got %q", rep.text.Value())
	}
}

func TestField_UserDropdownNavigationAndCommit(t *testing.T) {
	f := newField(jira.FieldMeta{ID: "assignee", Name: "Assignee"}, FieldKindUser)
	f.dropdown.open = true
	f.dropdown.results = []jira.User{
		{AccountID: "a1", DisplayName: "Alice"},
		{AccountID: "b2", DisplayName: "Bob"},
	}
	f.dropdown.cursor = 0

	var cmd tea.Cmd
	f, cmd = f.UpdateUser(tea.KeyMsg{Type: tea.KeyDown})
	_ = cmd
	if f.dropdown.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", f.dropdown.cursor)
	}
	f, _ = f.UpdateUser(tea.KeyMsg{Type: tea.KeyEnter})
	if f.UserAccountID() != "b2" {
		t.Fatalf("after enter, accountID = %q", f.UserAccountID())
	}
	if f.dropdown.open {
		t.Fatal("dropdown should close after commit")
	}
}

func TestField_UserDropdownResultsHonourToken(t *testing.T) {
	f := newField(jira.FieldMeta{ID: "assignee"}, FieldKindUser)
	f.dropdown.token = 5
	f.handleSearchResults(UserSearchResultsMsg{
		FieldID: "assignee",
		Token:   3, // stale
		Users:   []jira.User{{AccountID: "stale"}},
	})
	if len(f.dropdown.results) != 0 {
		t.Fatalf("stale results applied, got %d", len(f.dropdown.results))
	}
	f.handleSearchResults(UserSearchResultsMsg{
		FieldID: "assignee",
		Token:   5,
		Users:   []jira.User{{AccountID: "fresh"}},
	})
	if len(f.dropdown.results) != 1 || f.dropdown.results[0].AccountID != "fresh" {
		t.Fatalf("fresh results not applied: %+v", f.dropdown.results)
	}
}

func TestField_UserTypingArmsDebounce(t *testing.T) {
	f := newField(jira.FieldMeta{ID: "assignee"}, FieldKindUser)
	initialToken := f.dropdown.token
	f, cmd := f.UpdateUser(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("typing should return a non-nil cmd (debounce timer)")
	}
	if !f.dropdown.open {
		t.Fatal("dropdown should be marked open after typing")
	}
	if f.dropdown.token == initialToken {
		t.Fatal("token should bump on each keystroke for cancel-on-supersede")
	}
}
