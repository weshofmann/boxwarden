package session

import (
	"bytes"
	"testing"
)

func TestNewIDProducesRFC4122Version4IdentityFromTrustedRandomBytes(t *testing.T) {
	random := bytes.NewReader([]byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	})

	got, err := newID(random)
	if err != nil {
		t.Fatalf("newID() error = %v", err)
	}
	const want = "00112233-4455-4677-8899-aabbccddeeff"
	if got != want {
		t.Fatalf("newID() = %q, want %q", got, want)
	}
	if !validUUID(got) {
		t.Fatalf("newID() = %q, want valid session UUID", got)
	}
}

func TestNewIDFailsClosedWhenRandomSourceIsShort(t *testing.T) {
	if _, err := newID(bytes.NewReader([]byte{0x01})); err == nil {
		t.Fatal("newID(short source) error = nil, want failure")
	}
}
