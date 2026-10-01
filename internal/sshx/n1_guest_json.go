//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
)

// This finite transport decoder supports the observer's exact flattened header
// and integer counter object. It does not accept optional/missing struct fields.
func n1Decode(raw []byte, result any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return ErrN1Guest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if n1ScanJSON(d, 0) != nil {
		return ErrN1Guest
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrN1Guest
	}
	t := reflect.TypeOf(result)
	if t == nil || t.Kind() != reflect.Pointer || n1ShapeJSON(raw, t.Elem()) != nil {
		return ErrN1Guest
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(result) != nil {
		return ErrN1Guest
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrN1Guest
	}
	return nil
}
func n1ScanJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrN1Guest
	}
	token, err := d.Token()
	if err != nil {
		return ErrN1Guest
	}
	if number, ok := token.(json.Number); ok {
		if len(number.String()) > 20 || strings.ContainsAny(number.String(), ".eE") {
			return ErrN1Guest
		}
		return nil
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok || seen[key] {
				return ErrN1Guest
			}
			seen[key] = true
			if n1ScanJSON(d, depth+1) != nil {
				return ErrN1Guest
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return ErrN1Guest
		}
	case '[':
		for d.More() {
			if n1ScanJSON(d, depth+1) != nil {
				return ErrN1Guest
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return ErrN1Guest
		}
	default:
		return ErrN1Guest
	}
	return nil
}
func n1JSONFields(t reflect.Type, fields map[string]reflect.Type) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			if n1JSONFields(field.Type, fields) != nil {
				return ErrN1Guest
			}
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			return ErrN1Guest
		}
		if _, exists := fields[name]; exists {
			return ErrN1Guest
		}
		fields[name] = field.Type
	}
	return nil
}
func n1ShapeJSON(raw []byte, t reflect.Type) error {
	raw = bytes.TrimSpace(raw)
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(raw, []byte("null")) {
			return nil
		}
		return n1ShapeJSON(raw, t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return ErrN1Guest
		}
		fields := map[string]reflect.Type{}
		if n1JSONFields(t, fields) != nil || len(fields) != len(object) {
			return ErrN1Guest
		}
		for name, typ := range fields {
			value, ok := object[name]
			if !ok || n1ShapeJSON(value, typ) != nil {
				return ErrN1Guest
			}
		}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return ErrN1Guest
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil || len(object) > 64 {
			return ErrN1Guest
		}
		for _, value := range object {
			if n1ShapeJSON(value, t.Elem()) != nil {
				return ErrN1Guest
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 64 {
			return ErrN1Guest
		}
		for _, value := range values {
			if n1ShapeJSON(value, t.Elem()) != nil {
				return ErrN1Guest
			}
		}
	case reflect.Bool:
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return ErrN1Guest
		}
	case reflect.String:
		if len(raw) == 0 || raw[0] != '"' {
			return ErrN1Guest
		}
	case reflect.Int, reflect.Int64, reflect.Uint32:
		if len(raw) == 0 || raw[0] == 'n' || bytes.ContainsAny(raw, ".eE") {
			return ErrN1Guest
		}
	default:
		return ErrN1Guest
	}
	return nil
}
