package overlays

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// WizardStep enumerates the lifecycle of the first-run wizard overlay.
type WizardStep int

const (
	// WizardStepURL captures the Jira Cloud base URL.
	WizardStepURL WizardStep = iota
	// WizardStepEmail captures the user's account email.
	WizardStepEmail
	// WizardStepToken captures the API token (password input).
	WizardStepToken
	// WizardStepVerifying is shown while /myself is being called.
	WizardStepVerifying
	// WizardStepProject is the optional default-project picker.
	WizardStepProject
	// WizardStepDone signals success — config and token have been saved.
	WizardStepDone
)

// WizardVerifier abstracts the network calls the wizard needs to make.
// Implementations build a temporary jira.Client per call so that each step
// uses whatever the user has typed in so far.
type WizardVerifier interface {
	Verify(ctx context.Context, baseURL, email, token string) (jira.User, error)
	ListProjects(ctx context.Context, baseURL, email, token string) ([]jira.Project, error)
}

// WizardSavedMsg is published once the wizard has written config + secret and
// the host program may proceed (or exit, in the case of `ripjira login`).
type WizardSavedMsg struct {
	Config *config.Config
}

// wizardVerifyDoneMsg carries the result of the /myself probe back to the
// wizard. Stale messages are discarded by checking step on receipt.
type wizardVerifyDoneMsg struct {
	user jira.User
	err  error
}

// wizardProjectsLoadedMsg carries the projects fetched after a successful
// verification.
type wizardProjectsLoadedMsg struct {
	projects []jira.Project
	err      error
}

// Wizard is the first-run overlay. Unlike the other overlays it is designed to
// run as the only widget in a Bubble Tea program, since by definition there
// is no main TUI to host it before config exists.
type Wizard struct {
	step WizardStep

	urlInput     textinput.Model
	emailInput   textinput.Model
	tokenInput   textinput.Model
	projectInput textinput.Model

	verifier WizardVerifier
	store    config.SecretStore
	cfgPath  string

	initial *config.Config

	user        jira.User
	projects    []jira.Project
	projectsErr error
	verifyErr   string
	saveErr     string

	selectedProject int

	closeBinding key.Binding

	saved bool
	final *config.Config
}

// WizardOptions bundles the dependencies the wizard needs at construction time.
type WizardOptions struct {
	Verifier WizardVerifier
	Store    config.SecretStore
	CfgPath  string
	// Initial seeds the textinputs with existing values. May be nil.
	Initial *config.Config
	// CloseKey is the binding that closes/aborts a step (typically esc).
	CloseKey key.Binding
}

// NewWizard constructs a wizard ready to be wrapped in a tea.Program.
func NewWizard(opts WizardOptions) Wizard {
	urlInput := textinput.New()
	urlInput.Placeholder = "https://acme.atlassian.net"
	urlInput.Prompt = "  "
	urlInput.Width = 50

	emailInput := textinput.New()
	emailInput.Placeholder = "you@example.com"
	emailInput.Prompt = "  "
	emailInput.Width = 50

	tokenInput := textinput.New()
	tokenInput.Placeholder = "API token"
	tokenInput.Prompt = "  "
	tokenInput.Width = 50
	tokenInput.EchoMode = textinput.EchoPassword
	tokenInput.EchoCharacter = '•'

	projectInput := textinput.New()
	projectInput.Placeholder = "filter projects (or esc to skip)"
	projectInput.Prompt = "  "
	projectInput.Width = 50

	if opts.Initial != nil {
		urlInput.SetValue(opts.Initial.BaseURL)
		emailInput.SetValue(opts.Initial.Email)
	}

	w := Wizard{
		step:         WizardStepURL,
		urlInput:     urlInput,
		emailInput:   emailInput,
		tokenInput:   tokenInput,
		projectInput: projectInput,
		verifier:     opts.Verifier,
		store:        opts.Store,
		cfgPath:      opts.CfgPath,
		initial:      opts.Initial,
		closeBinding: opts.CloseKey,
	}
	w.urlInput.Focus()
	return w
}

// Step exposes the current wizard step (mostly for tests).
func (w Wizard) Step() WizardStep { return w.step }

// Done reports whether the wizard has completed successfully.
func (w Wizard) Done() bool { return w.saved }

// Config returns the saved config once the wizard is done.
func (w Wizard) Config() *config.Config { return w.final }

// VerifyError returns the message displayed under the token field after a
// failed /myself probe (empty when there is no error).
func (w Wizard) VerifyError() string { return w.verifyErr }

// SaveError returns any error from persisting config or secret.
func (w Wizard) SaveError() string { return w.saveErr }

// Init satisfies tea.Model. The wizard does not need any startup commands.
func (w Wizard) Init() tea.Cmd { return nil }

// Update routes input to the current step.
func (w Wizard) Update(msg tea.Msg) (Wizard, tea.Cmd) {
	switch m := msg.(type) {
	case wizardVerifyDoneMsg:
		if w.step != WizardStepVerifying {
			return w, nil
		}
		if m.err != nil {
			w.verifyErr = m.err.Error()
			w.step = WizardStepToken
			return w, w.tokenInput.Focus()
		}
		w.user = m.user
		w.verifyErr = ""
		w.step = WizardStepProject
		w.projectInput.Focus()
		return w, w.fetchProjectsCmd()
	case wizardProjectsLoadedMsg:
		if w.step != WizardStepProject {
			return w, nil
		}
		w.projects = m.projects
		w.projectsErr = m.err
		return w, nil
	case tea.KeyMsg:
		return w.handleKey(m)
	}
	return w, nil
}

func (w Wizard) handleKey(k tea.KeyMsg) (Wizard, tea.Cmd) {
	switch w.step {
	case WizardStepURL:
		return w.handleURLKey(k)
	case WizardStepEmail:
		return w.handleEmailKey(k)
	case WizardStepToken:
		return w.handleTokenKey(k)
	case WizardStepVerifying:
		return w, nil
	case WizardStepProject:
		return w.handleProjectKey(k)
	}
	return w, nil
}

func (w Wizard) handleURLKey(k tea.KeyMsg) (Wizard, tea.Cmd) {
	if key.Matches(k, w.closeBinding) {
		return w, tea.Quit
	}
	if k.Type == tea.KeyEnter {
		v := strings.TrimSpace(w.urlInput.Value())
		if err := validateBaseURL(v); err != nil {
			w.verifyErr = err.Error()
			return w, nil
		}
		w.urlInput.SetValue(v)
		w.verifyErr = ""
		w.urlInput.Blur()
		w.step = WizardStepEmail
		return w, w.emailInput.Focus()
	}
	var cmd tea.Cmd
	w.urlInput, cmd = w.urlInput.Update(k)
	return w, cmd
}

func (w Wizard) handleEmailKey(k tea.KeyMsg) (Wizard, tea.Cmd) {
	if key.Matches(k, w.closeBinding) {
		w.emailInput.Blur()
		w.step = WizardStepURL
		return w, w.urlInput.Focus()
	}
	if k.Type == tea.KeyEnter {
		v := strings.TrimSpace(w.emailInput.Value())
		if err := validateEmail(v); err != nil {
			w.verifyErr = err.Error()
			return w, nil
		}
		w.emailInput.SetValue(v)
		w.verifyErr = ""
		w.emailInput.Blur()
		w.step = WizardStepToken
		return w, w.tokenInput.Focus()
	}
	var cmd tea.Cmd
	w.emailInput, cmd = w.emailInput.Update(k)
	return w, cmd
}

func (w Wizard) handleTokenKey(k tea.KeyMsg) (Wizard, tea.Cmd) {
	if key.Matches(k, w.closeBinding) {
		w.tokenInput.Blur()
		w.step = WizardStepEmail
		return w, w.emailInput.Focus()
	}
	if k.Type == tea.KeyEnter {
		v := strings.TrimSpace(w.tokenInput.Value())
		if v == "" {
			w.verifyErr = "API token is required"
			return w, nil
		}
		w.tokenInput.SetValue(v)
		w.tokenInput.Blur()
		w.verifyErr = ""
		w.step = WizardStepVerifying
		return w, w.verifyCmd()
	}
	var cmd tea.Cmd
	w.tokenInput, cmd = w.tokenInput.Update(k)
	return w, cmd
}

func (w Wizard) handleProjectKey(k tea.KeyMsg) (Wizard, tea.Cmd) {
	if key.Matches(k, w.closeBinding) {
		// Skip default project entirely.
		return w.save("")
	}
	switch k.Type {
	case tea.KeyEnter:
		matches := w.filteredProjects()
		if len(matches) == 0 {
			return w.save("")
		}
		idx := w.selectedProject
		if idx < 0 || idx >= len(matches) {
			idx = 0
		}
		return w.save(matches[idx].Key)
	case tea.KeyUp:
		if w.selectedProject > 0 {
			w.selectedProject--
		}
		return w, nil
	case tea.KeyDown:
		matches := w.filteredProjects()
		if w.selectedProject < len(matches)-1 {
			w.selectedProject++
		}
		return w, nil
	}
	var cmd tea.Cmd
	w.projectInput, cmd = w.projectInput.Update(k)
	w.selectedProject = 0
	return w, cmd
}

func (w Wizard) filteredProjects() []jira.Project {
	q := strings.ToLower(strings.TrimSpace(w.projectInput.Value()))
	if q == "" {
		return w.projects
	}
	out := make([]jira.Project, 0, len(w.projects))
	for _, p := range w.projects {
		if strings.Contains(strings.ToLower(p.Key), q) || strings.Contains(strings.ToLower(p.Name), q) {
			out = append(out, p)
		}
	}
	return out
}

func (w Wizard) verifyCmd() tea.Cmd {
	verifier := w.verifier
	baseURL := w.urlInput.Value()
	email := w.emailInput.Value()
	token := w.tokenInput.Value()
	return func() tea.Msg {
		if verifier == nil {
			return wizardVerifyDoneMsg{err: errors.New("no verifier configured")}
		}
		u, err := verifier.Verify(context.Background(), baseURL, email, token)
		return wizardVerifyDoneMsg{user: u, err: err}
	}
}

func (w Wizard) fetchProjectsCmd() tea.Cmd {
	verifier := w.verifier
	baseURL := w.urlInput.Value()
	email := w.emailInput.Value()
	token := w.tokenInput.Value()
	return func() tea.Msg {
		if verifier == nil {
			return wizardProjectsLoadedMsg{}
		}
		ps, err := verifier.ListProjects(context.Background(), baseURL, email, token)
		return wizardProjectsLoadedMsg{projects: ps, err: err}
	}
}

func (w Wizard) save(defaultProject string) (Wizard, tea.Cmd) {
	cfg := config.Defaults()
	if w.initial != nil {
		cfg = w.initial
	}
	cfg.BaseURL = strings.TrimSpace(w.urlInput.Value())
	cfg.Email = strings.TrimSpace(w.emailInput.Value())
	cfg.DefaultProject = defaultProject
	if cfg.DefaultGrouping == "" {
		cfg.DefaultGrouping = config.GroupingStatus
	}
	if cfg.Theme == "" {
		cfg.Theme = config.ThemeTokyoNight
	}
	if cfg.Icons == "" {
		cfg.Icons = config.IconsUnicode
	}

	if w.cfgPath == "" {
		w.saveErr = "no config path"
		return w, nil
	}
	if err := config.Save(w.cfgPath, cfg); err != nil {
		w.saveErr = err.Error()
		return w, nil
	}
	if w.store != nil {
		if err := w.store.Set(cfg.Email, strings.TrimSpace(w.tokenInput.Value())); err != nil {
			w.saveErr = err.Error()
			return w, nil
		}
	}

	w.saved = true
	w.final = cfg
	w.step = WizardStepDone
	final := cfg
	return w, func() tea.Msg { return WizardSavedMsg{Config: final} }
}

// View renders the current step. The wizard always returns a non-empty string
// because it owns the screen until it completes.
func (w Wizard) View(s styles.Styles) string {
	title := s.OverlayTitle.Render("ripjira — first run")
	var body []string
	switch w.step {
	case WizardStepURL:
		body = []string{
			s.SectionHeader.Render("Step 1 / 4 — Jira URL"),
			"",
			w.urlInput.View(),
		}
		if w.verifyErr != "" {
			body = append(body, "", s.Error.Render(w.verifyErr))
		}
		body = append(body, "", s.Muted.Render("enter continue    "+w.closeBinding.Help().Key+" quit"))
	case WizardStepEmail:
		body = []string{
			s.SectionHeader.Render("Step 2 / 4 — Email"),
			"",
			w.emailInput.View(),
		}
		if w.verifyErr != "" {
			body = append(body, "", s.Error.Render(w.verifyErr))
		}
		body = append(body, "", s.Muted.Render("enter continue    "+w.closeBinding.Help().Key+" back"))
	case WizardStepToken:
		body = []string{
			s.SectionHeader.Render("Step 3 / 4 — API token"),
			"",
			w.tokenInput.View(),
			"",
			s.Muted.Render("Get a token at https://id.atlassian.com/manage-profile/security/api-tokens"),
		}
		if w.verifyErr != "" {
			body = append(body, "", s.Error.Render(w.verifyErr))
		}
		body = append(body, "", s.Muted.Render("enter verify    "+w.closeBinding.Help().Key+" back"))
	case WizardStepVerifying:
		body = []string{
			s.SectionHeader.Render("Verifying credentials…"),
			"",
			s.Muted.Render("Calling Jira /myself"),
		}
	case WizardStepProject:
		header := "Step 4 / 4 — Default project (optional)"
		if w.user.DisplayName != "" {
			header = fmt.Sprintf("Connected as %s", w.user.DisplayName)
		}
		body = []string{
			s.SectionHeader.Render(header),
			"",
			w.projectInput.View(),
			"",
			w.renderProjectList(s),
		}
		if w.projectsErr != nil {
			body = append(body, "", s.Error.Render("Failed to load projects: "+w.projectsErr.Error()))
		}
		if w.saveErr != "" {
			body = append(body, "", s.Error.Render(w.saveErr))
		}
		body = append(body, "", s.Muted.Render("enter select    "+w.closeBinding.Help().Key+" skip"))
	case WizardStepDone:
		msg := "Saved."
		if w.final != nil {
			msg = fmt.Sprintf("Saved %s.", w.cfgPath)
		}
		body = []string{s.SectionHeader.Render(msg)}
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, append([]string{title, ""}, body...)...)
	return s.OverlayBorder.Render(inner)
}

func (w Wizard) renderProjectList(s styles.Styles) string {
	matches := w.filteredProjects()
	if len(matches) == 0 {
		return s.Muted.Render("(no projects)")
	}
	end := min(len(matches), 5)
	lines := make([]string, 0, end)
	for i := 0; i < end; i++ {
		row := fmt.Sprintf("%s  %s", matches[i].Key, matches[i].Name)
		if i == w.selectedProject {
			lines = append(lines, s.Accent.Render("> "+row))
		} else {
			lines = append(lines, s.Muted.Render("  "+row))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func validateBaseURL(s string) error {
	if s == "" {
		return errors.New("base URL is required")
	}
	tmp := &config.Config{
		BaseURL:         s,
		Email:           "x@x.x",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := tmp.Validate(); err != nil {
		// surface URL-specific error, ignore the rest.
		if strings.Contains(err.Error(), "base_url") {
			return err
		}
	}
	return nil
}

func validateEmail(s string) error {
	if s == "" {
		return errors.New("email is required")
	}
	tmp := &config.Config{
		BaseURL:         "https://x.x",
		Email:           s,
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := tmp.Validate(); err != nil {
		if strings.Contains(err.Error(), "email") {
			return err
		}
	}
	return nil
}
