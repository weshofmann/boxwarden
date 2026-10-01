package n1

import (
	"strings"
	"testing"
)

func TestBoundedEncryptedAssociationNoInnerEncryptionAssumption(t *testing.T) {
	d := map[string]any{"VolumeUUID": "A178510A-D5EC-4495-828B-BD5445E2B66D", "MountPoint": "/Volumes/BoxwardenAlphaQualification", "FilesystemType": "apfs", "DeviceIdentifier": "disk9s1", "APFSPhysicalStores": []any{map[string]any{"APFSPhysicalStore": "disk8s2"}}, "Encrypted": false}
	h := map[string]any{"images": []any{map[string]any{"image-path": "/Volumes/DevelData/boxwarden/alpha-qualification-state.sparsebundle", "image-encrypted": true, "system-entities": []any{map[string]any{"dev-entry": "/dev/disk9s1"}, map[string]any{"dev-entry": "/dev/disk8s2"}}}}}
	if encryptedAssociation(d, h) != nil {
		t.Fatal("correct encrypted backing refused")
	}
	image := h["images"].([]any)[0].(map[string]any)
	for _, which := range []string{"unencrypted", "foreign-path", "wrong-device", "duplicate"} {
		oldPath, oldEncrypted, oldEntities := image["image-path"], image["image-encrypted"], image["system-entities"]
		oldImages := h["images"]
		switch which {
		case "unencrypted":
			image["image-encrypted"] = false
		case "foreign-path":
			image["image-path"] = "foreign"
		case "wrong-device":
			image["system-entities"] = []any{map[string]any{"dev-entry": "/dev/other"}}
		case "duplicate":
			h["images"] = []any{image, image}
		}
		if encryptedAssociation(d, h) == nil {
			t.Fatal("unknown storage association accepted", which)
		}
		image["image-path"], image["image-encrypted"], image["system-entities"] = oldPath, oldEncrypted, oldEntities
		h["images"] = oldImages
	}
}
func TestPlistStrictDuplicateMalformedAndBounds(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>flag</key><true/><key>array</key><array><string>nonsecret</string><integer>42</integer></array></dict></plist>`)
	m, e := parsePlist(raw)
	if e != nil || m["flag"] != true {
		t.Fatal(m, e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `<key>flag</key><true/>`, `<key>flag</key><true/><key>flag</key><false/>`, 1), string(raw) + `<dict/>`, strings.Replace(string(raw), `<true/>`, `<unknown/>`, 1), strings.Repeat(" ", 2<<20+1)} {
		if _, e := parsePlist([]byte(bad)); e == nil {
			t.Fatal("malformed metadata admitted")
		}
	}
}
