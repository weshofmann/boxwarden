package sshx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// OSIdentity resolves the exact effective trusted-host operator for CA metadata revalidation.
type OSIdentity struct{}

func (OSIdentity) Current(context.Context) (Operator, error) {
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return Operator{}, fmt.Errorf("resolve current operator: %w", err)
	}
	uid, err := strconv.Atoi(current.Uid)
	if err != nil || uid != os.Geteuid() {
		return Operator{}, fmt.Errorf("current operator UID does not match effective UID")
	}
	return Operator{UID: uid, Name: current.Username}, nil
}

// RandomUUID returns a crypto-random RFC4122 version-4 UUID for immutable CA creation metadata.
func RandomUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("read UUID randomness: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
