package clipboardhost

import (
	"strings"
	"testing"
)

func TestPrivateBoardNameNeverSelectsGeneral(t *testing.T) {
	for _, name := range []string{"", "general", "org.boxwarden.test.", "org.boxwarden.test.fake\n", "org.boxwarden.test.fake\x00", "org.boxwarden.test.fake☃", "org.boxwarden.test." + strings.Repeat("x", 256)} {
		if board, err := NewPrivate(name); err == nil || board != nil {
			t.Fatalf("unsafe private board %q: %#v %v", name, board, err)
		}
	}
	for _, name := range []string{"org.boxwarden.test.native-123", "org.boxwarden.test.GUI_1.20261003"} {
		if board, err := NewPrivate(name); err != nil || board == nil {
			t.Fatalf("valid private board %q: %v", name, err)
		}
	}
	if !validBoardName("") {
		t.Fatal("general helper default changed")
	}
}
