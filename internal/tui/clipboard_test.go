package tui

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCopyToClipboard_EmitsOSC52(t *testing.T) {
	var buf bytes.Buffer
	if err := copyToClipboard(&buf, "PROJ-1"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("missing OSC 52 framing: %q", out)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	dec, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(dec) != "PROJ-1" {
		t.Fatalf("payload = %q, want PROJ-1", dec)
	}
}
