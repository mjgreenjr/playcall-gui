package main

import "fyne.io/fyne/v2"

// Small vector balls for the possession marker. Color emoji do not render in
// Fyne's default font, so these are drawn.
var (
	footballRes = fyne.NewStaticResource("football.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
<ellipse cx="16" cy="16" rx="14" ry="8.5" fill="#8b4a1c" stroke="#5a2e10" stroke-width="1"/>
<path d="M6 9.5 Q4.2 16 6 22.5" fill="none" stroke="#f2f2f2" stroke-width="1.6"/>
<path d="M26 9.5 Q27.8 16 26 22.5" fill="none" stroke="#f2f2f2" stroke-width="1.6"/>
<path d="M11 16 L21 16" fill="none" stroke="#f2f2f2" stroke-width="1.4"/>
<path d="M13 13.8 L13 18.2 M16 13.8 L16 18.2 M19 13.8 L19 18.2" fill="none" stroke="#f2f2f2" stroke-width="1.2"/>
</svg>`))

	baseballRes = fyne.NewStaticResource("baseball.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
<circle cx="16" cy="16" r="13.5" fill="#f4f4f0" stroke="#9a9a94" stroke-width="1"/>
<path d="M9.5 5 Q15 16 9.5 27" fill="none" stroke="#c0392b" stroke-width="1.6"/>
<path d="M22.5 5 Q17 16 22.5 27" fill="none" stroke="#c0392b" stroke-width="1.6"/>
</svg>`))
)

// ballFor is the possession marker for a sport, nil when it has none.
func ballFor(sport string) fyne.Resource {
	switch sport {
	case "nfl", "cfb":
		return footballRes
	case "mlb":
		return baseballRes
	}
	return nil
}
