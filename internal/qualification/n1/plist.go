package n1

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
)

// Bounded system metadata decoder. It is not a command or receipt parser;
// duplicate dictionary keys and all unsupported value types refuse.
func parsePlist(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 2<<20 {
		return nil, ErrRefused
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	nodes := 0
	var value func(xml.StartElement, int) (any, error)
	next := func() (xml.Token, error) {
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch t.(type) {
			case xml.CharData:
				if len(bytes.TrimSpace(t.(xml.CharData))) != 0 {
					return nil, ErrRefused
				}
				continue
			case xml.Comment, xml.ProcInst, xml.Directive:
				continue
			}
			return t, nil
		}
	}
	value = func(start xml.StartElement, depth int) (any, error) {
		nodes++
		if depth > 16 || nodes > 16384 || len(start.Attr) != 0 {
			return nil, ErrRefused
		}
		switch start.Name.Local {
		case "dict":
			m := map[string]any{}
			for {
				t, e := next()
				if e != nil {
					return nil, ErrRefused
				}
				if end, ok := t.(xml.EndElement); ok {
					if end.Name != start.Name {
						return nil, ErrRefused
					}
					return m, nil
				}
				key, ok := t.(xml.StartElement)
				if !ok || key.Name.Local != "key" || len(key.Attr) != 0 {
					return nil, ErrRefused
				}
				var k string
				if d.DecodeElement(&k, &key) != nil || len(k) > 256 || k == "" {
					return nil, ErrRefused
				}
				if _, ok = m[k]; ok {
					return nil, ErrRefused
				}
				t, e = next()
				child, ok := t.(xml.StartElement)
				if e != nil || !ok {
					return nil, ErrRefused
				}
				v, e := value(child, depth+1)
				if e != nil {
					return nil, e
				}
				m[k] = v
				if len(m) > 256 {
					return nil, ErrRefused
				}
			}
		case "array":
			a := []any{}
			for {
				t, e := next()
				if e != nil {
					return nil, ErrRefused
				}
				if end, ok := t.(xml.EndElement); ok {
					if end.Name != start.Name {
						return nil, ErrRefused
					}
					return a, nil
				}
				child, ok := t.(xml.StartElement)
				if !ok {
					return nil, ErrRefused
				}
				v, e := value(child, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
				if len(a) > 1024 {
					return nil, ErrRefused
				}
			}
		case "string", "integer", "data", "real", "date":
			var s string
			if d.DecodeElement(&s, &start) != nil || len(s) > 65536 {
				return nil, ErrRefused
			}
			if start.Name.Local == "integer" {
				n, e := strconv.ParseUint(s, 10, 64)
				if e != nil {
					return nil, ErrRefused
				}
				return n, nil
			}
			return s, nil
		case "true", "false":
			t, e := next()
			end, ok := t.(xml.EndElement)
			if e != nil || !ok || end.Name != start.Name {
				return nil, ErrRefused
			}
			return start.Name.Local == "true", nil
		}
		return nil, ErrRefused
	}
	t, e := next()
	start, ok := t.(xml.StartElement)
	if e != nil || !ok || start.Name.Local != "plist" || len(start.Attr) != 1 || start.Attr[0].Name.Local != "version" || start.Attr[0].Value != "1.0" {
		return nil, ErrRefused
	}
	t, e = next()
	dict, ok := t.(xml.StartElement)
	if e != nil || !ok || dict.Name.Local != "dict" {
		return nil, ErrRefused
	}
	v, e := value(dict, 0)
	if e != nil {
		return nil, e
	}
	t, e = next()
	end, ok := t.(xml.EndElement)
	if e != nil || !ok || end.Name != start.Name {
		return nil, ErrRefused
	}
	if _, e = next(); e != io.EOF {
		return nil, ErrRefused
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrRefused
	}
	return m, nil
}
func encryptedAssociation(d, h map[string]any) error {
	if d["VolumeUUID"] != "A178510A-D5EC-4495-828B-BD5445E2B66D" || d["MountPoint"] != "/Volumes/BoxwardenAlphaQualification" || d["FilesystemType"] != "apfs" {
		return ErrRefused
	}
	device, ok := d["DeviceIdentifier"].(string)
	if !ok || device == "" {
		return ErrRefused
	}
	stores, ok := d["APFSPhysicalStores"].([]any)
	if !ok || len(stores) != 1 {
		return ErrRefused
	}
	store, ok := stores[0].(map[string]any)
	if !ok {
		return ErrRefused
	}
	backing, ok := store["APFSPhysicalStore"].(string)
	if !ok || backing == "" {
		return ErrRefused
	}
	images, ok := h["images"].([]any)
	if !ok || len(images) > 64 {
		return ErrRefused
	}
	matched := 0
	for _, raw := range images {
		image, ok := raw.(map[string]any)
		if !ok {
			return ErrRefused
		}
		if image["image-path"] != "/Volumes/DevelData/boxwarden/alpha-qualification-state.sparsebundle" {
			continue
		}
		matched++
		if image["image-encrypted"] != true {
			return ErrRefused
		}
		entities, ok := image["system-entities"].([]any)
		if !ok || len(entities) > 16 {
			return ErrRefused
		}
		dev, physical := false, false
		for _, raw := range entities {
			entity, ok := raw.(map[string]any)
			if !ok {
				return ErrRefused
			}
			if entity["dev-entry"] == "/dev/"+device {
				dev = true
			}
			if entity["dev-entry"] == "/dev/"+backing {
				physical = true
			}
		}
		if !dev || !physical {
			return ErrRefused
		}
	}
	if matched != 1 {
		return ErrRefused
	}
	return nil
}
