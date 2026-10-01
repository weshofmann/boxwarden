package architecture

import "testing"

func compositionSource(tag string) []byte {
	return []byte(tag + "\n\npackage fixture\nimport _ \"github.com/weshofmann/boxwarden/internal/qualification/n1/contract\"\n")
}
func TestQualificationCompositionRequiresExactPathAndProof(t *testing.T) {
	user := "//go:build darwin && cgo && n1diagnostic && !n1candidate"
	helper := "//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate"
	for _, x := range []struct{ path, tag string }{{"cmd/n1-attend/main.go", user}, {"cmd/n1-attend-root/main.go", user}, {"cmd/n1-cleanup/main.go", helper}, {"internal/hostx/diagnostic_cleanup.go", "//go:build n1diagnostic && n1cleanup && !n1candidate && (darwin || linux)"}, {"internal/qualification/n1/proposal.go", ""}} {
		if !qualificationCompositionAllowed(x.path, compositionSource(x.tag)) {
			t.Fatal("exact admitted composition refused", x)
		}
	}
	for _, x := range []struct{ name, path, source string }{
		{"missing", "cmd/n1-attend/main.go", string(compositionSource(""))},
		{"malformed", "cmd/n1-attend/main.go", string(compositionSource("//go:build n1diagnostic && ("))},
		{"multiple", "cmd/n1-attend/main.go", string(compositionSource(user + "\n" + user))},
		{"late", "cmd/n1-attend/main.go", "package fixture\n" + user + "\nimport _ \"github.com/weshofmann/boxwarden/internal/qualification/n1/contract\"\n"},
		{"OR-broadened", "cmd/n1-attend/main.go", string(compositionSource("//go:build (n1diagnostic && !n1candidate) || linux"))},
		{"candidate-broadened", "cmd/n1-attend/main.go", string(compositionSource("//go:build n1diagnostic"))},
		{"missing-cleanup", "cmd/n1-cleanup/main.go", string(compositionSource(user))},
		{"host-missing-cleanup", "internal/hostx/diagnostic_cleanup.go", string(compositionSource(user))},
		{"wrong-sibling", "cmd/n1-attend/extra.go", string(compositionSource(user))},
		{"wrong-directory", "cmd/n1-attend-evil/main.go", string(compositionSource(user))},
		{"untagged-production", "internal/session/ordinary.go", string(compositionSource(""))},
		{"tagged-production", "internal/session/ordinary.go", string(compositionSource(user))},
		{"too-many-tags", "cmd/n1-attend/main.go", string(compositionSource(user + " && a && b && c && d && e && f && g && h && i"))},
		{"legacy-zero-space", "cmd/n1-attend/main.go", string(compositionSource(user + "\n//+build n1diagnostic,!n1candidate"))},
		{"legacy-multiple-space", "cmd/n1-attend/main.go", string(compositionSource(user + "\n//  +build n1diagnostic,!n1candidate"))},
		{"legacy-tab", "cmd/n1-attend/main.go", string(compositionSource(user + "\n//\t+build n1diagnostic,!n1candidate"))},
		{"legacy-multiple", "cmd/n1-attend/main.go", string(compositionSource(user + "\n// +build linux"))},
	} {
		t.Run(x.name, func(t *testing.T) {
			if qualificationCompositionAllowed(x.path, []byte(x.source)) {
				t.Fatal("unproved qualification import admitted", x.name)
			}
		})
	}
}
