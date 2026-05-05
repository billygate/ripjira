package structure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// Event signals that <dir>/<ProjectKey>.yml changed. Consumers should call
// Store.Load(ProjectKey) to refresh.
type Event struct {
	ProjectKey string
}

// Watch installs a directory watcher and emits Event values until ctx is
// cancelled. The directory is created if missing. The returned channel is
// closed when the watcher stops (ctx done or fatal error). Non-YAML files
// and files with no project-key basename are ignored.
//
// Bursty filesystem events (atomic-write rename + create) are not debounced
// here; the consumer is expected to be cheap (Store.Load reads one small
// file). Add coalescing in the consumer if needed.
func Watch(ctx context.Context, dir string) (<-chan Event, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify: %w", err)
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("watch %s: %w", dir, err)
	}
	out := make(chan Event, 8)
	go func() {
		defer close(out)
		defer func() { _ = w.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if pk, ok := projectFromPath(ev.Name); ok {
					select {
					case out <- Event{ProjectKey: pk}:
					case <-ctx.Done():
						return
					}
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, nil
}

func projectFromPath(p string) (string, bool) {
	base := filepath.Base(p)
	if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
		return "", false
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}
