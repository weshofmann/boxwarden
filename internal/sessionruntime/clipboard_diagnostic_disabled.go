//go:build !n1clipboarddiagnostic || n1candidate

package sessionruntime

import "context"

func clipboardStage(context.Context, string, string)                              {}
func clipboardDiagnosticAdmission(context.Context, clipboardRuntime, string) bool { return true }
