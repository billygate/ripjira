package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// OpenSpec describes the buffer the editor should be launched on.
type OpenSpec struct {
	Summary string
	Body    string
	Title   string
	Token   int
}

// ClosedMsg is published when the editor exits. Cancelled is true on :cq /
// non-zero exit. On Err, Summary/Body are zero. On success Summary may still
// be "" — that means the file had no `# H1`, and the caller should leave the
// existing summary untouched.
type ClosedMsg struct {
	Token     int
	Cancelled bool
	Summary   string
	Body      string
	Err       error
}

// resolveEditor is a package variable so tests can stub the binary lookup.
// $EDITOR → nvim → vi.
var resolveEditor = func() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if p, err := exec.LookPath("nvim"); err == nil {
		return p
	}
	if p, err := exec.LookPath("vi"); err == nil {
		return p
	}
	return ""
}

// runEditor is the exec seam. The default invokes the resolved binary with
// stdio bound to the controlling terminal. Tests replace it with a fake
// that mutates the temp file directly.
var runEditor = func(path string) error {
	bin := resolveEditor()
	if bin == "" {
		return errors.New("no editor found in $PATH (set $EDITOR or install nvim/vim)")
	}
	cmd := exec.Command(bin, path) //nolint:gosec // user-controlled by design
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Open returns a tea.Cmd that creates a temp .md file seeded from spec,
// invokes the editor, then parses the result into a ClosedMsg.
func Open(spec OpenSpec) tea.Cmd {
	return func() tea.Msg {
		if resolveEditor() == "" {
			return ClosedMsg{
				Token: spec.Token,
				Err:   errors.New("no editor found in $PATH (set $EDITOR or install nvim/vim)"),
			}
		}

		f, err := os.CreateTemp("", "ripjira-*.md")
		if err != nil {
			return ClosedMsg{Token: spec.Token, Err: fmt.Errorf("create temp: %w", err)}
		}
		path := f.Name()
		defer func() { _ = os.Remove(path) }()

		if _, err := f.WriteString(buildSeed(spec)); err != nil {
			_ = f.Close()
			return ClosedMsg{Token: spec.Token, Err: fmt.Errorf("write seed: %w", err)}
		}
		if err := f.Close(); err != nil {
			return ClosedMsg{Token: spec.Token, Err: fmt.Errorf("close seed: %w", err)}
		}

		if err := runEditor(path); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return ClosedMsg{Token: spec.Token, Cancelled: true}
			}
			return ClosedMsg{Token: spec.Token, Err: err}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return ClosedMsg{Token: spec.Token, Err: fmt.Errorf("read result: %w", err)}
		}
		summary, body := SplitSummaryBody(string(data))
		return ClosedMsg{Token: spec.Token, Summary: summary, Body: body}
	}
}

// buildSeed assembles the temp-file contents we initially write. The banner
// is an HTML comment so commonmark parsers ignore it; SplitSummaryBody also
// strips it explicitly before parsing.
func buildSeed(spec OpenSpec) string {
	title := spec.Title
	if title == "" {
		title = "issue"
	}
	banner := "<!-- ripjira: edit " + title +
		" — :wq to apply, :cq to cancel. The first \"# Heading\" becomes the summary; everything after is the description body. -->\n"
	h1 := "# " + spec.Summary + "\n"
	if spec.Body == "" {
		return banner + h1 + "\n"
	}
	return banner + h1 + "\n" + spec.Body + "\n"
}
