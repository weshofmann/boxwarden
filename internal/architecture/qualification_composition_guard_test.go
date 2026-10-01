package architecture

import (
	"strings"
	"testing"
)

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

func TestSlice2ExactActorConstraints(t *testing.T) {
	both := "//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate"
	stock := "//go:build darwin && cgo && n1clipboarddiagnostic && !n1diagnostic && !n1candidate"
	for _, p := range []string{"cmd/n1-window/main.go", "cmd/n1-run-window/main.go", "cmd/n1-closeout/main.go", "cmd/n1-stock-worker/main.go", "cmd/n1-candidate-worker/main.go"} {
		t.Run(p, func(t *testing.T) {
			tag := both
			if p == "cmd/n1-stock-worker/main.go" {
				tag = stock
			}
			if !qualificationCompositionAllowed(p, compositionSource(tag)) {
				t.Fatal("exact actor refused")
			}
			for name, source := range map[string][]byte{"missing": compositionSource(""), "OR": compositionSource(tag + " || linux"), "late": []byte("package fixture\n" + tag + "\n"), "legacy": compositionSource(tag + "\n// +build linux"), "clipboard-absent": compositionSource("//go:build n1diagnostic && !n1candidate"), "candidate": compositionSource(tag + " || n1candidate"), "wrong-role": compositionSource(strings.ReplaceAll(tag, "!n1diagnostic", "n1diagnostic"))} {
				if name == "wrong-role" && p != "cmd/n1-stock-worker/main.go" {
					source = compositionSource(stock)
				}
				if qualificationCompositionAllowed(p, source) {
					t.Fatal("unproved actor admitted", name)
				}
			}
			sibling := strings.TrimSuffix(p, "main.go") + "sibling.go"
			moved := "cmd/moved-" + strings.TrimPrefix(p, "cmd/")
			if qualificationCompositionAllowed(sibling, compositionSource(tag)) || qualificationCompositionAllowed(moved, compositionSource(tag)) {
				t.Fatal("moved/sibling actor granted exemption")
			}
		})
	}
	for _, p := range []string{"cmd/n1-stock-worker/main.go", "cmd/n1-candidate-worker/main.go"} {
		tag := both
		if p == "cmd/n1-stock-worker/main.go" {
			tag = stock
		}
		if !tartQualificationCompositionAllowed(p, compositionSource(tag)) {
			t.Fatal("exact worker Tart composition refused", p)
		}
	}
	for _, p := range []string{"cmd/n1-window/main.go", "cmd/n1-run-window/main.go", "cmd/n1-closeout/main.go", "cmd/n1-stock-worker/other.go", "internal/qualification/n1/worker/main.go"} {
		if tartQualificationCompositionAllowed(p, compositionSource(both)) {
			t.Fatal("Tart factory broadened", p)
		}
	}
}
