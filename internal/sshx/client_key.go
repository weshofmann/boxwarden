package sshx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// EnsureClientKey creates one generation-local Ed25519 identity and verifies
// the exact private/public pair on every reuse. A partial pair is never
// repaired in place; the owning generation must be reconciled first.
func EnsureClientKey(ctx context.Context, runner Runner, runtimeDirectory string) (string, error) {
	if runner == nil {
		return "", fmt.Errorf("client-key runner is required")
	}
	if err := requirePrivateTree(runtimeDirectory, runtimeDirectory); err != nil {
		return "", fmt.Errorf("private generation: %w", err)
	}
	key := filepath.Join(runtimeDirectory, "client")
	public := key + ".pub"
	keyExists, err := fileExists(key)
	if err != nil {
		return "", err
	}
	publicExists, err := fileExists(public)
	if err != nil {
		return "", err
	}
	if keyExists != publicExists {
		return "", fmt.Errorf("generation client key pair is partial")
	}
	if !keyExists {
		operationCtx, cancel := boundedCAContext(ctx)
		defer cancel()
		result, err := runner.Run(operationCtx, Command{Path: "/usr/bin/ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-f", key}})
		if err != nil || result.Truncated || result.Stdout != "" || result.Stderr != "" {
			return "", fmt.Errorf("generate generation client key: %w", errOrInvalid(err))
		}
		if err := normalizePublicFile(public); err != nil {
			return "", fmt.Errorf("normalize generated public key: %w", err)
		}
	}
	if _, err := requireRuntimeFile(runtimeDirectory, key, privateFileMode); err != nil {
		return "", fmt.Errorf("private generation client key: %w", err)
	}
	publicBytes, err := readRuntimeFile(runtimeDirectory, public, publicFileMode)
	if err != nil {
		return "", fmt.Errorf("public generation client key: %w", err)
	}
	publicKey, _, _, err := parseEd25519PublicKey(string(publicBytes))
	if err != nil {
		return "", fmt.Errorf("public generation client key: %w", err)
	}
	operationCtx, cancel := boundedCAContext(ctx)
	defer cancel()
	derived, err := runner.Run(operationCtx, Command{Path: "/usr/bin/ssh-keygen", Args: []string{"-y", "-f", key}})
	if err != nil || derived.Truncated || derived.Stderr != "" {
		return "", fmt.Errorf("derive generation client public key: %w", errOrInvalid(err))
	}
	derivedKey, _, _, err := parseEd25519PublicKey(derived.Stdout)
	if err != nil || derivedKey != publicKey {
		return "", fmt.Errorf("generation client key pair does not match")
	}
	return key, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func errOrInvalid(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected command output")
}
