package closeout

import (
	"bytes"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

func doctorReturn(r fixed.ChildResult, e error) error {
	if e != nil || r.Exit != 0 || !r.Closed || !bytes.Equal(r.Raw, []byte("status: healthy\n")) {
		return ErrRefused
	}
	return nil
}
