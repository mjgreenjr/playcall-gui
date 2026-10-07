package core

import (
	"bytes"
	"encoding/json"
)

// marshalIndent encodes v like Python's json.dump(indent=2): two-space
// indent, no trailing newline, no HTML escaping. Map keys come out sorted.
// (Python also escapes non-ASCII as \uXXXX by default; Go writes UTF-8. Both
// parse to the same value.)
func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
