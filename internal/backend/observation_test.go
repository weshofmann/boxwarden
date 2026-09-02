package backend

import "testing"

func TestObjectStateValidationRejectsUnknownValues(t *testing.T) {
	for _, state := range []ObjectState{ObjectRunning, ObjectStopped, ObjectUnknown} {
		if !state.Valid() {
			t.Fatalf("%q.Valid() = false, want true", state)
		}
	}
	if ObjectState("paused").Valid() {
		t.Fatal("paused.Valid() = true, want false")
	}
}
