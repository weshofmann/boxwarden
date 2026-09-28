package workspaceformat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

const testHostVolumeUUID = "00112233-4455-6677-8899-aabbccddeeff"

type legacyFixture struct {
	root    string
	request Request
	journal Journal
	storage hostidentity.StorageExpectation
	observe func(*os.File) (hostidentity.Identity, error)
	check   func(hostidentity.StorageExpectation) error
}

func driftedLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	root := testRoot(t)
	request := testRequest()
	if _, err := Create(context.Background(), root, request, successfulFormatter(t)); err != nil {
		t.Fatal(err)
	}
	volumes := filepath.Join(root, "volumes")
	journal, err := ReadJournal(root, request)
	if err != nil {
		t.Fatal(err)
	}
	// Create uses v2 on a capable macOS host; render the historical v1 receipt
	// explicitly so this regression is deterministic on macOS and Linux.
	journal.Version = 1
	journal.HostIdentity = nil
	journal.Identity.Device++ // The same raw file is now observed through a new mount instance.
	parent, err := os.OpenRoot(volumes)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := replaceJournal(parent, journal); err != nil {
		t.Fatal(err)
	}
	return legacyFixture{root: root, request: request, journal: journal,
		storage: hostidentity.StorageExpectation{ConfigPath: "/external/config.json", StateRoot: root,
			MountPoint: root, VolumeUUID: testHostVolumeUUID},
		observe: func(file *os.File) (hostidentity.Identity, error) {
			info, err := file.Stat()
			if err != nil {
				return hostidentity.Identity{}, err
			}
			identity, err := diskIdentity(info)
			return hostidentity.Identity{VolumeUUID: testHostVolumeUUID, FileID: identity.Inode}, err
		},
		check: func(hostidentity.StorageExpectation) error { return nil },
	}
}

func TestLegacyBindingAdmitsSamePinnedFileAfterDeviceDrift(t *testing.T) {
	fixture := driftedLegacyFixture(t)
	root, request, journal := fixture.root, fixture.request, fixture.journal
	volumes := filepath.Join(root, "volumes")
	if file, _, err := Admit(root, request); err == nil {
		file.Close()
		t.Fatal("unbound v1 receipt admitted changed device number")
	}
	if err := bindLegacy(root, request, fixture.storage, fixture.observe, fixture.check); err != nil {
		t.Fatal(err)
	}
	file, qualified, err := admitVerifiedWithHost(root, request, fixture.observe)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if qualified.Identity != *journal.Identity {
		t.Fatalf("legacy receipt identity rewritten: %+v", qualified)
	}
	if err := bindLegacy(root, request, fixture.storage, fixture.observe, fixture.check); err != nil {
		t.Fatalf("identical binding retry must settle: %v", err)
	}
	entries, err := os.ReadDir(volumes)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".binding.json") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("binding count = %d", count)
	}
}

func TestLegacyBindingRejectsWrongHostVolumeAndSubstitutedRaw(t *testing.T) {
	for _, replacement := range []string{"wrong_volume", "new_inode_same_ext4_header"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := driftedLegacyFixture(t)
			if replacement == "wrong_volume" {
				fixture.storage.VolumeUUID = "11112233-4455-6677-8899-aabbccddeeff"
			} else {
				path := filepath.Join(fixture.root, "volumes", rawName(fixture.request.VolumeID))
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				newFile, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if err := newFile.Truncate(fixture.request.SizeBytes); err != nil {
					t.Fatal(err)
				}
				if err := newFile.Close(); err != nil {
					t.Fatal(err)
				}
				if err := writeExt4Header(path, fixture.request.FilesystemUUID); err != nil {
					t.Fatal(err)
				}
			}
			if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err == nil {
				t.Fatal("ambiguous legacy disk was bound")
			}
			if _, err := os.Lstat(filepath.Join(fixture.root, "volumes", legacyBindingName(fixture.request.VolumeID))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed binding published sidecar: %v", err)
			}
		})
	}
}

func TestLegacyBindingMalformedSidecarFailsClosed(t *testing.T) {
	fixture := driftedLegacyFixture(t)
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.root, "volumes", legacyBindingName(fixture.request.VolumeID))
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, _, err := admitVerifiedWithHost(fixture.root, fixture.request, fixture.observe); err == nil {
		file.Close()
		t.Fatal("malformed sidecar admitted legacy disk")
	}
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err == nil {
		t.Fatal("malformed sidecar was overwritten")
	}
}

func TestLegacyBindingChangedReceiptFailsClosed(t *testing.T) {
	fixture := driftedLegacyFixture(t)
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(filepath.Join(fixture.root, "volumes"))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	changed := fixture.journal
	changed.Identity.Device++
	if err := replaceJournal(parent, changed); err != nil {
		t.Fatal(err)
	}
	if file, _, err := admitVerifiedWithHost(fixture.root, fixture.request, fixture.observe); err == nil {
		file.Close()
		t.Fatal("changed receipt admitted through old binding")
	}
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err == nil {
		t.Fatal("old binding was replaced for changed receipt")
	}
}

func TestLegacyBindingCrashWithPublishedTempHardlinkFailsClosed(t *testing.T) {
	fixture := driftedLegacyFixture(t)
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.root, "volumes", legacyBindingName(fixture.request.VolumeID))
	if err := os.Link(path, path+".tmp-crash"); err != nil {
		t.Fatal(err)
	}
	if file, _, err := admitVerifiedWithHost(fixture.root, fixture.request, fixture.observe); err == nil {
		file.Close()
		t.Fatal("two-link crash sidecar admitted")
	}
	if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err == nil {
		t.Fatal("two-link crash sidecar was silently settled")
	}
}

func TestLegacyBindingPublicationFailuresSettleOnlyOnExactRetry(t *testing.T) {
	for _, stage := range []bindingPublicationStage{bindingBeforeLink, bindingAfterLink, bindingAfterRemove, bindingBeforeSync, bindingAfterSync} {
		t.Run(string(stage), func(t *testing.T) {
			fixture := driftedLegacyFixture(t)
			err := bindLegacyWithHook(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check,
				func(at bindingPublicationStage) error {
					if at == stage {
						return errors.New("injected binding publication failure")
					}
					return nil
				})
			if err == nil {
				t.Fatal("publication failure reported success")
			}
			path := filepath.Join(fixture.root, "volumes", legacyBindingName(fixture.request.VolumeID))
			_, statErr := os.Lstat(path)
			if stage == bindingBeforeLink {
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("pre-link failure published binding: %v", statErr)
				}
			} else if statErr != nil {
				t.Fatalf("post-link failure lost published binding: %v", statErr)
			}
			if err := bindLegacy(fixture.root, fixture.request, fixture.storage, fixture.observe, fixture.check); err != nil {
				t.Fatalf("exact retry did not settle: %v", err)
			}
			file, _, err := admitVerifiedWithHost(fixture.root, fixture.request, fixture.observe)
			if err != nil {
				t.Fatal(err)
			}
			file.Close()
		})
	}
}
