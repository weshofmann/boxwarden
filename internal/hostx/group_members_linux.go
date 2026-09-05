//go:build linux

package hostx

import (
	"fmt"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func inspectExactLocalOperatorGroup(_ execx.Runner, _ Operator, _ string, _ bool) (Group, error) {
	return Group{}, fmt.Errorf("exact local Directory Service group inspection is unavailable off Darwin")
}
