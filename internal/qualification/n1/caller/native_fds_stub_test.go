//go:build darwin && !cgo

package caller

import "testing"

func TestCallerNoCGODescriptorContainmentRefuses(t *testing.T) {
	if _, _, e := nativeDescriptorNames(4); e == nil {
		t.Fatal("unchecked native-free reader admitted")
	}
}
