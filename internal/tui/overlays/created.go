package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// CreatedDismissedMsg is published when the post-create popup closes. The
// root model uses Key to attempt selecting the new issue in the list when
// it is part of the current view.
type CreatedDismissedMsg struct{ Key string }

// CreatedCopyRequestedMsg asks the root model to copy Text to the clipboard
// and toast Label (e.g. "key" / "URL"). The overlay stays visible.
type CreatedCopyRequestedMsg struct {
	Text  string
	Label string
}

// CreatedOpenRequestedMsg asks the root model to open URL in the browser.
// The overlay closes immediately after emitting this.
type CreatedOpenRequestedMsg struct{ URL string }

// CreatedOpenInAppMsg asks the root model to navigate to the issue inside
// the TUI. The overlay closes immediately after emitting this.
type CreatedOpenInAppMsg struct{ Key string }

// Created is a small confirmation overlay shown after a successful issue
// creation. It carries no domain logic; it surfaces the new key, accepts a
// few hotkeys (copy/open/close), and emits messages for the root model.
type Created struct {
	visible bool
	key     string
	url     string

	copyKey   key.Binding
	copyURL   key.Binding
	openInApp key.Binding
	browser   key.Binding
	closeKey  key.Binding
}

// NewCreated builds a hidden Created overlay reusing the supplied key
// bindings (no new bindings registered in the keymap).
func NewCreated(copyKey, copyURL, openInApp, browser, closeKey key.Binding) Created {
	return Created{
		copyKey:   copyKey,
		copyURL:   copyURL,
		openInApp: openInApp,
		browser:   browser,
		closeKey:  closeKey,
	}
}

// Visible reports whether the overlay is currently shown.
func (c Created) Visible() bool { return c.visible }

// Key returns the issue key the overlay is presenting (empty when hidden).
func (c Created) Key() string { return c.key }

// URL returns the issue URL the overlay is presenting.
func (c Created) URL() string { return c.url }

// Show binds the overlay to the freshly created issue and makes it visible.
func (c Created) Show(issue jira.Issue) Created {
	c.visible = true
	c.key = issue.Key
	c.url = issue.URL
	return c
}

// Hide makes the overlay invisible without emitting any message.
func (c Created) Hide() Created {
	c.visible = false
	return c
}

// Update handles key messages while visible. y / Y request copies (overlay
// stays open); o opens the issue in-app and closes; O opens in browser and
// closes; Esc / Enter close. Any other key is swallowed.
func (c Created) Update(msg tea.Msg) (Created, tea.Cmd) {
	if !c.visible {
		return c, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch {
	case key.Matches(km, c.copyKey):
		text, label := c.key, "key"
		return c, func() tea.Msg { return CreatedCopyRequestedMsg{Text: text, Label: label} }
	case key.Matches(km, c.copyURL):
		if c.url == "" {
			return c, nil
		}
		text, label := c.url, "URL"
		return c, func() tea.Msg { return CreatedCopyRequestedMsg{Text: text, Label: label} }
	case key.Matches(km, c.openInApp):
		issueKey := c.key
		dismissed := CreatedDismissedMsg{Key: c.key}
		c.visible = false
		return c, tea.Batch(
			func() tea.Msg { return CreatedOpenInAppMsg{Key: issueKey} },
			func() tea.Msg { return dismissed },
		)
	case key.Matches(km, c.browser):
		if c.url == "" {
			c.visible = false
			dismissed := CreatedDismissedMsg{Key: c.key}
			return c, func() tea.Msg { return dismissed }
		}
		url := c.url
		dismissed := CreatedDismissedMsg{Key: c.key}
		c.visible = false
		return c, tea.Batch(
			func() tea.Msg { return CreatedOpenRequestedMsg{URL: url} },
			func() tea.Msg { return dismissed },
		)
	case key.Matches(km, c.closeKey), km.Type == tea.KeyEnter:
		dismissed := CreatedDismissedMsg{Key: c.key}
		c.visible = false
		return c, func() tea.Msg { return dismissed }
	}
	return c, nil
}

// View renders the centered popup.
func (c Created) View(s styles.Styles) string {
	if !c.visible {
		return ""
	}
	title := s.Accent.Render("Created " + c.key)
	hints := s.Muted.Render("y copy key   Y copy URL   o open   O browser   esc/enter close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", hints)
	return s.OverlayBorder.Render(body)
}
