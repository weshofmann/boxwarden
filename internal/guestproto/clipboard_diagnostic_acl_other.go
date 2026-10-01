//go:build n1clipboarddiagnostic && !linux

package guestproto

import "github.com/weshofmann/boxwarden/internal/clipboarddiag"

// The fixed trial helper is a Linux/arm64 artifact. Other platforms can exercise
// its pure framing/transport/publisher fixtures, never admit the guest namespace.
func diagnosticNoACL(string, bool) error { return clipboarddiag.ErrMetadata }
