// Package runview is the Bubble Tea program that shows a session while it
// runs. A pure reducer (State.Apply) turns the runner events into the state
// of the view. The model adds the keys, the countdown, the theme, and the
// summary after the session. The view is inline, so that its last frame
// stays in the terminal history.
package runview
