package hostx

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

var (
	ErrUnsupportedPlatform     = errors.New("qualified host installation is unsupported on this platform")
	ErrAttendedInstallRequired = errors.New("host installation requires the qualified attended Darwin installer")
)

// FSInspector, GroupDB, ProcessInventory, PrivilegeRunner, and Clock are the
// explicit seams used by the Darwin installer. They make policy testable with
// fakes and prevent common control-plane code from directly owning privilege.
type FSInspector interface {
	Lstat(string) (os.FileInfo, error)
	ReadFile(string) ([]byte, error)
}

type GroupDB interface {
	LookupGroup(string) (Group, error)
	LookupOperator(int) (Operator, error)
	IsMember(Operator, Group) (bool, error)
}

type ProcessInventory interface {
	EffectiveGroups() ([]int, error)
	HasConsumer(context.Context, string) (bool, error)
}

type PrivilegeRunner interface {
	Run(context.Context, execx.Command) (execx.Result, error)
}

type Clock interface{ Now() time.Time }
