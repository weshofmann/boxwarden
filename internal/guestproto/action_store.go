package guestproto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrActionIndeterminate = errors.New("action was claimed without a completed receipt")

type actionClaim struct {
	Version       int    `json:"version"`
	RequestSHA256 string `json:"request_sha256"`
}

// ClaimAction durably claims exact request bytes before any guest process may
// run. A repeated claim without a receipt is indeterminate and never grants
// permission to execute again. A completed exact receipt may be read back.
func (b *Bootstrapper) ClaimAction(request ActionRequest) (*ActionReceipt, error) {
	store, err := b.openActionStore(request, true)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	claim, err := actionClaimBytes(request)
	if err != nil {
		return nil, err
	}
	claimName := request.AttemptID + ".claim"
	receiptName := request.AttemptID + ".receipt"
	if _, err := store.Lstat(claimName); errors.Is(err, os.ErrNotExist) {
		if _, err := store.Lstat(receiptName); err == nil {
			return nil, fmt.Errorf("action receipt exists without a claim")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		file, err := store.OpenFile(claimName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err == nil {
			if err = writeExact(file, claim); err == nil {
				err = file.Sync()
			}
			err = errors.Join(err, file.Close())
			if err != nil {
				return nil, fmt.Errorf("persist action claim: %w", err)
			}
			if err := syncActionDirectory(store); err != nil {
				return nil, fmt.Errorf("sync action claim directory: %w", err)
			}
			return nil, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	existing, err := readActionFile(store, claimName, 256)
	if err != nil || !bytes.Equal(existing, claim) {
		return nil, fmt.Errorf("action claim differs from exact request: %v", err)
	}
	raw, err := readActionFile(store, receiptName, MaxActionReceiptBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrActionIndeterminate
	}
	if err != nil {
		return nil, err
	}
	receipt, err := DecodeActionReceipt(request, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return &receipt, nil
}

// PublishActionSuccess records a checked guest receipt after a successful
// command. An absent or changed prior claim cannot be converted to success.
func (b *Bootstrapper) PublishActionSuccess(request ActionRequest, receipt ActionReceipt) error {
	raw, err := EncodeActionReceipt(request, receipt)
	if err != nil {
		return err
	}
	store, err := b.openActionStore(request, false)
	if err != nil {
		return err
	}
	defer store.Close()
	claim, err := actionClaimBytes(request)
	if err != nil {
		return err
	}
	existing, err := readActionFile(store, request.AttemptID+".claim", 256)
	if err != nil || !bytes.Equal(existing, claim) {
		return fmt.Errorf("action success lacks exact durable claim: %v", err)
	}
	target := request.AttemptID + ".receipt"
	if prior, err := readActionFile(store, target, MaxActionReceiptBytes); err == nil {
		if !bytes.Equal(prior, raw) {
			return fmt.Errorf("existing action receipt differs")
		}
		return syncActionDirectory(store)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporaryName := target + ".tmp-" + hex.EncodeToString(nonce[:])
	file, err := store.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer store.Remove(temporaryName)
	if err = writeExact(file, raw); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := store.Link(temporaryName, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		prior, readErr := readActionFile(store, target, MaxActionReceiptBytes)
		if readErr != nil || !bytes.Equal(prior, raw) {
			return fmt.Errorf("concurrent action receipt differs: %v", readErr)
		}
	}
	if err := store.Remove(temporaryName); err != nil {
		return err
	}
	return syncActionDirectory(store)
}

func actionClaimBytes(request ActionRequest) ([]byte, error) {
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(actionClaim{Version: Version, RequestSHA256: digest})
}

func (b *Bootstrapper) openActionStore(request ActionRequest, create bool) (*os.Root, error) {
	if b == nil || b.Root == "" {
		return nil, fmt.Errorf("action bootstrapper is required")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := b.validateBootstrapAncestors(); err != nil {
		return nil, err
	}
	active, err := b.path(filepath.Join(boxwardenRelative, activeName))
	if err != nil {
		return nil, err
	}
	if err := verifyActiveAssociation(active, request.Association); err != nil {
		return nil, err
	}
	base, err := b.path("var/lib/boxwarden")
	if err != nil {
		return nil, err
	}
	if err := safeDirectory(base, 0o700); err != nil {
		return nil, fmt.Errorf("action state parent: %w", err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	attempts, err := openActionChild(root, "action-attempts", create)
	if err != nil {
		return nil, err
	}
	defer attempts.Close()
	return openActionChild(attempts, request.SessionID, create)
}

func openActionChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := syncActionDirectory(parent); err != nil {
			return nil, err
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("unsafe action directory")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, fmt.Errorf("action directory changed while opening")
	}
	return child, nil
}

func readActionFile(root *os.Root, name string, limit int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || stat.Nlink != 1 {
		return nil, fmt.Errorf("unsafe action file")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("action file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, fmt.Errorf("action file exceeds bound: %v", err)
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return nil, fmt.Errorf("action file changed while reading")
	}
	return raw, nil
}

func syncActionDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
