package structure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_LoadEmptyDirReturnsBuiltinsOnly(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	got, err := s.Load("BIL")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 builtins, got %d", len(got))
	}
	if got[0].ID != BuiltinDefaultID || got[1].ID != BuiltinInboxID {
		t.Fatalf("unexpected ids: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestStore_LoadParsesUserYAML(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `
- id: my-team
  name: My team
  sections:
    - title: In progress
      filter:
        status: [Open, "In Progress"]
        assignee:
          exists: true
- id: synced
  name: Pilot synced
  source: pilot
  sections:
    - title: Foo
      filter:
        status: [Open]
`
	if err := os.WriteFile(filepath.Join(dir, "BIL.yml"), []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	got, err := s.Load("BIL")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 structures, got %d: %#v", len(got), got)
	}
	if got[2].ID != "my-team" || got[2].IsReadOnly() {
		t.Errorf("user structure: %#v", got[2])
	}
	if !got[3].IsReadOnly() {
		t.Errorf("synced should be read-only")
	}
}

func TestStore_LoadRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BIL.yml"), []byte(`- id: bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if _, err := s.Load("BIL"); err == nil {
		t.Fatal("expected validation error for nameless structure")
	}
}

func TestDefaultDir_XDGOverridesHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg-xyz")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("default dir: %v", err)
	}
	if got != filepath.Join("/tmp/cfg-xyz", "ripjira", "structures") {
		t.Fatalf("got %q", got)
	}
}

func TestStore_FindByID(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	got, err := s.FindByID("BIL", BuiltinDefaultID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != BuiltinDefaultID {
		t.Fatalf("got %q", got.ID)
	}
	if _, err := s.FindByID("BIL", "nope"); err == nil {
		t.Fatal("expected not found")
	}
}
