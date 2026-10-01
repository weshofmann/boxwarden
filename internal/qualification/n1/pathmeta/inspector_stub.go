//go:build !darwin

package pathmeta

import "errors"

type OSInspector struct{}

func (OSInspector) HasExtendedACL(string) (bool, error) {
	return false, errors.New("n1 native ACL unavailable")
}
