package tui

import (
	"strings"

	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// persistLastView writes the currently active ViewKind to state.json so
// the next session boots into the user's last view instead of MyTasks.
func (m *Model) persistLastView(v panes.ViewKind) {
	if m.statePath == "" {
		return
	}
	path := m.statePath
	id := int(v)
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			s.LastView = &id
		})
	}()
}

// persistLastSubView writes the active sub-view under top to state.json so
// the next session restores the user's scope. Async; no-op without a state
// path.
func (m *Model) persistLastSubView(top panes.TopTabKind, v panes.ViewKind) {
	if m.statePath == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if s.LastSubView == nil {
				s.LastSubView = map[int]int{}
			}
			s.LastSubView[int(top)] = int(v)
		})
	}()
}

// persistLastStructure writes the structure id for project to state.json
// asynchronously. No-op when state path is unset.
func (m *Model) persistLastStructure(project, id string) {
	if m.statePath == "" || project == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if s.LastStructure == nil {
				s.LastStructure = map[string]string{}
			}
			s.LastStructure[project] = id
		})
	}()
}

// loadDraft returns the saved comment-in-progress for issueKey, or "".
// Reads the in-memory cache populated at startup; never touches disk.
func (m Model) loadDraft(issueKey string) string {
	if issueKey == "" {
		return ""
	}
	return m.commentDrafts[issueKey]
}

// saveDraft updates the cached draft synchronously and persists to
// state.json in the background. Empty bodies clear the draft.
func (m Model) saveDraft(issueKey, body string) {
	if issueKey == "" {
		return
	}
	if strings.TrimSpace(body) == "" {
		delete(m.commentDrafts, issueKey)
	} else {
		if m.commentDrafts == nil {
			m.commentDrafts = map[string]string{}
		}
		m.commentDrafts[issueKey] = body
	}
	if m.statePath == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if strings.TrimSpace(body) == "" {
				delete(s.CommentDrafts, issueKey)
				return
			}
			if s.CommentDrafts == nil {
				s.CommentDrafts = map[string]string{}
			}
			s.CommentDrafts[issueKey] = body
		})
	}()
}

// clearDraft drops the stored draft for issueKey.
func (m Model) clearDraft(issueKey string) { m.saveDraft(issueKey, "") }

// loadFavoriteEntries returns the saved favorites as overlay entries, or
// an empty slice when state is unavailable.
func (m Model) loadFavoriteEntries() []overlays.FavoriteEntry {
	if m.statePath == "" {
		return nil
	}
	st, err := state.Load(m.statePath)
	if err != nil {
		return nil
	}
	out := make([]overlays.FavoriteEntry, 0, len(st.Favorites))
	for _, f := range st.Favorites {
		out = append(out, overlays.FavoriteEntry{Name: f.Name, JQL: f.JQL})
	}
	return out
}
