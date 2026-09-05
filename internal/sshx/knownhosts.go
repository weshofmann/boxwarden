package sshx

import (
	"fmt"
	"os"
	"path/filepath"
)

func HostKeyAlias(sessionID string) string { return "boxwarden-session-" + sessionID }

// WriteKnownHosts materializes the one exact pin for a private supervisor generation directory.
func WriteKnownHosts(runtimeDirectory string, pin HostKeyPin) (string, error) {
	if !validUUID(pin.SessionID) || pin.Version != hostKeyPinVersion || pin.Algorithm != "ssh-ed25519" {
		return "", fmt.Errorf("invalid host-key pin")
	}
	public, _, fingerprint, err := parseEd25519PublicKey(pin.PublicKey)
	if err != nil || public != pin.PublicKey || fingerprint != pin.Fingerprint {
		return "", fmt.Errorf("invalid pinned public key")
	}
	if err := requirePrivateTree(runtimeDirectory, runtimeDirectory); err != nil {
		return "", err
	}
	path := filepath.Join(runtimeDirectory, "known_hosts")
	contents := []byte(HostKeyAlias(pin.SessionID) + " " + pin.PublicKey + "\n")
	if _, err := os.Lstat(path); err == nil {
		existing, err := readPrivateFile(path)
		if err != nil {
			return "", err
		}
		if string(existing) != string(contents) {
			return "", fmt.Errorf("known-hosts file already differs")
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := writePrivateNew(path, contents); err != nil {
		return "", err
	}
	return path, nil
}
