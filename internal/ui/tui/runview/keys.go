package runview

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"

	"dyno/internal/ui/format"
	"dyno/internal/ui/tui/theme"
)

// Texts of the key hint.
const (
	savedNote    = "finished runs are saved"
	confirmText  = "press q again to stop"
	stoppingText = "stopping"
)

// keyMap holds the keys of the run view.
type keyMap struct {
	Stop      key.Binding // q, twice, while the session runs
	Interrupt key.Binding // ctrl+c while the session runs
	Quit      key.Binding // after the session
}

func newKeyMap() keyMap {
	return keyMap{
		Stop:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "stop")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "stop now")),
		Quit:      key.NewBinding(key.WithKeys("q", "enter", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// hintMode selects the key hint of the footer.
type hintMode int

const (
	hintRunning  hintMode = iota // q stops after a second q
	hintConfirm                  // the first q came
	hintStopping                 // stop was called
	hintDone                     // the session function returned
	hintNone                     // the program quits
)

// newHelp returns the help model with the styles of t.
func newHelp(t theme.Theme) help.Model {
	h := help.New()
	h.ShortSeparator = format.Separator
	h.Styles = help.Styles{
		Ellipsis:       t.Muted,
		ShortKey:       t.Text,
		ShortDesc:      t.Muted,
		ShortSeparator: t.Muted,
	}
	return h
}

// hint returns the key hint for mode.
func hint(mode hintMode, k keyMap, h help.Model, t theme.Theme) string {
	switch mode {
	case hintRunning:
		return h.ShortHelpView([]key.Binding{k.Stop}) + t.Muted.Render(format.Separator+savedNote)
	case hintConfirm:
		return t.Warn.Render(confirmText)
	case hintStopping:
		return t.Warn.Render(stoppingText + format.Separator + savedNote)
	case hintDone:
		return h.ShortHelpView([]key.Binding{k.Quit})
	}
	return ""
}
