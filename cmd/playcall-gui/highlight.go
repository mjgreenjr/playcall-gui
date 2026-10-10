package main

import "fyne.io/fyne/v2"
import "fyne.io/fyne/v2/theme"

// highlight is a choice for coloring urgent plays (scores, turnovers).
type highlight struct {
	key   string // what config.json stores
	label string
	color fyne.ThemeColorName
}

// highlights lists the choices; the first is the default.
var highlights = []highlight{
	{"blue", "Blue", theme.ColorNamePrimary},
	{"green", "Green", theme.ColorNameSuccess},
	{"orange", "Orange", theme.ColorNameWarning},
	{"red", "Red", theme.ColorNameError},
	{"none", "None (bold only)", theme.ColorNameForeground},
}

// highlightByKey finds a choice by its stored key, the default if unknown.
func highlightByKey(key string) highlight {
	for _, h := range highlights {
		if h.key == key {
			return h
		}
	}
	return highlights[0]
}

func highlightByLabel(label string) highlight {
	for _, h := range highlights {
		if h.label == label {
			return h
		}
	}
	return highlights[0]
}

func highlightLabels() []string {
	out := make([]string, len(highlights))
	for i, h := range highlights {
		out[i] = h.label
	}
	return out
}

// rowColor is the color of a play's text: the highlight for urgent plays,
// the normal text color for the rest.
func rowColor(r Row, h highlight) fyne.ThemeColorName {
	if r.Urgent {
		return h.color
	}
	return theme.ColorNameForeground
}
