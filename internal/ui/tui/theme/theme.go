// Package theme holds every style of the TUI, for light and dark terminals.
// The views get their styles from a Theme and never create styles.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the set of styles of the TUI.
type Theme struct {
	Title    lipgloss.Style // the first line of a view
	Header   lipgloss.Style // table headers
	Text     lipgloss.Style // normal text
	Muted    lipgloss.Style // help, hints, secondary facts
	Good     lipgloss.Style // finished items, good values
	Warn     lipgloss.Style // excluded runs, waits
	Bad      lipgloss.Style // errors and failed steps
	Selected lipgloss.Style // the selected table row
	Active   lipgloss.Style // the item that runs now

	// Accent is the main color, for progress bars and charts.
	Accent color.Color
	// Track is the color of the empty part of a progress bar.
	Track color.Color
}

// New returns the theme for a dark or a light terminal background.
func New(isDark bool) Theme {
	pick := lipgloss.LightDark(isDark)
	accent := pick(lipgloss.Color("#5A56E0"), lipgloss.Color("#9D9AFF"))
	muted := pick(lipgloss.Color("#6B6B6B"), lipgloss.Color("#9A9A9A"))
	text := pick(lipgloss.Color("#1F1F1F"), lipgloss.Color("#E6E6E6"))
	good := pick(lipgloss.Color("#1A7F37"), lipgloss.Color("#56D364"))
	warn := pick(lipgloss.Color("#9A6700"), lipgloss.Color("#E3B341"))
	bad := pick(lipgloss.Color("#CF222E"), lipgloss.Color("#FF7B72"))
	selected := pick(lipgloss.Color("#E7E6FB"), lipgloss.Color("#2E2C5C"))

	return Theme{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		Header:   lipgloss.NewStyle().Bold(true).Foreground(muted),
		Text:     lipgloss.NewStyle().Foreground(text),
		Muted:    lipgloss.NewStyle().Foreground(muted),
		Good:     lipgloss.NewStyle().Foreground(good),
		Warn:     lipgloss.NewStyle().Foreground(warn),
		Bad:      lipgloss.NewStyle().Foreground(bad),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(text).Background(selected),
		Active:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		Accent:   accent,
		Track:    pick(lipgloss.Color("#D0D0D0"), lipgloss.Color("#3A3A3A")),
	}
}
