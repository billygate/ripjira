package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeEditorEnv struct {
	hasNvim    bool
	hasBrew    bool
	isDarwin   bool
	installed  bool
	installErr error
}

func (f *fakeEditorEnv) HasNvim() bool  { return f.hasNvim }
func (f *fakeEditorEnv) HasBrew() bool  { return f.hasBrew }
func (f *fakeEditorEnv) IsDarwin() bool { return f.isDarwin }
func (f *fakeEditorEnv) RunBrewInstall() error {
	f.installed = true
	return f.installErr
}

type fakeAdviceState struct {
	shown   bool
	flipped bool
}

func (f *fakeAdviceState) AdviceShown() bool { return f.shown }
func (f *fakeAdviceState) MarkAdviceShown()  { f.flipped = true; f.shown = true }

func TestEditorAdvice_AlreadyShown_ReturnsNil(t *testing.T) {
	st := &fakeAdviceState{shown: true}
	env := &fakeEditorEnv{}
	if cmd := EditorAdviceCmd(st, env); cmd != nil {
		t.Fatalf("expected nil cmd when already shown, got non-nil")
	}
	if st.flipped {
		t.Fatal("flag should not be flipped when already shown")
	}
}

func TestEditorAdvice_NvimPresent_FlipsAndReturnsNil(t *testing.T) {
	st := &fakeAdviceState{}
	env := &fakeEditorEnv{hasNvim: true}
	cmd := EditorAdviceCmd(st, env)
	if cmd != nil {
		t.Fatalf("expected nil cmd when nvim present")
	}
	if !st.flipped {
		t.Fatal("flag should flip even when nvim is present")
	}
}

func TestEditorAdvice_LinuxNoNvim_TipToast(t *testing.T) {
	st := &fakeAdviceState{}
	env := &fakeEditorEnv{}
	cmd := EditorAdviceCmd(st, env)
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("got %T, want ToastMsg", msg)
	}
	if toast.Level != ToastInfo {
		t.Errorf("level: got %v want ToastInfo", toast.Level)
	}
	if !strings.Contains(toast.Text, "LazyVim") {
		t.Errorf("toast should mention LazyVim, got %q", toast.Text)
	}
	if !st.flipped {
		t.Fatal("flag must flip")
	}
}

func TestEditorAdvice_DarwinWithBrew_NoNvim_ReturnsConfirm(t *testing.T) {
	st := &fakeAdviceState{}
	env := &fakeEditorEnv{isDarwin: true, hasBrew: true}
	cmd := EditorAdviceCmd(st, env)
	if cmd == nil {
		t.Fatal("expected confirm cmd")
	}
	msg := cmd()
	if _, ok := msg.(EditorInstallConfirmMsg); !ok {
		t.Fatalf("got %T, want EditorInstallConfirmMsg", msg)
	}
	if !st.flipped {
		t.Fatal("flag must flip")
	}
}

func TestEditorAdvice_DarwinNoBrew_TipToast(t *testing.T) {
	st := &fakeAdviceState{}
	env := &fakeEditorEnv{isDarwin: true}
	cmd := EditorAdviceCmd(st, env)
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Fatalf("got %T, want ToastMsg", cmd())
	}
}

func TestRunBrewInstall_SuccessToasts(t *testing.T) {
	env := &fakeEditorEnv{}
	cmd := runBrewInstallCmd(env)
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("got %T, want ToastMsg", msg)
	}
	if toast.Level != ToastInfo {
		t.Errorf("expected ToastInfo on success, got %v", toast.Level)
	}
	if !env.installed {
		t.Fatal("RunBrewInstall not invoked")
	}
}

func TestRunBrewInstall_ErrorToasts(t *testing.T) {
	env := &fakeEditorEnv{installErr: errors.New("boom")}
	cmd := runBrewInstallCmd(env)
	msg := cmd().(ToastMsg)
	if msg.Level != ToastError {
		t.Errorf("expected ToastError, got %v", msg.Level)
	}
	if !strings.Contains(msg.Text, "Install failed") {
		t.Errorf("toast text: %q", msg.Text)
	}
}

func TestModel_InstallPrompt_AcceptRunsBrew(t *testing.T) {
	m := newTestModel(t)
	env := &fakeEditorEnv{isDarwin: true, hasBrew: true}
	m.editorEnv = env
	m.editorInstallPrompt = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if m.editorInstallPrompt {
		t.Fatal("prompt should be dismissed after y")
	}
	if cmd == nil {
		t.Fatal("expected install cmd")
	}
	_ = cmd()
	if !env.installed {
		t.Fatal("brew install not invoked")
	}
}

func TestModel_InstallPrompt_DeclineToasts(t *testing.T) {
	m := newTestModel(t)
	env := &fakeEditorEnv{isDarwin: true, hasBrew: true}
	m.editorEnv = env
	m.editorInstallPrompt = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected tip toast")
	}
	if env.installed {
		t.Fatal("decline must not install")
	}
	msg := cmd()
	if _, ok := msg.(ToastMsg); !ok {
		t.Fatalf("got %T, want ToastMsg", msg)
	}
}
