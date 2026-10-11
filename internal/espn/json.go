// Package espn builds ESPN URLs, fetches JSON and reads ESPN's loosely typed
// responses.
//
// Decoding strategy: ESPN responses are decoded into generic values
// (map[string]any, []any, string, bool, json.Number, nil) instead of structs,
// because ids, scores and clocks are strings in some places and numbers in
// others, and the per-sport rules read many optional fields. Numbers decode as
// json.Number, which keeps the original text, so play ids wider than 2^53
// (MLB ids reach 4019080151799990058) are never rounded through float64. Use
// the accessors below (Map, List, Str, Int64, Truthy) instead of type
// assertions; they treat a missing or wrongly typed value as empty.
package espn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Data is a decoded ESPN JSON object (a summary, scoreboard event, play, ...).
type Data = map[string]any

// Decode parses a JSON object with numbers kept as json.Number. Anything that
// is not a single JSON object is an error.
func Decode(b []byte) (Data, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var d Data
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("espn: JSON value is not an object")
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("espn: trailing data after JSON object")
	}
	return d, nil
}

// Map returns v as an object, or nil if it is not one. Reading a nil map is safe.
func Map(v any) Data {
	m, _ := v.(map[string]any)
	return m
}

// List returns v as an array, or nil if it is not one.
func List(v any) []any {
	l, _ := v.([]any)
	return l
}

// Truthy mirrors Python truthiness for decoded JSON: null, false, 0, "",
// empty arrays and empty objects are falsy. It is how the Python's
// `a or b` fallbacks are reproduced.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// Str returns v as a string: strings as is, numbers as their JSON text,
// anything else "".
func Str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

// Int64 returns v as an integer when it is an integer number or a string of
// digits (ESPN sends play ids both ways).
func Int64(v any) (int64, bool) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case json.Number:
		s = x.String()
	default:
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// Float returns v as a number: a JSON number or a string holding one.
func Float(v any) (float64, bool) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case json.Number:
		s = x.String()
	case float64:
		return x, true
	default:
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil
}
