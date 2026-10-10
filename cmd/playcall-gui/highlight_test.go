package main

import (
	"testing"

	"fyne.io/fyne/v2/theme"
)

func TestHighlightChoices(t *testing.T) {
	if highlightByKey("green").label != "Green" {
		t.Error("green")
	}
	if highlightByKey("").key != "blue" || highlightByKey("bogus").key != "blue" {
		t.Error("unknown or empty should be the default, blue")
	}
	for _, h := range highlights {
		if highlightByLabel(h.label).key != h.key {
			t.Errorf("%s does not round-trip through its label", h.key)
		}
	}
}

func TestRowColor(t *testing.T) {
	red := highlightByKey("red")
	if rowColor(Row{Urgent: true}, red) != theme.ColorNameError {
		t.Error("urgent play should use the highlight")
	}
	if rowColor(Row{}, red) != theme.ColorNameForeground {
		t.Error("normal play should use the normal color")
	}
	none := highlightByKey("none")
	if rowColor(Row{Urgent: true}, none) != theme.ColorNameForeground {
		t.Error("'none' should leave urgent plays in the normal color")
	}
}
