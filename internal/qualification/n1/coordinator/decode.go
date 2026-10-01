//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Worker output is canonical Go JSON and includes fixed arrays, flattened
// observer fields and finite counter maps. Bound/scan before typed allocation;
// exact re-encoding rejects missing/null scalars, alternate numbers and shapes.
func decode(raw []byte, v any, limit int) error {
	if len(raw) < 2 || len(raw) > limit || raw[len(raw)-1] != '\n' {
		return ErrRefused
	}
	body := raw[:len(raw)-1]
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	nodes := 0
	var walk func(int) error
	walk = func(depth int) error {
		nodes++
		if depth > 16 || nodes > 8192 {
			return ErrRefused
		}
		t, e := d.Token()
		if e != nil {
			return ErrRefused
		}
		switch x := t.(type) {
		case string:
			if len(x) > 512 {
				return ErrRefused
			}
		case json.Number:
			if len(x) > 20 || strings.ContainsAny(string(x), ".eE") {
				return ErrRefused
			}
		case json.Delim:
			if x != '{' && x != '[' {
				return ErrRefused
			}
			seen := map[string]bool{}
			count := 0
			for d.More() {
				count++
				if count > 256 {
					return ErrRefused
				}
				if x == '{' {
					k, e := d.Token()
					s, ok := k.(string)
					if e != nil || !ok || len(s) > 64 || seen[s] {
						return ErrRefused
					}
					seen[s] = true
				}
				if walk(depth+1) != nil {
					return ErrRefused
				}
			}
			end, e := d.Token()
			if e != nil || x == '{' && end != json.Delim('}') || x == '[' && end != json.Delim(']') {
				return ErrRefused
			}
		}
		return nil
	}
	if walk(0) != nil {
		return ErrRefused
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrRefused
	}
	d = json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrRefused
	}
	want, e := json.Marshal(v)
	if e != nil || !bytes.Equal(want, body) {
		return ErrRefused
	}
	return nil
}
