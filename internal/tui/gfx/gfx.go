// Package gfx detects the host terminal's inline-graphics protocol and
// renders raster images into the appropriate escape sequence. ripjira uses
// this only for "nice to have" previews (issue attachments); on terminals
// without graphics support the public API returns ("", false) so callers
// can fall back to a textual placeholder.
package gfx

import (
	"bytes"
	"image"
	// Decoder side effects: register image/gif, image/jpeg, image/png
	// handlers with the standard image package so that image.Decode can
	// recognise the formats Jira typically returns for attachments.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/BourgeoisBear/rasterm" // used by Render
)

// Warm performs the env-only protocol detection and primes the internal
// cache so that later calls to Detect (typically from inside a Bubble Tea
// Update loop) never touch the TTY. Call this once at process startup,
// before tea.Program.Run, to avoid synchronous terminal queries that
// would otherwise stall the UI on first use.
func Warm() Protocol { return Detect() }

// Protocol identifies the inline-image protocol the host terminal speaks.
type Protocol int

const (
	// None means no inline graphics are available; callers should render
	// a textual placeholder.
	None Protocol = iota
	// Kitty is the Kitty graphics protocol (Kitty, Ghostty, WezTerm).
	Kitty
	// Iterm is the iTerm2 inline-image escape (iTerm2, WezTerm).
	Iterm
)

// Detect reports the best inline-image protocol available on the host
// terminal. The result is cached for the process lifetime — terminal
// capabilities don't change after launch. When ripjira is not attached to
// a TTY (tests, redirected stdout) the result is None.
func Detect() Protocol {
	detectOnce.Do(func() {
		if !isTerminal(os.Stdout) {
			detectResult = None
			return
		}
		// Env-only detection: rasterm's IsKittyCapable / IsItermCapable
		// fall back to a synchronous TTY query (CSI primary device
		// attributes) when env vars are inconclusive. Inside an already
		// running Bubble Tea program the response is consumed by tea's
		// input reader and the query stalls for its full timeout
		// (~2–3s), which appears to the user as a hang on first issue
		// selection. Env vars are reliable on every modern terminal we
		// care about, so we deliberately skip the TTY round-trip.
		switch {
		case envSuggestsKitty():
			detectResult = Kitty
		case envSuggestsIterm():
			detectResult = Iterm
		default:
			detectResult = None
		}
	})
	return detectResult
}

// Render encodes data (a PNG, JPEG, or GIF image) as the appropriate
// inline-image escape sequence for the detected terminal, sized to fit
// within `cols` × `rows` terminal cells while preserving aspect. Pass
// cols/rows ≤ 0 to let the terminal pick a size from the image's pixel
// dimensions. Returns the escape and the actual cell footprint the
// renderer asked the terminal to use, so callers can centre or pad the
// surrounding layout.
//
// Returns ("", 0, 0, false) when no protocol is available, the image
// fails to decode, or rasterm errors. Callers should fall back to a
// placeholder in that case.
func Render(data []byte, cols, rows int) (escape string, fitCols, fitRows int, ok bool) {
	p := Detect()
	if p == None {
		return "", 0, 0, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, false
	}
	fitCols, fitRows = fitCells(img, cols, rows)
	var buf strings.Builder
	switch p {
	case Kitty:
		opts := rasterm.KittyImgOpts{}
		if fitCols > 0 {
			opts.DstCols = uint32(fitCols)
		}
		if fitRows > 0 {
			opts.DstRows = uint32(fitRows)
		}
		// DECSC/DECRC bracketing keeps the terminal cursor at the
		// pre-escape position. Kitty's default ("C=0") advances the
		// cursor to the bottom-right of the placed image, which
		// overwrites the rest of the line bubbletea is about to write
		// from this position — the visible result is the layout
		// scrolling up by image-height rows on first paint. Save and
		// restore lets bubbletea keep drawing on the same row as if
		// the image were a zero-width glyph.
		buf.WriteString("\x1b7")
		if err := rasterm.KittyWriteImage(&buf, img, opts); err != nil {
			return "", 0, 0, false
		}
		buf.WriteString("\x1b8")
		// rasterm separates Kitty APC chunks with literal "\n", which
		// lipgloss measures as real rows and uses to grow the host
		// pane's height — pushing surrounding content up. Inter-chunk
		// whitespace isn't required by the Kitty protocol, so we strip
		// it out and emit a single 0-width logical line.
		return strings.ReplaceAll(buf.String(), "\x1b\\\n\x1b_", "\x1b\\\x1b_"),
			fitCols, fitRows, true
	case Iterm:
		if err := rasterm.ItermWriteImage(&buf, img); err != nil {
			return "", 0, 0, false
		}
	default:
		return "", 0, 0, false
	}
	return buf.String(), fitCols, fitRows, true
}

// cellPixelAspect is the assumed pixel aspect ratio of one terminal cell
// (width / height). Most monospaced fonts at common sizes produce cells
// roughly twice as tall as they are wide; 0.5 is a safe default that
// matches Kitty, Ghostty, iTerm2, and WezTerm in practice.
const cellPixelAspect = 0.5

// fitCells picks a cell-grid size that contains img within maxCols ×
// maxRows while preserving the image's pixel aspect ratio. Either input
// may be ≤ 0 to disable that bound (the terminal then auto-derives the
// missing dimension from the image's pixel size).
func fitCells(img image.Image, maxCols, maxRows int) (cols, rows int) {
	if maxCols <= 0 && maxRows <= 0 {
		return 0, 0
	}
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return maxCols, maxRows
	}
	// Cells the image would take at "native" size, given the assumed
	// cell pixel aspect. We don't actually know the host's font metrics,
	// but cellPixelAspect is close enough that the fit math stays within
	// a row or two of correct on every terminal we care about.
	imgAspect := float64(w) / float64(h)
	cellAspect := imgAspect / cellPixelAspect
	if maxCols <= 0 {
		rows = maxRows
		cols = max(int(float64(rows)*cellAspect), 1)
		return
	}
	if maxRows <= 0 {
		cols = maxCols
		rows = max(int(float64(cols)/cellAspect), 1)
		return
	}
	// Both bounds set: scale by whichever axis hits the limit first.
	if float64(maxCols)/cellAspect <= float64(maxRows) {
		cols = maxCols
		rows = int(float64(cols) / cellAspect)
	} else {
		rows = maxRows
		cols = int(float64(rows) * cellAspect)
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return
}

// ClearAll returns the escape sequence that erases every image the host
// terminal has on screen. Kitty graphics persist across redraws until
// explicitly deleted, so we emit this when leaving the preview pane.
// Returns "" for protocols where redraw alone clears the image (iTerm2
// inline images live in their cell and disappear when bubbletea repaints
// that row).
func ClearAll() string {
	if Detect() == Kitty {
		return "\x1b_Ga=d,d=A\x1b\\"
	}
	return ""
}

var (
	detectOnce   syncOnce
	detectResult Protocol
)

// syncOnce is a tiny shim around sync.Once that lets tests reset detection.
// We keep it private — Detect() handles the public surface.
type syncOnce struct{ done bool }

func (s *syncOnce) Do(f func()) {
	if !s.done {
		f()
		s.done = true
	}
}

// envSuggestsKitty covers terminals that speak the Kitty graphics protocol
// but don't satisfy rasterm's stricter env checks (Ghostty, WezTerm with
// the protocol enabled). Conservative on purpose: terminals that don't
// implement the protocol must not match here.
func envSuggestsKitty() bool {
	if t := os.Getenv("TERM"); strings.Contains(t, "kitty") || strings.Contains(t, "ghostty") {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "Ghostty", "WezTerm":
		return true
	}
	return os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != ""
}

// envSuggestsIterm covers iTerm2-flavoured terminals that don't satisfy
// rasterm's check (some WezTerm builds, terminals that announce iTerm2
// compatibility via TERM_PROGRAM).
func envSuggestsIterm() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm":
		return true
	}
	return os.Getenv("ITERM_SESSION_ID") != ""
}

// isTerminal returns true if f refers to a TTY. We avoid pulling in
// golang.org/x/term to stay aligned with what bubbletea already uses
// (mattn/go-isatty is in go.sum); but rasterm's checks already handle
// that, so we only need a cheap pre-flight.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
