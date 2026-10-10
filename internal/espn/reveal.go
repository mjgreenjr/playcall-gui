package espn

import "encoding/json"

// PlayHolders returns the objects holding a "plays" list, in the order
// ExtractPlays reads them.
func PlayHolders(d Data) []Data {
	var hs []Data
	add := func(m Data) {
		if _, ok := m["plays"].([]any); ok {
			hs = append(hs, m)
		}
	}
	add(d)
	gp := Map(d["gamepackageJSON"])
	add(Map(gp["playback"]))
	drives := d["drives"]
	if !Truthy(drives) {
		drives = gp["drives"]
	}
	var list []any
	if m, ok := drives.(map[string]any); ok {
		list = append(list, List(m["previous"])...)
		if Truthy(m["current"]) {
			list = append(list, m["current"])
		}
	} else if l, ok := drives.([]any); ok {
		list = l
	}
	for _, dr := range list {
		add(Map(dr))
	}
	return hs
}

// Copy returns a deep copy of decoded ESPN JSON (numbers stay json.Number).
func Copy(d Data) (Data, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return Decode(b)
}

// Reveal returns a copy of d with only the first k plays (in ExtractPlays
// order), as if the game were still being played. Unless finalNow, a final
// game is reported as in progress. It is for replaying a finished feed
// (mock mode, tests); live code never calls it.
func Reveal(d Data, k int, finalNow bool) (Data, error) {
	c, err := Copy(d)
	if err != nil {
		return nil, err
	}
	remaining := k
	for _, h := range PlayHolders(c) {
		l := h["plays"].([]any)
		keep := min(len(l), remaining)
		h["plays"] = l[:keep]
		remaining -= keep
	}
	if !finalNow && IsFinal(c) {
		status := Status(c)
		typ := Map(status["type"])
		typ["name"] = "STATUS_IN_PROGRESS"
		typ["shortDetail"] = "In Progress"
		typ["state"] = "in"
		status["type"] = typ
	}
	return c, nil
}
