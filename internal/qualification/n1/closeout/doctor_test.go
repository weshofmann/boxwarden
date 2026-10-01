package closeout

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorNeedsActualHealthyReturnAndEmptyArtifactOnlyState(t *testing.T) {
	for _, raw := range []string{"status: healthy\n", "", "status: healthy", "status: drifted\n", "status: healthy\nsecret"} {
		e := doctorReturn(fixed.ChildResult{Raw: []byte(raw), Exit: 0, Closed: true}, nil)
		if (raw == "status: healthy\n") != (e == nil) {
			t.Fatal("unproved healthy", raw, e)
		}
	}
	for _, r := range []fixed.ChildResult{{Raw: []byte("status: healthy\n"), Exit: 1, Closed: true}, {Raw: []byte("status: healthy\n"), Exit: 0, Closed: false}} {
		if doctorReturn(r, nil) == nil {
			t.Fatal("claimed healthy bypassed actual return")
		}
	}
	if doctorReturn(fixed.ChildResult{Raw: []byte("status: healthy\n"), Closed: true}, errors.New("late wait error")) == nil {
		t.Fatal("error bypass")
	}
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	if emptyDoctorState(t.Context(), p, os.Getuid(), noACL{}) != nil {
		t.Fatal("empty fixture refused")
	}
	if e = os.WriteFile(filepath.Join(p, "unexpected"), []byte("preserve"), 0600); e != nil {
		t.Fatal(e)
	}
	if emptyDoctorState(t.Context(), p, os.Getuid(), noACL{}) == nil {
		t.Fatal("nonempty doctor state accepted")
	}
}
