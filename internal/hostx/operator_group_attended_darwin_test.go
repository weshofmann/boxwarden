//go:build darwin

package hostx

import (
	"os"
	"testing"
)

// TestAttendedExactLocalOperatorGroupState records the read-only directory
// evidence that gates the one-time manifest migration. It deliberately uses
// the real UID so it cannot validate an effective root identity instead of
// the operator that will run the old published init.
func TestAttendedExactLocalOperatorGroupState(t *testing.T) {
	if os.Getenv("BOXWARDEN_ATTENDED_EXACT_GROUP") != "1" {
		t.Skip("set BOXWARDEN_ATTENDED_EXACT_GROUP=1 to inspect the real local operator group")
	}

	inspector := NewOSDoctorInspector()
	operator, err := inspector.LookupOperator(os.Getuid())
	if err != nil {
		t.Fatalf("LookupOperator(real UID): %v", err)
	}
	group, err := inspector.ExactOperatorGroup(operator, OperatorGroupName)
	if err != nil {
		t.Fatalf("ExactOperatorGroup(): %v", err)
	}
	if group.Name != OperatorGroupName {
		t.Fatalf("group name = %q, want %q", group.Name, OperatorGroupName)
	}
	if group.ID < 0 {
		t.Fatalf("group GID = %d, want nonnegative", group.ID)
	}
	if len(group.Members) != 1 || group.Members[0] != operator.UID {
		t.Fatalf("group numeric members = %v, want [%d]", group.Members, operator.UID)
	}

	t.Logf("operator_uid=%d group_gid=%d numeric_members=[%d]", operator.UID, group.ID, group.Members[0])
}
