package structure

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch_FiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BIL.yml")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events, err := Watch(ctx, dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	body := []byte("- id: u\n  name: U\n  sections:\n    - title: T\n      filter:\n        status: [Open]\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-events:
		if ev.ProjectKey != "BIL" {
			t.Fatalf("project = %q", ev.ProjectKey)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}
