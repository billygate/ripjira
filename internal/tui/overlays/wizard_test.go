package overlays

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
)

// stubVerifier records verify/listProjects calls and returns canned responses.
type stubVerifier struct {
	verifyCalls   int
	verifyUser    jira.User
	verifyErr     error
	verifyArgs    []string
	listCalls     int
	listProjects  []jira.Project
	listProjects2 error
}

func (s *stubVerifier) Verify(_ context.Context, baseURL, email, token string) (jira.User, error) {
	s.verifyCalls++
	s.verifyArgs = []string{baseURL, email, token}
	return s.verifyUser, s.verifyErr
}

func (s *stubVerifier) ListProjects(_ context.Context, _, _, _ string) ([]jira.Project, error) {
	s.listCalls++
	return s.listProjects, s.listProjects2
}

func newWizardOpts(t *testing.T, v *stubVerifier, store config.SecretStore) (overlayOpts WizardOptions, cfgPath string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "config.yaml")
	return WizardOptions{
		Verifier: v,
		Store:    store,
		CfgPath:  cfgPath,
		CloseKey: closeBinding(),
	}, cfgPath
}

func typeRunes(w Wizard, s string) Wizard {
	for _, r := range s {
		w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return w
}

func TestWizard_StartsOnURL(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)
	if got := w.Step(); got != WizardStepURL {
		t.Errorf("step = %v, want WizardStepURL", got)
	}
}

func TestWizard_URLToEmailAdvancesOnEnter(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if w.Step() != WizardStepEmail {
		t.Fatalf("step = %v, want WizardStepEmail", w.Step())
	}
}

func TestWizard_URLValidationBlocksAdvance(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "not a url")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if w.Step() != WizardStepURL {
		t.Fatalf("invalid URL must keep step, got %v", w.Step())
	}
	if w.VerifyError() == "" {
		t.Error("invalid URL must surface error message")
	}
}

func TestWizard_EmailValidationBlocksAdvance(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "not-an-email")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if w.Step() != WizardStepEmail {
		t.Fatalf("invalid email must keep step, got %v", w.Step())
	}
	if w.VerifyError() == "" {
		t.Error("invalid email must surface error message")
	}
}

func TestWizard_EscFromEmailGoesBack(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if w.Step() != WizardStepURL {
		t.Errorf("esc from email should return to URL, got %v", w.Step())
	}
}

func TestWizard_TokenSubmitDispatchesVerify(t *testing.T) {
	v := &stubVerifier{verifyUser: jira.User{AccountID: "abc", DisplayName: "Alice"}, listProjects: []jira.Project{{Key: "PROJ", Name: "Project"}}}
	opts, _ := newWizardOpts(t, v, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "alice@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "secret-token")
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if w.Step() != WizardStepVerifying {
		t.Fatalf("after token enter, step = %v, want WizardStepVerifying", w.Step())
	}
	if cmd == nil {
		t.Fatal("token enter must return a verify cmd")
	}

	msg := cmd()
	w, _ = w.Update(msg)
	if v.verifyCalls != 1 {
		t.Errorf("Verify calls = %d, want 1", v.verifyCalls)
	}
	if v.verifyArgs[2] != "secret-token" {
		t.Errorf("token forwarded as %q, want secret-token", v.verifyArgs[2])
	}
	if w.Step() != WizardStepProject {
		t.Errorf("after verify success, step = %v, want WizardStepProject", w.Step())
	}
}

func TestWizard_TokenSubmitWithEmptyValueIsBlocked(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "alice@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if w.Step() != WizardStepToken {
		t.Errorf("empty token should keep step, got %v", w.Step())
	}
	if cmd != nil {
		t.Errorf("empty token should not dispatch a verify cmd, got %v", cmd)
	}
}

func TestWizard_VerifyFailureReturnsToToken(t *testing.T) {
	v := &stubVerifier{verifyErr: errors.New("401 Unauthorized")}
	opts, _ := newWizardOpts(t, v, config.NewFakeStore())
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "alice@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "bad-token")
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	w, _ = w.Update(cmd())

	if w.Step() != WizardStepToken {
		t.Fatalf("after verify failure, step = %v, want WizardStepToken", w.Step())
	}
	if !strings.Contains(w.VerifyError(), "401") {
		t.Errorf("verify error = %q, want it to mention 401", w.VerifyError())
	}
}

func TestWizard_ProjectSkipSavesEmptyDefault(t *testing.T) {
	v := &stubVerifier{verifyUser: jira.User{DisplayName: "Bob"}}
	store := config.NewFakeStore()
	opts, cfgPath := newWizardOpts(t, v, store)
	w := NewWizard(opts)

	// Step through to project step.
	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "bob@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "tok")
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w, _ = w.Update(cmd())

	// Skip default project.
	w, savedCmd := w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !w.Done() {
		t.Fatal("esc on project step should save and finish")
	}
	if w.Config() == nil {
		t.Fatal("Done wizard must expose saved config")
	}
	if w.Config().DefaultProject != "" {
		t.Errorf("default project = %q, want empty (skipped)", w.Config().DefaultProject)
	}
	if w.Config().BaseURL != "https://acme.atlassian.net" {
		t.Errorf("saved base URL = %q", w.Config().BaseURL)
	}
	if w.Config().Email != "bob@example.com" {
		t.Errorf("saved email = %q", w.Config().Email)
	}

	// SecretStore received the token.
	got, err := store.Get("bob@example.com")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got != "tok" {
		t.Errorf("stored token = %q, want %q", got, "tok")
	}

	// Config file exists with 0600 perms.
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat cfg: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("config perms %v wider than 0600", info.Mode().Perm())
	}

	// Saved cmd publishes WizardSavedMsg.
	if savedCmd == nil {
		t.Fatal("save should publish WizardSavedMsg cmd")
	}
	if _, ok := savedCmd().(WizardSavedMsg); !ok {
		t.Errorf("save cmd produced %T, want WizardSavedMsg", savedCmd())
	}
}

func TestWizard_ProjectSelectSavesChosenKey(t *testing.T) {
	v := &stubVerifier{
		verifyUser:   jira.User{DisplayName: "Carol"},
		listProjects: []jira.Project{{Key: "ALPHA", Name: "Alpha"}, {Key: "BETA", Name: "Beta"}, {Key: "GAMMA", Name: "Gamma"}},
	}
	store := config.NewFakeStore()
	opts, _ := newWizardOpts(t, v, store)
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "carol@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "tok")
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w, projectsCmd := w.Update(cmd())
	if projectsCmd == nil {
		t.Fatal("verify success must dispatch a project-fetch cmd")
	}
	w, _ = w.Update(projectsCmd())

	// Move down once, then enter — should pick BETA.
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyDown})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !w.Done() {
		t.Fatal("enter on project step should save")
	}
	if got := w.Config().DefaultProject; got != "BETA" {
		t.Errorf("default project = %q, want BETA", got)
	}
}

func TestWizard_ProjectFilterNarrowsList(t *testing.T) {
	v := &stubVerifier{
		verifyUser:   jira.User{DisplayName: "Carol"},
		listProjects: []jira.Project{{Key: "ALPHA", Name: "Alpha"}, {Key: "BETA", Name: "Beta"}, {Key: "GAMMAB", Name: "Gamma"}},
	}
	store := config.NewFakeStore()
	opts, _ := newWizardOpts(t, v, store)
	w := NewWizard(opts)

	w = typeRunes(w, "https://acme.atlassian.net")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "carol@example.com")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = typeRunes(w, "tok")
	w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w, projectsCmd := w.Update(cmd())
	w, _ = w.Update(projectsCmd())

	// Filter by "B" — matches BETA and GAMMAB by key.
	w = typeRunes(w, "B")
	matches := w.filteredProjects()
	if len(matches) != 2 {
		t.Errorf("filtered len = %d, want 2", len(matches))
	}
	for _, m := range matches {
		if !strings.Contains(strings.ToLower(m.Key), "b") && !strings.Contains(strings.ToLower(m.Name), "b") {
			t.Errorf("match %v should contain b", m)
		}
	}
}

func TestWizard_StaleVerifyMsgIsIgnored(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	// Wizard is at URL step; a verify message arriving here is stale.
	updated, cmd := w.Update(wizardVerifyDoneMsg{user: jira.User{DisplayName: "Ghost"}})
	if updated.Step() != WizardStepURL {
		t.Errorf("stale verify msg moved step to %v", updated.Step())
	}
	if cmd != nil {
		t.Errorf("stale verify msg returned cmd %v", cmd)
	}
}

func TestWizard_InitialSeedsURLAndEmail(t *testing.T) {
	initial := &config.Config{
		BaseURL:         "https://seed.atlassian.net",
		Email:           "seed@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	opts := WizardOptions{
		Verifier: &stubVerifier{},
		Store:    config.NewFakeStore(),
		CfgPath:  filepath.Join(t.TempDir(), "config.yaml"),
		Initial:  initial,
		CloseKey: closeBinding(),
	}
	w := NewWizard(opts)

	if w.urlInput.Value() != initial.BaseURL {
		t.Errorf("seeded URL = %q, want %q", w.urlInput.Value(), initial.BaseURL)
	}
	if w.emailInput.Value() != initial.Email {
		t.Errorf("seeded email = %q, want %q", w.emailInput.Value(), initial.Email)
	}
}

func TestWizard_QuitFromURLReturnsTeaQuit(t *testing.T) {
	opts, _ := newWizardOpts(t, &stubVerifier{}, config.NewFakeStore())
	w := NewWizard(opts)

	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc on URL step should produce a quit cmd")
	}
}
