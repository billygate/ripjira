// Package depspin pins direct module dependencies until consumed by later tasks.
// It is a temporary anchor; remove once each dep is used by real code.
package depspin

// Anchor imports keep the modules listed as direct deps until later tasks consume them.
import (
	_ "github.com/charmbracelet/bubbles/list"  // pinned for Stage 2
	_ "github.com/charmbracelet/bubbletea"     // pinned for Stage 2
	_ "github.com/charmbracelet/lipgloss"      // pinned for Stage 2
	_ "github.com/charmbracelet/x/exp/teatest" // pinned for Stage 2
	_ "github.com/zalando/go-keyring"          // pinned for Task 3
	_ "gopkg.in/yaml.v3"                       // pinned for Task 2
)
