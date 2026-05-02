package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/billygate/ripjira/internal/config"
)

// stubWizard installs a runWizardFn replacement that records the SecretStore
// it was handed and returns the supplied error. Cleanup is registered with t.
func stubWizard(t *testing.T, retErr error) *config.SecretStore {
	t.Helper()
	var captured config.SecretStore
	prev := runWizardFn
	runWizardFn = func(s config.SecretStore, _, _ io.Writer) error {
		captured = s
		return retErr
	}
	t.Cleanup(func() { runWizardFn = prev })
	return &captured
}

// stubSecretStoreFn swaps secretStoreFn so the login subcommand sees the given
// store instead of the real keyring chain.
func stubSecretStoreFn(t *testing.T, s config.SecretStore) {
	t.Helper()
	prev := secretStoreFn
	secretStoreFn = func() config.SecretStore { return s }
	t.Cleanup(func() { secretStoreFn = prev })
}

func TestRun_LoginInvokesWizard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	fake := config.NewFakeStore()
	stubSecretStoreFn(t, fake)
	got := stubWizard(t, nil)

	var out, errw bytes.Buffer
	code := run([]string{"login"}, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}
	if *got == nil {
		t.Fatal("wizard was not invoked")
	}
	if *got != config.SecretStore(fake) {
		t.Errorf("wizard received unexpected store: %T", *got)
	}
}

func TestRun_LoginResetDeletesStoredToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	cfgPath := filepath.Join(dir, "ripjira", "config.yaml")
	cfg := &config.Config{
		BaseURL:         "https://example.invalid",
		Email:           "alice@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fake := config.NewFakeStore()
	if err := fake.Set("alice@example.com", "old-token"); err != nil {
		t.Fatalf("seed fake store: %v", err)
	}
	stubSecretStoreFn(t, fake)
	stubWizard(t, nil)

	var out, errw bytes.Buffer
	code := run([]string{"login", "--reset"}, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}

	if _, err := fake.Get("alice@example.com"); !errors.Is(err, config.ErrSecretNotFound) {
		t.Errorf("expected token deleted from store, got err=%v", err)
	}
}

func TestRun_LoginResetWithNoConfigSucceeds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	fake := config.NewFakeStore()
	stubSecretStoreFn(t, fake)
	stubWizard(t, nil)

	var out, errw bytes.Buffer
	code := run([]string{"login", "--reset"}, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}
}

func TestRun_LoginResetMissingTokenSucceeds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	cfgPath := filepath.Join(dir, "ripjira", "config.yaml")
	cfg := &config.Config{
		BaseURL:         "https://example.invalid",
		Email:           "bob@example.com",
		DefaultGrouping: config.GroupingStatus,
		Theme:           config.ThemeTokyoNight,
		Icons:           config.IconsUnicode,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fake := config.NewFakeStore() // no token stored
	stubSecretStoreFn(t, fake)
	stubWizard(t, nil)

	var out, errw bytes.Buffer
	code := run([]string{"login", "--reset"}, &out, &errw)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errw.String())
	}
}

func TestRun_LoginUnknownFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	fake := config.NewFakeStore()
	stubSecretStoreFn(t, fake)

	wizardCalled := false
	prev := runWizardFn
	runWizardFn = func(config.SecretStore, io.Writer, io.Writer) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizardFn = prev })

	var out, errw bytes.Buffer
	code := run([]string{"login", "--bogus"}, &out, &errw)
	if code != 2 {
		t.Fatalf("expected exit 2 for unknown flag, got %d (stderr=%q)", code, errw.String())
	}
	if !strings.Contains(errw.String(), "unknown flag") {
		t.Errorf("expected 'unknown flag' in stderr, got %q", errw.String())
	}
	if wizardCalled {
		t.Error("wizard must not run when flags are invalid")
	}
}

func TestRun_LoginWizardErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	stubSecretStoreFn(t, config.NewFakeStore())
	stubWizard(t, errIO)

	var out, errw bytes.Buffer
	code := run([]string{"login"}, &out, &errw)
	if code == 0 {
		t.Fatal("expected non-zero exit when wizard fails")
	}
	if !strings.Contains(errw.String(), "boom") {
		t.Errorf("expected wizard error in stderr, got %q", errw.String())
	}
}
