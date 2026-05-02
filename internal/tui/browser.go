package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// BrowserOpener launches a URL in the user's preferred web browser. The
// interface is small on purpose so tests can substitute a fake that records
// the requested URL instead of actually shelling out.
type BrowserOpener interface {
	Open(url string) error
}

// OSOpener is the production BrowserOpener. It dispatches to the platform's
// canonical "open this URL" command — `open` on darwin, `xdg-open` on linux,
// and `rundll32 url.dll,FileProtocolHandler` on windows (the variant that
// works for arbitrary URLs without spawning a cmd.exe shell).
type OSOpener struct{}

// Open implements BrowserOpener using the host OS's launcher.
func (OSOpener) Open(url string) error {
	cmd, args, err := openerCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(cmd, args...).Start()
}

// openerCommand returns the executable and args used to open a URL on goos.
// Split out from Open so tests can assert the dispatch table without spawning
// processes.
func openerCommand(goos, url string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "linux", "freebsd", "openbsd", "netbsd":
		return "xdg-open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	default:
		return "", nil, fmt.Errorf("ripjira: open in browser unsupported on %s", goos)
	}
}
