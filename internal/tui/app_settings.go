package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/tui/overlays"
)

// handleSettingsApplied diffs the newly-applied config against the
// in-memory cfg and applies live-affecting changes (auto refresh tick,
// epic types), then asynchronously persists the file.
//
// Theme is special-cased: live re-styling leaves rendering artifacts on
// some terminals, so a theme change persists synchronously and then
// returns tea.Quit with restartRequested=true. The entry point reads
// the flag and re-execs the binary so the new theme paints from a
// clean alt-screen.
func (m Model) handleSettingsApplied(msg overlays.SettingsAppliedMsg) (Model, tea.Cmd) {
	newCfg := msg.NewCfg
	if err := newCfg.Validate(); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "invalid settings: " + err.Error(), Level: ToastError}
		}
	}

	themeChanged := newCfg.Theme != m.cfg.Theme

	// Apply non-theme live changes regardless of branch — the persisted
	// YAML reflects all of them.
	var cmds []tea.Cmd
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

	if themeChanged {
		// Save synchronously so the re-execed process reads the new
		// theme. Skip the live palette swap — the next process will
		// build styles from scratch and skipping the swap avoids a
		// one-frame flash of mid-transition rendering before exit.
		if m.cfgPath != "" {
			if err := config.Save(m.cfgPath, &newCfg); err != nil {
				errMsg := err.Error()
				return m, func() tea.Msg {
					return ToastMsg{Text: "failed to save settings: " + errMsg, Level: ToastError}
				}
			}
		}
		m.cfg = newCfg
		m.restartRequested = true
		cmds = append(cmds, tea.Quit)
		return m, tea.Batch(cmds...)
	}

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
