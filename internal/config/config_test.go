package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoad_Valid(t *testing.T) {
	cfg, err := Load("testdata/valid.yaml")
	if err != nil {
		// On a fresh checkout the testdata file may have group-readable mode;
		// the sentinel is not a fatal load error.
		if !errors.Is(err, ErrPermissionsTooWide) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if cfg.BaseURL != "https://acme.atlassian.net" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Email != "dev@example.com" {
		t.Errorf("Email = %q", cfg.Email)
	}
	if cfg.DefaultProject != "PROJ" {
		t.Errorf("DefaultProject = %q", cfg.DefaultProject)
	}
	if cfg.DefaultGrouping != "priority" {
		t.Errorf("DefaultGrouping = %q", cfg.DefaultGrouping)
	}
	if cfg.AutoRefreshSeconds != 60 {
		t.Errorf("AutoRefreshSeconds = %d", cfg.AutoRefreshSeconds)
	}
	if cfg.Theme != "catppuccin" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
	if cfg.Icons != "ascii" {
		t.Errorf("Icons = %q", cfg.Icons)
	}
}

func TestLoad_DefaultsAppliedForMinimal(t *testing.T) {
	cfg, err := Load("testdata/minimal.yaml")
	if err != nil && !errors.Is(err, ErrPermissionsTooWide) {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultGrouping != GroupingStatus {
		t.Errorf("DefaultGrouping default = %q, want %q", cfg.DefaultGrouping, GroupingStatus)
	}
	if cfg.Theme != ThemeTokyoNight {
		t.Errorf("Theme default = %q, want %q", cfg.Theme, ThemeTokyoNight)
	}
	if cfg.Icons != IconsUnicode {
		t.Errorf("Icons default = %q, want %q", cfg.Icons, IconsUnicode)
	}
	if cfg.AutoRefreshSeconds != 60 {
		t.Errorf("AutoRefreshSeconds default = %d, want 60", cfg.AutoRefreshSeconds)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"testdata/missing_url.yaml", "base_url"},
		{"testdata/missing_email.yaml", "email"},
		{"testdata/bad_url.yaml", "base_url"},
		{"testdata/bad_email.yaml", "email"},
		{"testdata/bad_grouping.yaml", "default_grouping"},
		{"testdata/bad_theme.yaml", "theme"},
		{"testdata/bad_icons.yaml", "icons"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			_, err := Load(tc.path)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLoad_FileMissing(t *testing.T) {
	_, err := Load("testdata/does_not_exist.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestLoad_PermissionsTooWide(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("base_url: https://x.atlassian.net\nemail: a@b.co\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if !errors.Is(err, ErrPermissionsTooWide) {
		t.Fatalf("err = %v, want ErrPermissionsTooWide", err)
	}
	if cfg == nil || cfg.BaseURL == "" {
		t.Fatal("expected config to still be returned alongside sentinel")
	}
}

func TestLoad_PermissionsExact0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("base_url: https://x.atlassian.net\nemail: a@b.co\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSave_RoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	cfg := &Config{
		BaseURL:            "https://acme.atlassian.net",
		Email:              "dev@example.com",
		DefaultProject:     "PROJ",
		DefaultGrouping:    GroupingStatus,
		AutoRefreshSeconds: 30,
		Theme:              ThemeTokyoNight,
		Icons:              IconsUnicode,
		EpicIssueTypes:     []string{"Epic", "Epic Feature"},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("round-trip differs:\n got %#v\nwant %#v", got, cfg)
	}
}

func TestSave_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &Config{}
	if err := Save(path, cfg); err == nil {
		t.Fatal("expected validation error from Save")
	}
}

func TestValidate_NegativeAutoRefresh(t *testing.T) {
	cfg := Defaults()
	cfg.BaseURL = "https://x.atlassian.net"
	cfg.Email = "a@b.co"
	cfg.AutoRefreshSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative auto_refresh_seconds")
	}
}

func TestConfig_EpicIssueTypesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("base_url: https://x.atlassian.net\nemail: a@b.c\ndefault_grouping: status\ntheme: nord\nicons: ascii\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"Epic", "Epic Feature"}
	if !reflect.DeepEqual(cfg.EpicIssueTypes, want) {
		t.Fatalf("EpicIssueTypes = %#v, want %#v", cfg.EpicIssueTypes, want)
	}
}

func TestConfig_EpicIssueTypesOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("base_url: https://x.atlassian.net\nemail: a@b.c\ndefault_grouping: status\ntheme: nord\nicons: ascii\nepic_issue_types: [Theme, Initiative]\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"Theme", "Initiative"}
	if !reflect.DeepEqual(cfg.EpicIssueTypes, want) {
		t.Fatalf("EpicIssueTypes = %#v, want %#v", cfg.EpicIssueTypes, want)
	}
}

func TestConfig_EpicGroupingValid(t *testing.T) {
	cfg := Defaults()
	cfg.BaseURL = "https://x.atlassian.net"
	cfg.Email = "a@b.c"
	cfg.DefaultGrouping = "epic"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("epic should be a valid grouping: %v", err)
	}
	cfg.DefaultGrouping = "parent"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("parent should be a valid grouping: %v", err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/xdg/ripjira/config.yaml"
	if got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}
