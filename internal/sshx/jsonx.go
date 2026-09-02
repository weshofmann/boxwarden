package sshx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeExactObject rejects every ambiguous JSON representation: duplicate and
// unknown fields, trailing values, and objects that omit an expected field.
func decodeExactObject(contents []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if len(contents) > maxStateFileBytes {
		return nil, fmt.Errorf("JSON exceeds %d bytes", maxStateFileBytes)
	}
	expected := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		expected[name] = struct{}{}
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("JSON must be one object")
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read JSON field: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("JSON object field is not a string")
		}
		if _, ok := expected[name]; !ok {
			return nil, fmt.Errorf("unknown JSON field %q", name)
		}
		if _, ok := fields[name]; ok {
			return nil, fmt.Errorf("duplicate JSON field %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("JSON field %q: %w", name, err)
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, fmt.Errorf("JSON object is incomplete")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON has trailing data")
	}
	for name := range expected {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("missing JSON field %q", name)
		}
	}
	return fields, nil
}

func decodeField(fields map[string]json.RawMessage, name string, output any) error {
	if err := json.Unmarshal(fields[name], output); err != nil {
		return fmt.Errorf("JSON field %q: %w", name, err)
	}
	return nil
}
