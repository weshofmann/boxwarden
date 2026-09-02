package sshx

import "testing"

func TestRandomUUIDIsRFC4122Version4(t *testing.T) {
	value, err := RandomUUID()
	if err != nil || !validUUID(value) || value[14] != '4' || value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b' {
		t.Fatalf("RandomUUID() = %q, %v", value, err)
	}
}
