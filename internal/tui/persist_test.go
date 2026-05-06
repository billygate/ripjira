package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/billygate/ripjira/internal/state"
)

// TestPersistAsync_EmitsToastErrorOnFailure confirms the helper's tea.Cmd
// resolves to a ToastError when state.Mutate fails. Forcing failure: use
// a path whose parent is a regular file so MkdirAll can't create the dir.
func TestPersistAsync_EmitsToastErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte{}, 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	// "blocker" is a file; "blocker/state.json" can't be created because
	// state.Mutate's MkdirAll(filepath.Dir(path)) fails on a non-dir parent.
	bad := filepath.Join(blocker, "state.json")

	cmd := persistAsync(bad, "test", func(s *state.State) { s.LastProject = "x" })
	if cmd == nil {
		t.Fatal("persistAsync returned nil cmd")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want ToastMsg", msg)
	}
	if toast.Level != ToastError {
		t.Fatalf("toast.Level = %v, want ToastError", toast.Level)
	}
	if toast.Text == "" {
		t.Fatal("toast.Text empty")
	}
}

// TestPersistAsync_NilCmdForEmptyPath confirms the helper short-circuits
// when path is "" so callers can chain unconditionally.
func TestPersistAsync_NilCmdForEmptyPath(t *testing.T) {
	if cmd := persistAsync("", "test", func(_ *state.State) {}); cmd != nil {
		t.Fatalf("persistAsync(\"\") = non-nil, want nil")
	}
}
