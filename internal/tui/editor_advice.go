package tui

import (
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/state"
)

// EditorAdviceState is the subset of the runtime state store the advice
// command needs. Defined as an interface so tests don't need a real file
// on disk.
type EditorAdviceState interface {
	AdviceShown() bool
	MarkAdviceShown()
}

// EditorEnv abstracts the host environment so the advice command can be
// unit-tested without invoking real binaries or depending on GOOS.
type EditorEnv interface {
	HasNvim() bool
	HasBrew() bool
	IsDarwin() bool
	RunBrewInstall() error
}

// EditorInstallConfirmMsg asks the app to surface a Y/N modal asking
// whether to install Neovim via Homebrew. Sent only on macOS+brew systems
// without nvim, on first launch.
type EditorInstallConfirmMsg struct{}

// EditorAdviceCmd returns the first-launch editor advice command, or nil
// when the advice has already been shown. Always flips the flag (even
// when nvim is present) so the gate fires exactly once per install.
func EditorAdviceCmd(state EditorAdviceState, env EditorEnv) tea.Cmd {
	if state.AdviceShown() {
		return nil
	}
	state.MarkAdviceShown()
	if env.HasNvim() {
		return nil
	}
	if env.IsDarwin() && env.HasBrew() {
		return func() tea.Msg { return EditorInstallConfirmMsg{} }
	}
	return func() tea.Msg {
		return ToastMsg{
			Text: "Tip: install Neovim for richer description editing — " +
				"LazyVim (https://www.lazyvim.org) is a popular preset. Any $EDITOR works.",
			Level: ToastInfo,
		}
	}
}

// runBrewInstallCmd returns a tea.Cmd that runs the env's brew install
// and emits a ToastMsg describing the outcome.
func runBrewInstallCmd(env EditorEnv) tea.Cmd {
	return func() tea.Msg {
		if err := env.RunBrewInstall(); err != nil {
			return ToastMsg{Text: "Install failed: " + err.Error(), Level: ToastError}
		}
		return ToastMsg{Text: "Neovim installed via Homebrew.", Level: ToastInfo}
	}
}

// defaultEditorEnv is the production EditorEnv backed by exec.LookPath
// and runtime.GOOS. Brew install runs synchronously, blocking the main
// goroutine for the duration — Task 10 wraps this in a confirm-overlay
// + suspend dance.
type defaultEditorEnv struct{}

func (defaultEditorEnv) HasNvim() bool {
	_, err := exec.LookPath("nvim")
	return err == nil
}

func (defaultEditorEnv) HasBrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func (defaultEditorEnv) IsDarwin() bool { return runtime.GOOS == "darwin" }

func (defaultEditorEnv) RunBrewInstall() error {
	cmd := exec.Command("brew", "install", "neovim")
	return cmd.Run()
}

// DefaultEditorEnv returns the production EditorEnv. Used by app bootstrap.
func DefaultEditorEnv() EditorEnv { return defaultEditorEnv{} }

// modelAdviceState adapts the Model's state-file plumbing to the
// EditorAdviceState interface. AdviceShown reads the state file once;
// MarkAdviceShown writes via state.Mutate. Errors are swallowed — the
// flag is best-effort and a missing or unwritable state file should not
// block startup.
type modelAdviceState struct {
	path string
}

func (a modelAdviceState) AdviceShown() bool {
	if a.path == "" {
		return true // no state path → don't show advice (test-mode safe default)
	}
	s, err := state.Load(a.path)
	if err != nil {
		return true
	}
	return s.EditorAdviceShown
}

func (a modelAdviceState) MarkAdviceShown() {
	if a.path == "" {
		return
	}
	_ = state.Mutate(a.path, func(s *state.State) {
		s.EditorAdviceShown = true
	})
}
