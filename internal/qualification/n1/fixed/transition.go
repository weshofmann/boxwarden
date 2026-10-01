package fixed

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"syscall"
	"time"
)

func ReadTransition(deadline time.Time) (contract.Witness, error) {
	var stat syscall.Stat_t
	if syscall.Fstat(3, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		return contract.Witness{}, ErrRefused
	}
	return readTransition(os.NewFile(3, "n1-transition"), deadline, func(f *os.File) error { return f.Close() })
}
func readTransition(f *os.File, deadline time.Time, closeFile func(*os.File) error) (contract.Witness, error) {
	if f == nil || !time.Now().Before(deadline) {
		if f != nil {
			closeFile(f)
		}
		return contract.Witness{}, ErrRefused
	}
	timer := time.AfterFunc(time.Until(deadline), func() { closeFile(f) })
	raw, re := io.ReadAll(io.LimitReader(f, contract.MaxWitnessBytes+1))
	ce := closeFile(f)
	timer.Stop()
	if re != nil || ce != nil || len(raw) > contract.MaxWitnessBytes || !time.Now().Before(deadline) {
		return contract.Witness{}, ErrRefused
	}
	w, e := contract.ParseWitness(raw)
	if e != nil || w.Phase != 1 {
		return contract.Witness{}, ErrRefused
	}
	return w, nil
}
