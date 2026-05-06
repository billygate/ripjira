package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// TestModel_LoadsCommentDraftsAtStartup asserts drafts written before
// New(...) was called are visible via loadDraft without re-reading disk.
// We corrupt the file mid-test to prove loadDraft uses the cache.
func TestModel_LoadsCommentDraftsAtStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := state.Mutate(path, func(s *state.State) {
		if s.CommentDrafts == nil {
			s.CommentDrafts = map[string]string{}
		}
		s.CommentDrafts["PROJ-1"] = "in-progress comment"
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("palette: %v", err)
	}
	m := New(p, WithStatePath(path))
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}
	if got := m.loadDraft("PROJ-1"); got != "in-progress comment" {
		t.Fatalf("loadDraft = %q, want %q", got, "in-progress comment")
	}
}

// TestSaveDraft_UpdatesCacheImmediately asserts saveDraft writes are
// visible to the next loadDraft synchronously, before the background
// state.Mutate goroutine flushes.
func TestSaveDraft_UpdatesCacheImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("palette: %v", err)
	}
	m := New(p, WithStatePath(path))
	m.saveDraft("PROJ-7", "fresh body")
	if got := m.loadDraft("PROJ-7"); got != "fresh body" {
		t.Fatalf("loadDraft after saveDraft = %q, want %q", got, "fresh body")
	}
}

// TestModel_LoadsFavoritesAtStartup asserts favorites written before
// New(...) was called are visible via loadFavoriteEntries without
// re-reading disk.
func TestModel_LoadsFavoritesAtStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := state.Mutate(path, func(s *state.State) {
		s.Favorites = append(s.Favorites, state.Favorite{Name: "mine", JQL: "assignee = currentUser()"})
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("palette: %v", err)
	}
	m := New(p, WithStatePath(path))
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}
	got := m.loadFavoriteEntries()
	if len(got) != 1 || got[0].Name != "mine" || got[0].JQL != "assignee = currentUser()" {
		t.Fatalf("loadFavoriteEntries = %+v, want one entry", got)
	}
}
