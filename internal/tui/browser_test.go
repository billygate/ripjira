package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// fakeOpener records every URL passed to Open and optionally returns a
// pre-canned error so tests can assert the toast path too.
type fakeOpener struct {
	urls []string
	err  error
}

func (f *fakeOpener) Open(url string) error {
	f.urls = append(f.urls, url)
	return f.err
}

func TestOpenerCommand_Dispatch(t *testing.T) {
	cases := []struct {
		goos    string
		wantCmd string
		wantArg string
		wantErr bool
	}{
		{"darwin", "open", "https://x/browse/PROJ-1", false},
		{"linux", "xdg-open", "https://x/browse/PROJ-1", false},
		{"freebsd", "xdg-open", "https://x/browse/PROJ-1", false},
		{"windows", "rundll32", "url.dll,FileProtocolHandler", false},
		{"plan9", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			cmd, args, err := openerCommand(c.goos, "https://x/browse/PROJ-1")
			if c.wantErr {
				if err == nil {
					t.Fatalf("openerCommand(%q) err = nil, want non-nil", c.goos)
				}
				return
			}
			if err != nil {
				t.Fatalf("openerCommand(%q) unexpected err: %v", c.goos, err)
			}
			if cmd != c.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, c.wantCmd)
			}
			if len(args) == 0 || args[0] != c.wantArg {
				t.Errorf("args[0] = %v, want %q", args, c.wantArg)
			}
		})
	}
}

func newBrowserTestModel(t *testing.T, opener BrowserOpener) Model {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	issue := jira.Issue{
		Key:     "PROJ-1",
		Summary: "First",
		Status:  jira.Status{Name: "To Do", Category: "new"},
		URL:     "https://example.atlassian.net/browse/PROJ-1",
	}
	model := New(p, WithBrowserOpener(opener), WithInitialIssues([]jira.Issue{issue}))
	var mod tea.Model = model
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// First list row is the group header; ↓ moves onto the issue row so
	// detail.Issue() becomes non-nil.
	mod, _ = mod.Update(tea.KeyMsg{Type: tea.KeyDown})
	return mod.(Model)
}

func TestOpenInBrowser_PressingO_InvokesOpener(t *testing.T) {
	fake := &fakeOpener{}
	m := newBrowserTestModel(t, fake)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("'o' did not produce a command")
	}
	msg := cmd()
	if _, ok := msg.(browserOpenedMsg); !ok {
		t.Fatalf("expected browserOpenedMsg, got %T", msg)
	}
	if got := len(fake.urls); got != 1 {
		t.Fatalf("opener called %d times, want 1", got)
	}
	if fake.urls[0] != "https://example.atlassian.net/browse/PROJ-1" {
		t.Errorf("opener received url %q, want PROJ-1 browse URL", fake.urls[0])
	}
	// Pressing `o` should not change focus or open any overlay.
	m2 := updated.(Model)
	if m2.HelpVisible() || m2.TransitionVisible() || m2.CommentVisible() || m2.AssignVisible() {
		t.Error("'o' opened an overlay; it should only invoke the opener")
	}
}

func TestOpenInBrowser_NoSelection_NoOp(t *testing.T) {
	fake := &fakeOpener{}
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	m := New(p, WithBrowserOpener(fake))
	// no SetIssues — list is empty, nothing selected
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd != nil {
		// Drain it to be sure the opener was not invoked.
		_ = cmd()
	}
	if len(fake.urls) != 0 {
		t.Errorf("opener invoked with empty selection: %v", fake.urls)
	}
}

func TestOpenInBrowser_OpenerError_PushesToast(t *testing.T) {
	fake := &fakeOpener{err: errors.New("boom")}
	m := newBrowserTestModel(t, fake)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("'o' did not produce a command")
	}
	msg := cmd()
	_, cmd2 := m.Update(msg)
	if cmd2 == nil {
		t.Fatal("browserOpenedMsg with error did not yield a toast cmd")
	}
	tmsg := cmd2()
	tm, ok := tmsg.(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %T", tmsg)
	}
	if tm.Level != ToastError {
		t.Errorf("toast level = %v, want ToastError", tm.Level)
	}
	if !strings.Contains(tm.Text, "boom") {
		t.Errorf("toast text %q does not mention underlying error", tm.Text)
	}
}

func TestOpenInBrowser_SuccessIsSilent(t *testing.T) {
	m := newBrowserTestModel(t, &fakeOpener{})
	_, cmd := m.Update(browserOpenedMsg{URL: "https://x", Err: nil})
	if cmd != nil {
		t.Errorf("successful open produced a follow-up cmd: %v", cmd)
	}
	if !m.Toasts().Empty() {
		t.Error("successful open pushed a toast; success should be silent")
	}
}
