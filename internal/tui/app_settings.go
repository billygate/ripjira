package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// handleSettingsApplied diffs the newly-applied config against the
// in-memory cfg and applies live-affecting changes (theme/styles, auto
// refresh tick), then asynchronously persists the file. Diff scope is
// limited to fields the Settings overlay can edit.
func (m Model) handleSettingsApplied(msg overlays.SettingsAppliedMsg) (Model, tea.Cmd) {
	newCfg := msg.NewCfg
	if err := newCfg.Validate(); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "invalid settings: " + err.Error(), Level: ToastError}
		}
	}

	var cmds []tea.Cmd

	if newCfg.Theme != m.cfg.Theme {
		if pal, err := themes.ByName(newCfg.Theme); err == nil {
			m.palette = pal
			m.styles = styles.New(pal)
		}
	}
	// Icons: persisted but currently has no rendering effect; intentional —
	// icon rendering is tracked in the design spec.

	if newCfg.AutoRefreshSeconds != m.cfg.AutoRefreshSeconds {
		m.autoRefresh = time.Duration(newCfg.AutoRefreshSeconds) * time.Second
		if tick := m.scheduleAutoRefresh(); tick != nil {
			cmds = append(cmds, tick)
		}
	}

	// Always sync — including the empty-list case — so deleting every epic
	// type in the sub-overlay actually drops them from the parent-grouping
	// strategy without waiting for a restart.
	m.epicTypes = append([]string(nil), newCfg.EpicIssueTypes...)

	m.cfg = newCfg

	if m.cfgPath != "" {
		path := m.cfgPath
		cfgCopy := newCfg
		cmds = append(cmds, func() tea.Msg {
			if err := config.Save(path, &cfgCopy); err != nil {
				return SettingsSaveErrorMsg{Draft: cfgCopy, Err: err}
			}
			return nil
		})
	}
	return m, tea.Batch(cmds...)
}

// handleSettingsSaveError surfaces the failure as a toast and re-opens
// the Settings overlay with the user's draft so they can adjust and try
// again.
func (m Model) handleSettingsSaveError(msg SettingsSaveErrorMsg) (Model, tea.Cmd) {
	m.settings = m.settings.Show(msg.Draft)
	return m, func() tea.Msg {
		return ToastMsg{Text: "failed to save settings: " + msg.Err.Error(), Level: ToastError}
	}
}
