//go:build n1candidate && n1diagnostic

package hostx

// Fail compilation rather than select either privileged artifact ambiguously.
var _ = N1CandidateAndN1DiagnosticBuildTagsAreMutuallyExclusive
