package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

func newSessionID() (string, error) {
	return newID(rand.Reader)
}

func newID(source io.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("read session identity randomness: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
