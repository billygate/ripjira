package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/billygate/ripjira/internal/state"
)

func TestDefaultPathHonoursXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	got, err := state.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(dir, "ripjira", "state.json")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")

	got, err := state.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "ripjira", "state.json")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such.json")
	s, err := state.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LastProject != "" || s.Grouping != "" || s.Sort != "" || s.SortDesc != nil || len(s.Favorites) != 0 {
		t.Fatalf("Load missing = %+v, want zero", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := state.State{LastProject: "RIP"}
	if err := state.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := state.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastProject != want.LastProject {
		t.Fatalf("round-trip = %+v, want %+v", got, want)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "state.json")
	if err := state.Save(path, state.State{LastProject: "X"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat after Save: %v", err)
	}
}

func TestSaveSetsMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := state.Save(path, state.State{LastProject: "X"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 0600", perm)
	}
}

// Atomic: tested implicitly via temp+rename.

func TestLoadMalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := state.Load(path)
	if err == nil {
		t.Fatalf("Load malformed: want error, got nil")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "json") {
		t.Logf("error message: %v", err)
	}
}

func TestState_RoundTripGroupingSort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	desc := false
	in := state.State{
		LastProject: "ENG",
		Grouping:    "priority",
		Sort:        "updated",
		SortDesc:    &desc,
	}
	if err := state.Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := state.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Grouping != "priority" || out.Sort != "updated" {
		t.Fatalf("round trip: got grouping=%q sort=%q", out.Grouping, out.Sort)
	}
	if out.SortDesc == nil || *out.SortDesc != false {
		t.Fatalf("SortDesc should round-trip as *false, got %v", out.SortDesc)
	}
}

func TestState_OmitemptyKeepsLegacyShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := state.Save(path, state.State{LastProject: "ENG"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "grouping") || strings.Contains(string(raw), "sort") {
		t.Fatalf("legacy state should not include new fields when unset; got %s", string(raw))
	}
}

func TestState_LastStructureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := state.Mutate(path, func(s *state.State) {
		if s.LastStructure == nil {
			s.LastStructure = map[string]string{}
		}
		s.LastStructure["BIL"] = "my-team"
		s.LastStructure["OPS"] = "default"
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	got, err := state.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.LastStructure["BIL"] != "my-team" || got.LastStructure["OPS"] != "default" {
		t.Fatalf("round-trip lost data: %#v", got.LastStructure)
	}
}

func TestState_EditorAdviceShownRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := state.Mutate(path, func(s *state.State) {
		s.EditorAdviceShown = true
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.EditorAdviceShown {
		t.Fatalf("EditorAdviceShown not persisted")
	}
}
