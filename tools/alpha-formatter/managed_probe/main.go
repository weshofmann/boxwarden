// managed_probe is an alpha-only, explicitly planned, one-volume qualification
// driver. It is not a public workspace creation command.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/privateacl"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

const volumeBytes = int64(64 << 20)

type ownedVolume struct {
	Domain         string `json:"domain"`
	VolumeID       string `json:"volume_id"`
	FilesystemUUID string `json:"filesystem_uuid"`
	Path           string `json:"path"`
	SizeBytes      int64  `json:"size_bytes"`
	State          string `json:"state"`
}

type ownership struct {
	Version      int           `json:"version"`
	AlphaState   string        `json:"alpha_state"`
	OwnedVolumes []ownedVolume `json:"owned_volumes"`
}

func admitPlannedOwnership(manifestPath, stateRoot string, selected domain.ID, volumeID, fsUUID string) error {
	if !filepath.IsAbs(manifestPath) || filepath.Clean(manifestPath) != manifestPath {
		return fmt.Errorf("private ownership manifest path is not clean and absolute")
	}
	before, err := os.Lstat(manifestPath)
	if err != nil {
		return err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || before.Size() > 1<<20 || stat.Nlink != 1 || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("private ownership manifest identity or mode differs")
	}
	if err := privateacl.Check(manifestPath, before, privateacl.OSInspector{}); err != nil {
		return err
	}
	file, err := os.OpenFile(manifestPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return fmt.Errorf("private ownership manifest changed while opening: %v", err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("private ownership manifest is unreadable or oversized: %v", err)
	}
	var manifest ownership
	if err := rejectDuplicateJSON(raw); err != nil {
		return fmt.Errorf("private ownership manifest has duplicate JSON: %w", err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if manifest.Version != 1 || manifest.AlphaState != filepath.Dir(stateRoot) {
		return fmt.Errorf("private ownership manifest does not bind alpha state")
	}
	requested := ownedVolume{Domain: string(selected), VolumeID: volumeID, FilesystemUUID: fsUUID,
		Path: filepath.Join(stateRoot, "volumes", volumeID+".raw"), SizeBytes: volumeBytes, State: "planned"}
	count := 0
	for _, entry := range manifest.OwnedVolumes {
		if entry.VolumeID == volumeID || entry.FilesystemUUID == fsUUID || entry.Path == requested.Path {
			count++
			if entry != requested {
				return fmt.Errorf("private ownership entry differs from planned volume")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("private ownership must contain exactly one planned volume")
	}
	after, err := os.Lstat(manifestPath)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || after.Mode() != before.Mode() {
		return fmt.Errorf("private ownership manifest changed while reading: %v", err)
	}
	return nil
}

func rejectDuplicateJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid key")
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}

func run(ctx context.Context, configPath, bundle, sourceRoot, ownershipPath, volumeID, fsUUID string) error {
	loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	selected, err := loaded.Domain("alpha")
	if err != nil {
		return err
	}
	request := workspaceformat.Request{Domain: selected.ID, VolumeID: volumeID, FilesystemUUID: fsUUID, SizeBytes: volumeBytes}
	if err := admitPlannedOwnership(ownershipPath, selected.StateRoot, selected.ID, volumeID, fsUUID); err != nil {
		return err
	}
	formatter := workspaceformat.VZFormatter{StateRoot: selected.StateRoot, Domain: selected.ID,
		BundlePath: bundle, SourceRoot: sourceRoot}
	if err := formatter.Check(ctx); err != nil {
		return fmt.Errorf("formatter bundle admission: %w", err)
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return err
	}
	if _, err := hostx.NewSystemDoctor().CheckRuntime(ctx, hostx.Request{
		ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable,
		TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource,
	}); err != nil {
		return fmt.Errorf("host doctor: %w", err)
	}
	err = diskreserve.Run(ctx, []string{selected.StateRoot, bundle}, func(ctx context.Context) error {
		// Recheck the private plan immediately before the first durable write.
		if err := admitPlannedOwnership(ownershipPath, selected.StateRoot, selected.ID, volumeID, fsUUID); err != nil {
			return err
		}
		held, err := workspacex.AcquireStorageOperation(ctx, selected.StateRoot, selected.ID)
		if err != nil {
			return err
		}
		record := workspacex.Record{Version: 1, Domain: selected.ID, VolumeID: volumeID,
			SizeBytes: volumeBytes, Format: workspacex.FormatRawExt4, FilesystemUUID: fsUUID,
			State: workspacex.StateCreating}
		saveErr := workspacex.SaveRecord(selected.StateRoot, selected.ID, record)
		releaseErr := held.Release()
		if err := errors.Join(saveErr, releaseErr); err != nil {
			return fmt.Errorf("reserve creating workspace record: %w", err)
		}
		qualified, err := workspaceformat.Create(ctx, selected.StateRoot, request, formatter)
		if err != nil {
			return fmt.Errorf("managed formatter failed; retain creating record and journal: %w", err)
		}
		if qualified.VolumeID != volumeID || qualified.FilesystemUUID != fsUUID || qualified.Identity.Device == 0 || qualified.Identity.Inode == 0 {
			return fmt.Errorf("managed formatter returned different volume identity")
		}
		available, err := workspacex.PromoteVerified(ctx, selected.StateRoot, selected.ID, volumeID)
		if err != nil {
			return err
		}
		if available.State != workspacex.StateAvailable || available.Disk == nil || available.Disk.Device != qualified.Identity.Device || available.Disk.Inode != qualified.Identity.Inode {
			return fmt.Errorf("promoted workspace does not retain formatter identity")
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("qualified managed volume %s with filesystem %s and available record\n", volumeID, fsUUID)
	return nil
}

func main() {
	configPath := flag.String("config", "", "private alpha config")
	bundle := flag.String("bundle", "", "private prepared formatter bundle")
	sourceRoot := flag.String("source-root", "", "clean source checkout")
	ownershipPath := flag.String("ownership", "", "private ownership manifest")
	volumeID := flag.String("volume-id", "", "fresh planned volume UUID")
	fsUUID := flag.String("filesystem-uuid", "", "fresh planned filesystem UUID")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(2)
	}
	if err := run(context.Background(), *configPath, *bundle, *sourceRoot, *ownershipPath, *volumeID, *fsUUID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
