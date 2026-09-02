package hostx

import "errors"

var (
	errMissingConfiguredStateRoots     = errors.New("configured state-root collection is missing")
	errNoncanonicalConfiguredStateRoot = errors.New("configured state root is noncanonical")
	errHostPathOverlap                 = errors.New("host paths overlap")
)

// validateHostPathOverlaps enforces the host-global disjointness invariant for
// all configured state roots, the fixed production toolchain, and every pair
// of configured host input paths. It is pure so doctor and init cannot drift.
func validateHostPathOverlaps(request Request) error {
	if len(request.ConfiguredStateRoots) == 0 {
		return errMissingConfiguredStateRoots
	}
	hostPaths := []string{request.TartPath, request.TartHome, request.SoftnetPath, productionToolchainPath()}
	for _, root := range request.ConfiguredStateRoots {
		if !canonicalAbsolute(root) {
			return errNoncanonicalConfiguredStateRoot
		}
		for _, path := range hostPaths {
			if canonicalAbsolute(path) && pathsOverlap(root, path) {
				return errHostPathOverlap
			}
		}
	}
	for _, pair := range [][2]string{
		{request.TartPath, request.TartHome},
		{request.TartPath, request.SoftnetPath},
		{request.TartHome, request.SoftnetPath},
	} {
		if canonicalAbsolute(pair[0]) && canonicalAbsolute(pair[1]) && pathsOverlap(pair[0], pair[1]) {
			return errHostPathOverlap
		}
	}
	return nil
}
