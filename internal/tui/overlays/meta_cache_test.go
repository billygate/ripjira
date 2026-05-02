package overlays

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// advanceToFields drives c through Step1→Step2→Step3 picking projectKey and the
// issue type whose Name matches typeName. The CreateIssueTypesMsg reply is
// stubbed in-line. Returns the overlay parked on Step 3 and the cmd emitted by
// the final Enter (caller inspects it to detect cache hit vs. miss).
func advanceToFields(t *testing.T, c Create, projectKey, typeName string) (Create, tea.Cmd) {
	t.Helper()
	c, _ = c.Show(sampleProjects(), projectKey)
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c, _ = c.Update(CreateIssueTypesMsg{
		ProjectKey: projectKey,
		IssueTypes: sampleIssueTypes(),
	})
	c.typeInput.SetValue(typeName)
	c.typeCursor = 0
	c, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c.Step() != 3 {
		t.Fatalf("advanceToFields: Step = %d, want 3", c.Step())
	}
	return c, cmd
}

// TestCreate_MetaCache_HitSkipsRefetch is the core requirement of Task 25:
// once the overlay has received a CreateMetaLoadedMsg for project+type, a
// second open of the same project+type must surface the form immediately and
// must NOT emit a fresh CreateTypeChosenMsg (which is what triggers the
// network fetch in the root model).
func TestCreate_MetaCache_HitSkipsRefetch(t *testing.T) {
	c := NewCreate(closeBinding(), "")

	c, firstCmd := advanceToFields(t, c, "BETA", "Task")
	if _, ok := firstCmd().(CreateTypeChosenMsg); !ok {
		t.Fatalf("first open: enter on Step 2 did not emit CreateTypeChosenMsg, got %T", firstCmd())
	}
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	if !c.FormReady() {
		t.Fatal("setup: FormReady should be true after meta arrives")
	}

	c = c.Hide()

	c, secondCmd := advanceToFields(t, c, "BETA", "Task")
	if secondCmd != nil {
		t.Errorf("cache hit should return nil cmd, got %T", secondCmd())
	}
	if c.FormLoading() {
		t.Error("cache hit should skip the loading state")
	}
	if !c.FormReady() {
		t.Error("cache hit should make form ready immediately")
	}
	if got := len(c.Form().Fields); got == 0 {
		t.Error("cache hit should populate Form fields")
	}
}

// TestCreate_MetaCache_MissOnDifferentType verifies the cache is keyed by
// project+type — choosing a different type for the same project still triggers
// a fetch.
func TestCreate_MetaCache_MissOnDifferentType(t *testing.T) {
	c := NewCreate(closeBinding(), "")

	c, _ = advanceToFields(t, c, "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	c = c.Hide()

	c, cmd := advanceToFields(t, c, "BETA", "Bug")
	if cmd == nil {
		t.Fatal("different type should miss cache and emit cmd")
	}
	chosen, ok := cmd().(CreateTypeChosenMsg)
	if !ok {
		t.Fatalf("different type: enter did not emit CreateTypeChosenMsg, got %T", cmd())
	}
	if chosen.IssueType.Name != "Bug" {
		t.Errorf("chosen.IssueType.Name = %q, want Bug", chosen.IssueType.Name)
	}
	if !c.FormLoading() {
		t.Error("cache miss should enter loading state")
	}
	if c.FormReady() {
		t.Error("cache miss should not be ready before meta arrives")
	}
}

// TestCreate_MetaCache_MissOnDifferentProject verifies cache is keyed by
// project as well, not just type.
func TestCreate_MetaCache_MissOnDifferentProject(t *testing.T) {
	c := NewCreate(closeBinding(), "")

	c, _ = advanceToFields(t, c, "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Meta:        sampleMeta(),
	})
	c = c.Hide()

	_, cmd := advanceToFields(t, c, "ALPHA", "Task")
	if cmd == nil {
		t.Fatal("different project should miss cache and emit cmd")
	}
	chosen, ok := cmd().(CreateTypeChosenMsg)
	if !ok {
		t.Fatalf("different project: did not emit CreateTypeChosenMsg, got %T", cmd())
	}
	if chosen.ProjectKey != "ALPHA" {
		t.Errorf("chosen.ProjectKey = %q, want ALPHA", chosen.ProjectKey)
	}
}

// TestCreate_MetaCache_DoesNotCacheErrors makes sure an errored fetch is not
// cached — a retry must hit the network again.
func TestCreate_MetaCache_DoesNotCacheErrors(t *testing.T) {
	c := NewCreate(closeBinding(), "")

	c, _ = advanceToFields(t, c, "BETA", "Task")
	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: c.SelectedIssueType().ID,
		Err:         errBoom("createmeta down"),
	})
	if c.FormErr() == nil {
		t.Fatal("setup: FormErr should be populated")
	}
	c = c.Hide()

	_, cmd := advanceToFields(t, c, "BETA", "Task")
	if cmd == nil {
		t.Fatal("errored fetch must not be cached — retry must emit cmd")
	}
	if _, ok := cmd().(CreateTypeChosenMsg); !ok {
		t.Errorf("retry did not emit CreateTypeChosenMsg, got %T", cmd())
	}
}

// TestCreate_MetaCache_StalePayloadStillCaches verifies the subtle case where
// the user backs out before a fetch returns: the late-arriving CreateMetaLoaded
// msg is dropped from the live form (existing behavior), but its payload is
// still stashed so the next open of that project+type hits the cache.
func TestCreate_MetaCache_StalePayloadStillCaches(t *testing.T) {
	c := NewCreate(closeBinding(), "")

	c, _ = advanceToFields(t, c, "BETA", "Task")
	taskID := c.SelectedIssueType().ID

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc}) // step 3 → 2
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc}) // step 2 → 1

	c, _ = c.Update(CreateMetaLoadedMsg{
		ProjectKey:  "BETA",
		IssueTypeID: taskID,
		Meta:        sampleMeta(),
	})
	if c.FormReady() {
		t.Error("stale payload must not flip live FormReady")
	}

	c = c.Hide()
	c, cmd := advanceToFields(t, c, "BETA", "Task")
	if cmd != nil {
		t.Errorf("stale-cached payload should serve next open, got cmd %T", cmd())
	}
	if !c.FormReady() {
		t.Error("stale-cached payload should make form ready immediately on re-open")
	}
}
