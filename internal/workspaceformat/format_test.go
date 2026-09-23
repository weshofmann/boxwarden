package workspaceformat

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testVolumeID = "00112233-4455-4677-8899-aabbccddeeff"
	testFSUUID   = "10213243-5465-4768-899a-bbccddeeff00"
)

type formatFunc func(context.Context, FormatRequest) (FormatEvidence, error)

func (f formatFunc) FormatAndVerify(ctx context.Context, request FormatRequest) (FormatEvidence, error) {
	return f(ctx, request)
}

func testRequest() Request {
	return Request{Domain: "work", VolumeID: testVolumeID, FilesystemUUID: testFSUUID, SizeBytes: 16 << 20}
}

func testRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// This fixture represents only the host-readable fields. A real Linux guest
// must create and check the filesystem before the formatter reports success.
func writeExt4Header(path, uuid string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{0x53, 0xef}, 1024+0x38); err != nil {
		return err
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil {
		return err
	}
	if _, err := file.WriteAt(raw, 1024+0x68); err != nil {
		return err
	}
	return file.Sync()
}

func successfulFormatter(t *testing.T) Formatter {
	t.Helper()
	return formatFunc(func(_ context.Context, request FormatRequest) (FormatEvidence, error) {
		if request.VolumeID != testVolumeID || request.FilesystemUUID != testFSUUID || request.SizeBytes != 16<<20 || !strings.HasSuffix(request.DiskPath, testVolumeID+".raw") {
			t.Fatalf("unexpected formatter request: %+v", request)
		}
		if err := writeExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return FormatEvidence{}, err
		}
		return FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}, nil
	})
}

func TestCreateQualifiesExactNewRawDiskAndAdmitRechecks(t *testing.T) {
	root := testRoot(t)
	qualified, err := Create(context.Background(), root, testRequest(), successfulFormatter(t))
	if err != nil {
		t.Fatal(err)
	}
	if qualified.VolumeID != testVolumeID || qualified.FilesystemUUID != testFSUUID || qualified.SizeBytes != 16<<20 || qualified.Identity.Device == 0 || qualified.Identity.Inode == 0 {
		t.Fatalf("incomplete qualification: %+v", qualified)
	}
	file, admitted, err := Admit(root, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if admitted != qualified {
		t.Fatalf("admitted qualification changed: got %+v want %+v", admitted, qualified)
	}
	info, err := file.Stat()
	if err != nil || info.Size() != 16<<20 || info.Mode().Perm() != 0o600 {
		t.Fatalf("admitted disk metadata: %v %v", info, err)
	}
	if _, err := Create(context.Background(), root, testRequest(), successfulFormatter(t)); err == nil {
		t.Fatal("repeated create must never reformat the volume")
	}
}

func TestCreatePersistsFailureAndNeverRetriesFormat(t *testing.T) {
	root := testRoot(t)
	calls := 0
	formatter := formatFunc(func(context.Context, FormatRequest) (FormatEvidence, error) {
		calls++
		return FormatEvidence{}, errors.New("formatter failed")
	})
	if _, err := Create(context.Background(), root, testRequest(), formatter); err == nil {
		t.Fatal("formatter failure accepted")
	}
	journal, err := ReadJournal(root, testRequest())
	if err != nil || journal.State != StateFailed {
		t.Fatalf("journal = %+v, %v", journal, err)
	}
	if _, err := Create(context.Background(), root, testRequest(), formatter); err == nil || calls != 1 {
		t.Fatalf("interrupted volume was retried: calls=%d err=%v", calls, err)
	}
	if file, _, err := Admit(root, testRequest()); err == nil {
		file.Close()
		t.Fatal("failed volume admitted")
	}
}

func TestCreateRequiresIndependentExt4FieldsAndLinuxEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		writeID  string
		evidence FormatEvidence
	}{
		{"missing-header", "", FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}},
		{"wrong-header-uuid", testVolumeID, FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}},
		{"wrong-guest-uuid", testFSUUID, FormatEvidence{ObservedUUID: testVolumeID, WholeDevice: true, FilesystemClean: true}},
		{"unclean", testFSUUID, FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true}},
		{"partition-instead-of-whole-device", testFSUUID, FormatEvidence{ObservedUUID: testFSUUID, FilesystemClean: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testRoot(t)
			formatter := formatFunc(func(_ context.Context, request FormatRequest) (FormatEvidence, error) {
				if tc.writeID != "" {
					if err := writeExt4Header(request.DiskPath, tc.writeID); err != nil {
						return FormatEvidence{}, err
					}
				}
				return tc.evidence, nil
			})
			if _, err := Create(context.Background(), root, testRequest(), formatter); err == nil {
				t.Fatal("unqualified filesystem accepted")
			}
			journal, err := ReadJournal(root, testRequest())
			if err != nil || journal.State != StateFailed {
				t.Fatalf("journal = %+v, %v", journal, err)
			}
		})
	}
}

func TestCreateRejectsExistingSymlinkOrHardlinkWithoutTouchingTarget(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := testRoot(t)
			volumeDir := filepath.Join(root, "volumes")
			if err := os.Mkdir(volumeDir, 0o700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(root, "victim")
			if err := os.WriteFile(victim, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(volumeDir, testVolumeID+".raw")
			var err error
			if kind == "symlink" {
				err = os.Symlink(victim, link)
			} else {
				err = os.Link(victim, link)
			}
			if err != nil {
				t.Fatal(err)
			}
			called := false
			formatter := formatFunc(func(context.Context, FormatRequest) (FormatEvidence, error) {
				called = true
				return FormatEvidence{}, nil
			})
			if _, err := Create(context.Background(), root, testRequest(), formatter); err == nil || called {
				t.Fatalf("existing %s used for format: err=%v called=%v", kind, err, called)
			}
			contents, err := os.ReadFile(victim)
			if err != nil || string(contents) != "safe" {
				t.Fatalf("victim changed: %q, %v", contents, err)
			}
		})
	}
}

func TestAdmitRejectsReplacementSymlinkAndHardlink(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := testRoot(t)
			if _, err := Create(context.Background(), root, testRequest(), successfulFormatter(t)); err != nil {
				t.Fatal(err)
			}
			disk := filepath.Join(root, "volumes", testVolumeID+".raw")
			if kind == "symlink" {
				if err := os.Rename(disk, disk+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(disk+".old", disk); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Link(disk, disk+".extra"); err != nil {
				t.Fatal(err)
			}
			if file, _, err := Admit(root, testRequest()); err == nil {
				file.Close()
				t.Fatalf("%s admitted", kind)
			}
		})
	}
}

func TestCreateRejectsPathReplacementDuringFormatter(t *testing.T) {
	root := testRoot(t)
	formatter := formatFunc(func(_ context.Context, request FormatRequest) (FormatEvidence, error) {
		if err := writeExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return FormatEvidence{}, err
		}
		if err := os.Rename(request.DiskPath, request.DiskPath+".old"); err != nil {
			return FormatEvidence{}, err
		}
		if err := os.Symlink(request.DiskPath+".old", request.DiskPath); err != nil {
			return FormatEvidence{}, err
		}
		return FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}, nil
	})
	if _, err := Create(context.Background(), root, testRequest(), formatter); err == nil {
		t.Fatal("replaced path accepted")
	}
}

func TestInterruptedJournalBlocksReformatAndAdmit(t *testing.T) {
	root := testRoot(t)
	opened, err := openStateRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := openVolumes(opened, true)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest()
	if err := writeInitialJournal(volumes, Journal{Version: journalVersion, Domain: request.Domain, VolumeID: request.VolumeID, FilesystemUUID: request.FilesystemUUID, SizeBytes: request.SizeBytes, State: StateReserved}); err != nil {
		t.Fatal(err)
	}
	volumes.Close()
	opened.Close()
	called := false
	formatter := formatFunc(func(context.Context, FormatRequest) (FormatEvidence, error) {
		called = true
		return FormatEvidence{}, nil
	})
	if _, err := Create(context.Background(), root, request, formatter); err == nil || called {
		t.Fatalf("interrupted operation resumed: err=%v called=%v", err, called)
	}
	if file, _, err := Admit(root, request); err == nil {
		file.Close()
		t.Fatal("incomplete journal admitted")
	}
}

func TestAdmitRejectsDifferentInodeWithSameHeader(t *testing.T) {
	root := testRoot(t)
	if _, err := Create(context.Background(), root, testRequest(), successfulFormatter(t)); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(root, "volumes", testVolumeID+".raw")
	if err := os.Rename(disk, disk+".old"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(testRequest().SizeBytes); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeExt4Header(disk, testFSUUID); err != nil {
		t.Fatal(err)
	}
	if file, _, err := Admit(root, testRequest()); err == nil {
		file.Close()
		t.Fatal("replacement inode admitted")
	}
}

func TestReadJournalRejectsDuplicateFields(t *testing.T) {
	root := testRoot(t)
	if _, err := Create(context.Background(), root, testRequest(), successfulFormatter(t)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "volumes", testVolumeID+".format.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(raw), `"state":"verified"`, `"state":"failed","state":"verified"`, 1)
	if corrupt == string(raw) {
		t.Fatal("fixture did not insert duplicate state")
	}
	if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJournal(root, testRequest()); err == nil {
		t.Fatal("duplicate journal state accepted")
	}
}

func TestCreateRequiresCleanBoundedRequest(t *testing.T) {
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Domain = "../work" },
		func(r *Request) { r.VolumeID = "../escape" },
		func(r *Request) { r.FilesystemUUID = "wrong" },
		func(r *Request) { r.SizeBytes = 512 },
		func(r *Request) { r.SizeBytes = (1 << 43) + 512 },
	} {
		root := testRoot(t)
		request := testRequest()
		mutate(&request)
		if _, err := Create(context.Background(), root, request, successfulFormatter(t)); err == nil {
			t.Fatalf("invalid request accepted: %+v", request)
		}
	}
}

func TestDiskHeadroomRequiresGreaterOfTwentyGiBOrTenPercent(t *testing.T) {
	const gib = uint64(1 << 30)
	for _, tc := range []struct {
		capacity uint64
		free     uint64
		wantOK   bool
	}{
		{100 * gib, 21 * gib, true},
		{100 * gib, 20 * gib, false},
		{240 * gib, 25 * gib, true},
		{240 * gib, 24 * gib, false},
	} {
		err := checkHeadroomValues(tc.capacity, tc.free)
		if (err == nil) != tc.wantOK {
			t.Fatalf("capacity=%d free=%d accepted=%v, want %v: %v", tc.capacity, tc.free, err == nil, tc.wantOK, err)
		}
	}
}

func TestCreateDoesNotVerifyAfterCallerCancellation(t *testing.T) {
	root := testRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	formatter := formatFunc(func(_ context.Context, request FormatRequest) (FormatEvidence, error) {
		if err := writeExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return FormatEvidence{}, err
		}
		cancel()
		return FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}, nil
	})
	if _, err := Create(ctx, root, testRequest(), formatter); err == nil {
		t.Fatal("canceled format operation was marked verified")
	}
	journal, err := ReadJournal(root, testRequest())
	if err != nil || journal.State == StateVerified {
		t.Fatalf("canceled journal = %+v, %v", journal, err)
	}
}
