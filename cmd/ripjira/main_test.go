package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
)

func TestVersionConstant(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty")
	}
}

func TestRun_VersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		t.Run(flag, func(t *testing.T) {
			var out, errw bytes.Buffer
			code := run([]string{flag}, &out, &errw)
			if code != 0 {
				t.Fatalf("exit %d, stderr=%q", code, errw.String())
			}
			if !strings.Contains(out.String(), version) {
				t.Fatalf("expected output to contain %q, got %q", version, out.String())
			}
		})
	}
}

func TestRun_HelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			var out, errw bytes.Buffer
			code := run([]string{flag}, &out, &errw)
			if code != 0 {
				t.Fatalf("exit %d, stderr=%q", code, errw.String())
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("expected help to contain 'Usage:', got %q", out.String())
			}
		})
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	var out, errw bytes.Buffer
	code := run([]string{"frobnicate"}, &out, &errw)
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown subcommand")
	}
	if !strings.Contains(errw.String(), "unknown command") {
		t.Fatalf("expected 'unknown command' in stderr, got %q", errw.String())
	}
}

// TestRun_TUILaunchWiresClient stubs runTUIFn so the test can assert that
// the default `ripjira` invocation builds a working client (config loaded,
// token resolved, base URL overridden) and hands it to the TUI launcher.
// We verify the wiring by having the stub call MyIssues against a stub
// server and checking the returned issues.
func TestRun_TUILaunchWiresClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Errorf("missing Basic auth header, got %q", got)
		}
		if !strings.HasPrefix(r.URL.Path, "/rest/api/3/search/jql") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issues": []map[string]any{
				{
					"key": "PROJ-1",
					"fields": map[string]any{
						"summary": "First issue",
						"status":  map[string]any{"id": "1", "name": "In Progress"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "ripjira", "config.yaml")
	cfg := &config.Config{
		BaseURL:         "https://example.invalid",
		Email:           "tester@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	t.Setenv(envBaseURLOverride, srv.URL)
	t.Setenv(config.EnvTokenVar, "test-token")

	prevStoreFn := secretStoreFn
	secretStoreFn = func() config.SecretStore {
		return config.NewEnvFallbackStore(config.NewFakeStore())
	}
	t.Cleanup(func() { secretStoreFn = prevStoreFn })

	var gotCfg *config.Config
	var gotIssues []jira.Issue
	prevTUI := runTUIFn
	runTUIFn = func(c *config.Config, cli *jira.Client, _, _ io.Writer) error {
		gotCfg = c
		issues, err := cli.MyIssues(context.Background())
		if err != nil {
			return err
		}
		gotIssues = issues
		return nil
	}
	t.Cleanup(func() { runTUIFn = prevTUI })

	var out, errw bytes.Buffer
	code := run(nil, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}
	if gotCfg == nil || gotCfg.Email != "tester@example.com" {
		t.Fatalf("runTUIFn received cfg=%v", gotCfg)
	}
	if len(gotIssues) != 1 || gotIssues[0].Key != "PROJ-1" {
		t.Fatalf("MyIssues via TUI client returned %v", gotIssues)
	}
}

func TestRun_TUILaunchPropagatesError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "ripjira", "config.yaml")
	cfg := &config.Config{
		BaseURL:         "https://example.invalid",
		Email:           "tester@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv(config.EnvTokenVar, "test-token")
	prevStoreFn := secretStoreFn
	secretStoreFn = func() config.SecretStore {
		return config.NewEnvFallbackStore(config.NewFakeStore())
	}
	t.Cleanup(func() { secretStoreFn = prevStoreFn })

	prevTUI := runTUIFn
	runTUIFn = func(*config.Config, *jira.Client, io.Writer, io.Writer) error {
		return errIO
	}
	t.Cleanup(func() { runTUIFn = prevTUI })

	var out, errw bytes.Buffer
	code := run(nil, &out, &errw)
	if code == 0 {
		t.Fatal("expected non-zero exit when TUI returns error")
	}
	if !strings.Contains(errw.String(), "boom") {
		t.Errorf("stderr missing TUI error text: %q", errw.String())
	}
}

var errIO = stubErr{msg: "boom"}

type stubErr struct{ msg string }

func (s stubErr) Error() string { return s.msg }

func TestRun_MissingConfigInvokesWizard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	_ = os.Remove(filepath.Join(dir, "ripjira", "config.yaml"))

	prevWizard := runWizardFn
	wizardCalled := false
	runWizardFn = func(_ config.SecretStore, _, _ io.Writer) error {
		wizardCalled = true
		return errIO
	}
	t.Cleanup(func() { runWizardFn = prevWizard })

	var out, errw bytes.Buffer
	code := run(nil, &out, &errw)
	if code == 0 {
		t.Fatalf("expected non-zero exit when wizard fails; stderr=%q", errw.String())
	}
	if !wizardCalled {
		t.Error("missing config should invoke runWizardFn")
	}
	if !strings.Contains(errw.String(), "boom") {
		t.Errorf("stderr missing wizard error text: %q", errw.String())
	}
}

func TestRun_SkipsWizardWhenConfigValid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "ripjira", "config.yaml")
	cfg := &config.Config{
		BaseURL:         "https://example.invalid",
		Email:           "tester@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv(config.EnvTokenVar, "test-token")
	prevStoreFn := secretStoreFn
	secretStoreFn = func() config.SecretStore {
		return config.NewEnvFallbackStore(config.NewFakeStore())
	}
	t.Cleanup(func() { secretStoreFn = prevStoreFn })

	prevWizard := runWizardFn
	runWizardFn = func(_ config.SecretStore, _, _ io.Writer) error {
		t.Error("wizard should not be invoked when config is valid")
		return nil
	}
	t.Cleanup(func() { runWizardFn = prevWizard })

	prevTUI := runTUIFn
	runTUIFn = func(*config.Config, *jira.Client, io.Writer, io.Writer) error {
		return nil
	}
	t.Cleanup(func() { runTUIFn = prevTUI })

	var out, errw bytes.Buffer
	code := run(nil, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}
}

func TestRunWithRestart_RecoversFromPanic(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("RIPJIRA_NO_RESTART", "")

	prev := runTUIFn
	t.Cleanup(func() { runTUIFn = prev })

	var calls int
	runTUIFn = func(*config.Config, *jira.Client, io.Writer, io.Writer) error {
		calls++
		if calls == 1 {
			panic("boom")
		}
		return nil
	}

	var errw bytes.Buffer
	if err := runWithRestart(&config.Config{}, nil, io.Discard, &errw); err != nil {
		t.Fatalf("runWithRestart returned %v", err)
	}
	if calls != 2 {
		t.Fatalf("runTUIFn called %d times, want 2", calls)
	}
	if !strings.Contains(errw.String(), "panic: boom") {
		t.Fatalf("stderr missing panic line: %q", errw.String())
	}
}

func TestRunWithRestart_GivesUpAfterMaxRestarts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("RIPJIRA_NO_RESTART", "")

	prev := runTUIFn
	t.Cleanup(func() { runTUIFn = prev })

	var calls int
	runTUIFn = func(*config.Config, *jira.Client, io.Writer, io.Writer) error {
		calls++
		panic("always boom")
	}

	err := runWithRestart(&config.Config{}, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error after exceeding max restarts")
	}
	if calls != maxRestarts+1 {
		t.Fatalf("runTUIFn called %d times, want %d", calls, maxRestarts+1)
	}
}

func TestRunWithRestart_DisabledByEnv(t *testing.T) {
	t.Setenv("RIPJIRA_NO_RESTART", "1")

	prev := runTUIFn
	t.Cleanup(func() { runTUIFn = prev })

	var calls int
	runTUIFn = func(*config.Config, *jira.Client, io.Writer, io.Writer) error {
		calls++
		return nil
	}

	if err := runWithRestart(&config.Config{}, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("err: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
