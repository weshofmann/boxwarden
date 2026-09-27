package session

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/recipe"
)

const maxStoredRecipeIntentBytes = 1 << 20

// PublishRecipeIntent durably stores the complete canonical recipe before a
// session can reference it. A digest name is never replaced, including when
// an existing object is corrupt. A concurrent exact publication is harmless.
func PublishRecipeIntent(stateRoot string, value recipe.Recipe) (string, error) {
	raw, digest, err := recipe.CanonicalIntent(value)
	if err != nil {
		return "", err
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return "", fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	intents, err := openSessionChild(root, "recipe-intents", true)
	if err != nil {
		return "", fmt.Errorf("recipe intent directory: %w", err)
	}
	defer intents.Close()
	target := digest + ".json"
	if existing, err := readRecipeIntent(intents, digest); err == nil {
		if !bytes.Equal(existing, raw) {
			return "", fmt.Errorf("existing recipe intent differs from exact canonical bytes")
		}
		if err := sessionSyncRoot(intents); err != nil {
			return "", fmt.Errorf("sync existing recipe intent directory: %w", err)
		}
		return digest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	temporaryName, err := sessionTemporaryName(target)
	if err != nil {
		return "", err
	}
	temporary, err := intents.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", fmt.Errorf("create recipe intent staging file: %w", err)
	}
	defer intents.Remove(temporaryName)
	n, writeErr := temporary.Write(raw)
	if writeErr != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write recipe intent: %w", writeErr)
	}
	if n != len(raw) {
		_ = temporary.Close()
		return "", fmt.Errorf("write recipe intent: %w", io.ErrShortWrite)
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return "", fmt.Errorf("sync recipe intent staging file: %w", err)
	}
	if err := intents.Link(temporaryName, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("publish recipe intent: %w", err)
		}
		concurrent, readErr := readRecipeIntent(intents, digest)
		if readErr != nil {
			return "", fmt.Errorf("read concurrent recipe intent: %w", readErr)
		}
		if !bytes.Equal(concurrent, raw) {
			return "", fmt.Errorf("concurrent recipe intent differs from exact canonical bytes")
		}
	}
	if err := intents.Remove(temporaryName); err != nil {
		return "", fmt.Errorf("remove recipe intent staging link: %w", err)
	}
	if err := sessionSyncRoot(intents); err != nil {
		return "", fmt.Errorf("sync recipe intent directory: %w", err)
	}
	return digest, nil
}

// LoadRecipeIntent rechecks a private stored object's exact digest and type.
// A missing or corrupt binding must block later session execution.
func LoadRecipeIntent(stateRoot, digest string) ([]byte, error) {
	if !lowerSHA256(digest) {
		return nil, fmt.Errorf("invalid recipe intent digest")
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	intents, err := openSessionChild(root, "recipe-intents", false)
	if err != nil {
		return nil, err
	}
	defer intents.Close()
	return readRecipeIntent(intents, digest)
}

func readRecipeIntent(intents *os.Root, digest string) ([]byte, error) {
	file, err := openSessionPrivateRegular(intents, digest+".json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxStoredRecipeIntentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read recipe intent: %w", err)
	}
	if len(raw) > maxStoredRecipeIntentBytes {
		return nil, fmt.Errorf("recipe intent exceeds bound")
	}
	actual := sha256.Sum256(raw)
	if fmt.Sprintf("%x", actual[:]) != digest {
		return nil, fmt.Errorf("recipe intent digest mismatch")
	}
	if _, err := recipe.DecodeIntent(raw); err != nil {
		return nil, fmt.Errorf("invalid stored recipe intent: %w", err)
	}
	return raw, nil
}
