package theme

import "testing"

func TestNewPicksColorsForTheBackground(t *testing.T) {
	t.Parallel()
	dark, light := New(true), New(false)
	if dark.Accent == light.Accent {
		t.Error("the accent color is the same for dark and light backgrounds")
	}
	if dark.Title.Render("x") == "" || light.Bad.Render("x") == "" {
		t.Error("a style rendered nothing")
	}
}
