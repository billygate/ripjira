package editor

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeExitErr returns an *exec.ExitError-shaped error with the given code by
// running a real `sh -c "exit N"`. We can't construct exec.ExitError directly.
func fakeExitErr(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit "+itoa(code))
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected error from exit %d", code)
	}
	return err
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func runCmd(t *testing.T, c tea.Cmd) tea.Msg {
	t.Helper()
	if c == nil {
		t.Fatal("nil cmd")
	}
	return c()
}

func TestOpen_SuccessAppliesParse(t *testing.T) {
	prevRun := runEditor
	t.Cleanup(func() { runEditor = prevRun })

	runEditor = func(path string) error {
		return os.WriteFile(path, []byte("# New summary\n\nNew body line\n"), 0o600)
	}

	cmd := Open(OpenSpec{Summary: "Old", Body: "Old body", Title: "ABC-1", Token: 7})
	msg := runCmd(t, cmd)

	got, ok := msg.(ClosedMsg)
	if !ok {
		t.Fatalf("got %T, want ClosedMsg", msg)
	}
	if got.Cancelled {
		t.Fatalf("unexpected cancel")
	}
	if got.Err != nil {
		t.Fatalf("unexpected error: %v", got.Err)
	}
	if got.Token != 7 {
		t.Fatalf("token: got %d want 7", got.Token)
	}
	if got.Summary != "New summary" || got.Body != "New body line" {
		t.Fatalf("parse: got %q / %q", got.Summary, got.Body)
	}
}

func TestOpen_NonZeroExitIsCancelled(t *testing.T) {
	prevRun := runEditor
	t.Cleanup(func() { runEditor = prevRun })

	runEditor = func(_ string) error {
		return fakeExitErr(t, 1)
	}

	cmd := Open(OpenSpec{Token: 1})
	msg := runCmd(t, cmd).(ClosedMsg)
	if !msg.Cancelled {
		t.Fatalf("expected cancelled")
	}
	if msg.Err != nil {
		t.Fatalf("expected nil err on cancel, got %v", msg.Err)
	}
}

func TestOpen_GenericErrorPropagates(t *testing.T) {
	prevRun := runEditor
	t.Cleanup(func() { runEditor = prevRun })

	want := errors.New("spawn boom")
	runEditor = func(_ string) error { return want }

	cmd := Open(OpenSpec{Token: 2})
	msg := runCmd(t, cmd).(ClosedMsg)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "spawn boom") {
		t.Fatalf("err: got %v", msg.Err)
	}
	if msg.Cancelled {
		t.Fatalf("did not expect cancelled when generic error")
	}
}

func TestOpen_NoEditorResolvedReturnsError(t *testing.T) {
	prevResolve := resolveEditor
	t.Cleanup(func() { resolveEditor = prevResolve })
	resolveEditor = func() string { return "" }

	cmd := Open(OpenSpec{Token: 3})
	msg := runCmd(t, cmd).(ClosedMsg)
	if msg.Err == nil {
		t.Fatalf("expected error when no editor resolves")
	}
	if !strings.Contains(msg.Err.Error(), "no editor") {
		t.Fatalf("err message: %v", msg.Err)
	}
}

func TestOpen_BannerStrippedBeforeParse(t *testing.T) {
	prevRun := runEditor
	t.Cleanup(func() { runEditor = prevRun })

	runEditor = func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), "<!-- ripjira:") {
			t.Errorf("temp file missing banner: %q", string(data))
		}
		if !strings.Contains(string(data), "# Existing") {
			t.Errorf("temp file missing seeded H1: %q", string(data))
		}
		return nil
	}

	cmd := Open(OpenSpec{Summary: "Existing", Body: "Body", Title: "ABC-9"})
	msg := runCmd(t, cmd).(ClosedMsg)
	if msg.Err != nil {
		t.Fatalf("err: %v", msg.Err)
	}
	if msg.Summary != "Existing" || msg.Body != "Body" {
		t.Fatalf("parse: got %q / %q", msg.Summary, msg.Body)
	}
}
