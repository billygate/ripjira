// Package main is the ripjira TUI entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui"
	"github.com/billygate/ripjira/internal/tui/gfx"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

const version = "ripjira dev"

const usage = `ripjira — Jira Cloud TUI client

Usage:
  ripjira [flags]
  ripjira login [--reset]

Flags:
  --version, -v   print version and exit
  --help, -h      show this help and exit

Subcommands:
  login           re-run the first-run wizard, preserving existing values
  login --reset   delete the stored API token before re-running the wizard

Environment:
  RIPJIRA_TOKEN              fallback API token if no keyring entry exists
  RIPJIRA_DEBUG=1            write request/response log to ~/.cache/ripjira/debug.log
  RIPJIRA_BASE_URL_OVERRIDE  override config base_url (used by tests)
`

// envBaseURLOverride lets tests point the client at an httptest.Server without
// touching the user's config.
const envBaseURLOverride = "RIPJIRA_BASE_URL_OVERRIDE"

// secretStoreFn lets tests inject an in-memory SecretStore. The default chains
// the OS keyring with a RIPJIRA_TOKEN env-var fallback.
var secretStoreFn = func() config.SecretStore {
	return config.NewEnvFallbackStore(config.NewKeyringStore())
}

// runTUIFn is the indirection that lets tests replace the actual Bubble Tea
// program run with a stub. The default implementation builds the model and
// calls (*tea.Program).Run; tests substitute a hook that returns
// immediately after observing the loader.
var runTUIFn = runTUI

// runWizardFn is swapped in tests to avoid spawning a real Bubble Tea program
// for the first-run wizard. The default implementation runs the overlay as
// the only widget in a fullscreen tea.Program.
var runWizardFn = runWizard

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errw io.Writer) int {
	jira.Version = strings.TrimPrefix(version, "ripjira ")
	// Only consume global flags that appear before the first non-flag argument.
	// Otherwise `ripjira login --version` would print the version instead of
	// being routed to the login subcommand's parser.
	for _, a := range args {
		if len(a) == 0 || a[0] != '-' {
			break
		}
		switch a {
		case "--version", "-v":
			_, _ = fmt.Fprintln(out, version)
			return 0
		case "--help", "-h":
			_, _ = fmt.Fprint(out, usage)
			return 0
		}
	}

	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		switch args[0] {
		case "login":
			return runLogin(args[1:], secretStoreFn(), out, errw)
		default:
			_, _ = fmt.Fprintf(errw, "ripjira: unknown command %q\n", args[0])
			_, _ = fmt.Fprint(errw, usage)
			return 2
		}
	}

	if needsWizard() {
		if werr := runWizardFn(secretStoreFn(), out, errw); werr != nil {
			_, _ = fmt.Fprintf(errw, "ripjira: %v\n", werr)
			return 1
		}
	}

	cfg, client, err := buildClient()
	if err != nil {
		_, _ = fmt.Fprintf(errw, "ripjira: %v\n", err)
		return 1
	}

	if err := runWithRestart(cfg, client, out, errw); err != nil {
		_, _ = fmt.Fprintf(errw, "ripjira: %v\n", err)
		return 1
	}
	return 0
}

// maxRestarts caps how many times we'll relaunch the TUI after a panic
// before giving up — prevents a tight crash loop if the panic is
// deterministic.
const maxRestarts = 5

// runWithRestart wraps runTUIFn in a recover loop. A panic in Update/View
// (or anywhere else on the main goroutine) is caught, logged to the crash
// log, and the TUI is restarted from a fresh state. Setting
// RIPJIRA_NO_RESTART=1 disables the supervisor — useful in development so
// panics produce normal Go traces.
func runWithRestart(cfg *config.Config, client *jira.Client, out, errw io.Writer) error {
	if os.Getenv("RIPJIRA_NO_RESTART") == "1" {
		return runTUIFn(cfg, client, out, errw)
	}
	var lastErr error
	for attempt := 1; attempt <= maxRestarts+1; attempt++ {
		panicked, err := runTUIOnce(cfg, client, out, errw)
		lastErr = err
		if !panicked {
			return err
		}
		if attempt > maxRestarts {
			return fmt.Errorf("crashed %d times in a row, giving up", attempt-1)
		}
		_, _ = fmt.Fprintf(errw, "ripjira: crashed; restarting (%d/%d)\n", attempt, maxRestarts)
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

func runTUIOnce(cfg *config.Config, client *jira.Client, out, errw io.Writer) (panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			logCrash(r, debug.Stack(), errw)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	err = runTUIFn(cfg, client, out, errw)
	return false, err
}

// logCrash appends the panic value and stack trace to the crash log under
// $XDG_STATE_HOME/ripjira/crashes.log. Failures to open the log are silent
// — we'd rather restart than block on disk errors. The trace also goes to
// stderr so the user sees something even when the log is unreachable.
func logCrash(r any, stack []byte, errw io.Writer) {
	now := time.Now().Format(time.RFC3339)
	header := fmt.Sprintf("\n=== %s panic: %v\n", now, r)
	_, _ = fmt.Fprint(errw, header)
	_, _ = errw.Write(stack)

	statePath, err := state.DefaultPath()
	if err != nil {
		return
	}
	logPath := filepath.Join(filepath.Dir(statePath), "crashes.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(header)
	_, _ = f.Write(stack)
}

// runLogin handles `ripjira login [--reset]`. The wizard already seeds its
// inputs from the existing config file, so this command simply re-launches it.
// With --reset, the stored API token for the configured email is deleted up
// front so the user is forced to enter a new one.
func runLogin(args []string, store config.SecretStore, out, errw io.Writer) int {
	reset := false
	for _, a := range args {
		switch a {
		case "--reset":
			reset = true
		case "--help", "-h":
			_, _ = fmt.Fprint(out, usage)
			return 0
		default:
			_, _ = fmt.Fprintf(errw, "ripjira login: unknown flag %q\n", a)
			return 2
		}
	}

	if reset {
		if err := deleteStoredToken(store); err != nil {
			_, _ = fmt.Fprintf(errw, "ripjira: %v\n", err)
			return 1
		}
	}

	if err := runWizardFn(store, out, errw); err != nil {
		_, _ = fmt.Fprintf(errw, "ripjira: %v\n", err)
		return 1
	}
	return 0
}

// deleteStoredToken removes the keyring entry for the email recorded in the
// existing config. Missing config or missing secret are not errors — the goal
// is to leave no token behind, which a missing one already achieves.
func deleteStoredToken(store config.SecretStore) error {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil && !errors.Is(err, config.ErrPermissionsTooWide) {
		// No config or unreadable config — nothing to delete by email.
		return nil
	}
	if cfg == nil || cfg.Email == "" {
		return nil
	}
	if derr := store.Delete(cfg.Email); derr != nil && !errors.Is(derr, config.ErrSecretNotFound) {
		return fmt.Errorf("delete stored token: %w", derr)
	}
	return nil
}

// buildClient resolves the config + token and constructs a Jira HTTP client.
// Splitting it out of run keeps the entry point readable and lets the TUI
// hook accept ready-to-use values.
func buildClient() (*config.Config, *jira.Client, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil && !errors.Is(err, config.ErrPermissionsTooWide) {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	token, err := secretStoreFn().Get(cfg.Email)
	if err != nil {
		return nil, nil, fmt.Errorf("get API token (set %s or run `ripjira login`): %w", config.EnvTokenVar, err)
	}

	baseURL := cfg.BaseURL
	if v := os.Getenv(envBaseURLOverride); v != "" {
		baseURL = v
	}

	client, err := jira.NewClient(baseURL, cfg.Email, token)
	if err != nil {
		return nil, nil, fmt.Errorf("build jira client: %w", err)
	}
	if len(cfg.CustomFields) > 0 {
		ids := make([]string, 0, len(cfg.CustomFields))
		for _, id := range cfg.CustomFields {
			ids = append(ids, id)
		}
		client.SetExtraFields(ids)
	}
	return cfg, client, nil
}

// needsWizard reports whether the first-run wizard should be invoked. The
// wizard runs when the config file is absent or when its required fields fail
// validation; permissions-too-wide is treated as a warning, not a trigger.
func needsWizard() bool {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return false
	}
	if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	_, err = config.Load(cfgPath)
	if err == nil || errors.Is(err, config.ErrPermissionsTooWide) {
		return false
	}
	return true
}

// wizardClient adapts jira.NewClient + Myself/Projects to the WizardVerifier
// interface so the wizard can probe the user's credentials before saving.
type wizardClient struct{}

func (wizardClient) Verify(ctx context.Context, baseURL, email, token string) (jira.User, error) {
	cli, err := jira.NewClient(baseURL, email, token)
	if err != nil {
		return jira.User{}, err
	}
	return cli.Myself(ctx)
}

func (wizardClient) ListProjects(ctx context.Context, baseURL, email, token string) ([]jira.Project, error) {
	cli, err := jira.NewClient(baseURL, email, token)
	if err != nil {
		return nil, err
	}
	return cli.Projects(ctx)
}

// wizardModel wraps overlays.Wizard so it satisfies tea.Model and quits the
// program when the user finishes or aborts.
type wizardModel struct {
	w        overlays.Wizard
	styles   styles.Styles
	finished bool
}

// Init satisfies tea.Model.
func (m wizardModel) Init() tea.Cmd { return m.w.Init() }

// Update routes messages to the inner wizard, exiting on success.
func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(overlays.WizardSavedMsg); ok {
		m.finished = true
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.w, cmd = m.w.Update(msg)
	if m.w.Done() {
		m.finished = true
	}
	return m, cmd
}

// View renders the wizard inside the App style envelope so the background
// color matches the rest of ripjira.
func (m wizardModel) View() string {
	return m.styles.App.Render(m.w.View(m.styles))
}

// runWizard launches the first-run wizard as a standalone Bubble Tea program.
// On success it has already persisted config + secret to disk; on user
// cancellation it returns an error so the caller can exit instead of
// proceeding into a broken TUI session.
func runWizard(store config.SecretStore, _, _ io.Writer) error {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	palette, err := themes.ByName(config.ThemeTokyoNight)
	if err != nil {
		return fmt.Errorf("theme: %w", err)
	}

	// Best-effort: seed the wizard with whatever was in the (broken) config
	// file so the user does not have to re-type fields that were already valid.
	initial, _ := config.Load(cfgPath)

	w := overlays.NewWizard(overlays.WizardOptions{
		Verifier: wizardClient{},
		Store:    store,
		CfgPath:  cfgPath,
		Initial:  initial,
		CloseKey: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	})

	prog := tea.NewProgram(wizardModel{w: w, styles: styles.New(palette)}, tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return err
	}
	pm, ok := final.(wizardModel)
	if !ok || !pm.finished {
		return errors.New("wizard cancelled")
	}
	return nil
}

// runTUI is the default TUI launcher. It resolves the palette, looks up the
// account ID for cache scoping, pre-loads any cached issues so the first
// frame paints them, and then hands control to bubbletea.
func runTUI(cfg *config.Config, client *jira.Client, _, _ io.Writer) error {
	// Probe the terminal for inline-graphics capability before bubbletea
	// takes over the TTY. This caches the protocol so any later call from
	// inside the Update loop is a pure map read.
	gfx.Warm()

	palette, err := themes.ByName(cfg.Theme)
	if err != nil {
		return fmt.Errorf("theme: %w", err)
	}

	cachePath, _ := tui.DefaultCachePath()

	cfgPath, _ := config.DefaultPath()
	opts := []tui.Option{
		tui.WithLoader(tui.NewCachingLoader(tui.NewClientLoader(client))),
		tui.WithCachePath(cachePath),
		tui.WithDefaultProject(cfg.DefaultProject),
		tui.WithDefaultPriority(cfg.DefaultPriority),
		tui.WithEpicTypes(cfg.EpicIssueTypes),
		tui.WithCustomFields(cfg.CustomFields),
		tui.WithConfig(*cfg),
		tui.WithConfigPath(cfgPath),
	}
	if statePath, err := state.DefaultPath(); err == nil {
		opts = append(opts, tui.WithStatePath(statePath))
	}
	if cfg.AutoRefreshSeconds > 0 {
		opts = append(opts, tui.WithAutoRefresh(time.Duration(cfg.AutoRefreshSeconds)*time.Second))
	}
	if dir, err := structure.DefaultDir(); err == nil {
		structCtx, structCancel := context.WithCancel(context.Background())
		defer structCancel()
		opts = append(opts, tui.WithStructures(structCtx, structure.NewStore(dir)))
	}
	model := tui.New(palette, opts...)

	prog := tea.NewProgram(model, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}
