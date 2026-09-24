package workspaceformat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/execx"
)

// VZFormatter runs the fixed signed macOS Virtualization.framework formatter.
// BundlePath names a private, digest-bound bundle prepared for this exact
// domain state root. A successful child exit is admitted only with a stopped
// VM report bound to this format attempt. Create retains the storage lock,
// journal, and raw file and performs its independent ext4/identity checks.
type VZFormatter struct {
	StateRoot  string
	Domain     domain.ID
	BundlePath string
	runner     execx.Runner
	pins       formatterPins
}

func (v VZFormatter) commandRunner() execx.Runner {
	if v.runner != nil {
		return v.runner
	}
	return execx.OSRunner{MaxOutputBytes: 8 << 10, MaxStdinBytes: 8 << 10}
}

func (v VZFormatter) artifactPins() formatterPins {
	if v.pins.iso != "" {
		return v.pins
	}
	return productionFormatterPins()
}

// Check verifies the private bundle, exact compiled state-root binding,
// executable signature, and sole virtualization entitlement before a caller
// reserves a new volume ID. FormatAndVerify repeats this admission before boot.
func (v VZFormatter) Check(ctx context.Context) error {
	if err := validateStateRootPath(v.StateRoot); err != nil {
		return err
	}
	if _, err := domain.Parse(string(v.Domain)); err != nil {
		return err
	}
	_, err := admitVZBundle(ctx, v.BundlePath, v.StateRoot, v.Domain, v.commandRunner(), v.artifactPins())
	return err
}

func (v VZFormatter) FormatAndVerify(ctx context.Context, request FormatRequest) (FormatEvidence, error) {
	if err := validateStateRootPath(v.StateRoot); err != nil {
		return FormatEvidence{}, err
	}
	if request.Domain != v.Domain || !validUUID(request.VolumeID) || !validUUID(request.FilesystemUUID) ||
		request.SizeBytes < 4096 || request.SizeBytes > 1<<43 || request.SizeBytes%512 != 0 ||
		!lowerHex(request.Marker, 64) || request.Marker == "0000000000000000000000000000000000000000000000000000000000000000" ||
		request.DiskPath != filepath.Join(v.StateRoot, "volumes", rawName(request.VolumeID)) ||
		request.Identity.Device == 0 || request.Identity.Inode == 0 {
		return FormatEvidence{}, fmt.Errorf("formatter request differs from exact managed volume")
	}
	runner := v.commandRunner()
	bundle, err := admitVZBundle(ctx, v.BundlePath, v.StateRoot, v.Domain, runner, v.artifactPins())
	if err != nil {
		return FormatEvidence{}, fmt.Errorf("admit signed formatter bundle: %w", err)
	}
	root, err := openStateRoot(v.StateRoot)
	if err != nil {
		return FormatEvidence{}, err
	}
	defer root.Close()
	volumes, err := openVolumes(root, false)
	if err != nil {
		return FormatEvidence{}, err
	}
	defer volumes.Close()
	journalRequest := Request{Domain: request.Domain, VolumeID: request.VolumeID, FilesystemUUID: request.FilesystemUUID, SizeBytes: request.SizeBytes}
	journal, err := readJournal(volumes, journalRequest)
	if err != nil {
		return FormatEvidence{}, err
	}
	if journal.State != StateFormatting || journal.Identity == nil || *journal.Identity != request.Identity || journal.Evidence != nil {
		return FormatEvidence{}, fmt.Errorf("formatter journal does not bind this creating volume")
	}
	file, err := volumes.OpenFile(rawName(request.VolumeID), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return FormatEvidence{}, err
	}
	defer file.Close()
	identity, err := exactFile(volumes, rawName(request.VolumeID), file, request.SizeBytes)
	if err != nil {
		return FormatEvidence{}, err
	}
	if identity != request.Identity {
		return FormatEvidence{}, fmt.Errorf("formatter raw identity differs from creating journal")
	}
	marker, _ := hex.DecodeString(request.Marker)
	observed := make([]byte, len(marker))
	if _, err := file.ReadAt(observed, 0); err != nil {
		return FormatEvidence{}, err
	}
	if !bytes.Equal(marker, observed) {
		return FormatEvidence{}, fmt.Errorf("formatter raw marker differs")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return FormatEvidence{}, err
	}
	transaction := hex.EncodeToString(nonce[:])
	runCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	command := execx.Command{Path: bundle.runner, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"},
		Args: []string{"run-managed", bundle.kernel, bundle.initrd, request.DiskPath,
			fmt.Sprint(request.Identity.Device), fmt.Sprint(request.Identity.Inode), fmt.Sprint(request.SizeBytes),
			transaction, request.FilesystemUUID, request.Marker}}
	result, err := runner.Run(runCtx, command)
	if err != nil {
		return FormatEvidence{}, fmt.Errorf("isolated formatter did not exit cleanly: %w", err)
	}
	if result.Truncated || runCtx.Err() != nil {
		return FormatEvidence{}, fmt.Errorf("isolated formatter result overflowed or exceeded its deadline")
	}
	evidence, err := parseVZRunEvidence([]byte(result.Stdout), transaction, request.FilesystemUUID)
	if err != nil {
		return FormatEvidence{}, err
	}
	post, err := exactFile(volumes, rawName(request.VolumeID), file, request.SizeBytes)
	if err != nil {
		return FormatEvidence{}, err
	}
	if post != request.Identity {
		return FormatEvidence{}, fmt.Errorf("formatter raw identity changed after VM stop")
	}
	journal, err = readJournal(volumes, journalRequest)
	if err != nil {
		return FormatEvidence{}, err
	}
	if journal.State != StateFormatting || journal.Identity == nil || *journal.Identity != request.Identity || journal.Evidence != nil {
		return FormatEvidence{}, fmt.Errorf("formatter journal changed during VM run")
	}
	if err := verifyExt4Header(file, request.FilesystemUUID); err != nil {
		return FormatEvidence{}, err
	}
	return evidence, nil
}
