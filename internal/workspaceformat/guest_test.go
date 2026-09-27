package workspaceformat

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const testMarker = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type guestEnvironmentFake struct {
	disks     []GuestDisk
	snapshots [][]GuestDisk
	calls     [][]string
	uuid      string
	fail      string
}

func (e *guestEnvironmentFake) Disks(context.Context, int64) ([]GuestDisk, error) {
	if len(e.snapshots) != 0 {
		e.disks = e.snapshots[0]
		e.snapshots = e.snapshots[1:]
	}
	return append([]GuestDisk(nil), e.disks...), nil
}

func (e *guestEnvironmentFake) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	call := append([]string{path}, args...)
	e.calls = append(e.calls, call)
	if strings.Contains(path, e.fail) && e.fail != "" {
		return nil, errors.New("guest command failed")
	}
	if strings.HasSuffix(path, "/blkid") {
		if len(args) >= 5 && args[4] == "TYPE" {
			return []byte("ext4\n"), nil
		}
		return []byte(e.uuid + "\n"), nil
	}
	return nil, nil
}

func testGuestRequest() GuestFormatRequest {
	return GuestFormatRequest{FilesystemUUID: testFSUUID, SizeBytes: 16 << 20, Marker: testMarker}
}

func testGuestDisk() GuestDisk {
	return GuestDisk{Path: "/dev/vdb", Identity: "252:16", SizeBytes: 16 << 20, Marker: testMarker, Block: true}
}

func TestGuestFormatterUsesOnlyUniqueMarkerMatchedWholeDevice(t *testing.T) {
	env := &guestEnvironmentFake{disks: []GuestDisk{{Path: "/dev/vda", Identity: "252:0", SizeBytes: 40 << 30}, testGuestDisk()}, uuid: testFSUUID}
	evidence, err := formatInLinuxGuest(t.Context(), testGuestRequest(), env)
	if err != nil {
		t.Fatal(err)
	}
	if evidence != (FormatEvidence{ObservedUUID: testFSUUID, WholeDevice: true, FilesystemClean: true}) {
		t.Fatalf("evidence = %+v", evidence)
	}
	want := [][]string{
		{"/usr/sbin/mkfs.ext4", "-F", "-U", testFSUUID, "-m", "0", "-L", "boxwarden-workspace", "/dev/vdb"},
		{"/usr/sbin/e2fsck", "-f", "-n", "/dev/vdb"},
		{"/usr/sbin/blkid", "-p", "-o", "value", "-s", "TYPE", "/dev/vdb"},
		{"/usr/sbin/blkid", "-p", "-o", "value", "-s", "UUID", "/dev/vdb"},
	}
	if !reflect.DeepEqual(env.calls, want) {
		t.Fatalf("guest commands = %#v, want %#v", env.calls, want)
	}
}

func TestGuestFormatterRefusesAmbiguousOrUnsafeDiskBeforeMkfs(t *testing.T) {
	for _, kind := range []string{"absent", "duplicate", "mounted", "partitioned", "root", "swap", "stacked", "not block", "wrong size", "bad marker", "invalid path"} {
		t.Run(kind, func(t *testing.T) {
			disk := testGuestDisk()
			env := &guestEnvironmentFake{disks: []GuestDisk{disk}, uuid: testFSUUID}
			switch kind {
			case "absent":
				env.disks = nil
			case "duplicate":
				copy := disk
				copy.Path, copy.Identity = "/dev/vdc", "252:32"
				env.disks = append(env.disks, copy)
			case "mounted":
				disk.Mounted = true
			case "partitioned":
				disk.Partitioned = true
			case "root":
				disk.Root = true
			case "swap":
				disk.Swap = true
			case "stacked":
				disk.Stacked = true
			case "not block":
				disk.Block = false
			case "wrong size":
				disk.SizeBytes = 8 << 20
			case "bad marker":
				disk.Marker = "not-the-marker"
			case "invalid path":
				disk.Path = "/dev/../vda"
			}
			if kind != "absent" && kind != "duplicate" {
				env.disks[0] = disk
			}
			if _, err := formatInLinuxGuest(t.Context(), testGuestRequest(), env); err == nil || len(env.calls) != 0 {
				t.Fatalf("%s reached mkfs: err=%v calls=%#v", kind, err, env.calls)
			}
		})
	}
}

func TestGuestFormatterRequiresIndependentChecks(t *testing.T) {
	for _, kind := range []string{"mkfs failure", "e2fsck failure", "wrong UUID"} {
		t.Run(kind, func(t *testing.T) {
			env := &guestEnvironmentFake{disks: []GuestDisk{testGuestDisk()}, uuid: testFSUUID}
			switch kind {
			case "mkfs failure":
				env.fail = "mkfs.ext4"
			case "e2fsck failure":
				env.fail = "e2fsck"
			case "wrong UUID":
				env.uuid = testVolumeID
			}
			if _, err := formatInLinuxGuest(t.Context(), testGuestRequest(), env); err == nil {
				t.Fatal("unverified guest filesystem accepted")
			}
		})
	}
}

func TestGuestFormatterRejectsChangedMarkerBeforeMkfs(t *testing.T) {
	changed := testGuestDisk()
	changed.Marker = "different"
	env := &guestEnvironmentFake{snapshots: [][]GuestDisk{{testGuestDisk()}, {changed}}, uuid: testFSUUID}
	if _, err := formatInLinuxGuest(t.Context(), testGuestRequest(), env); err == nil || len(env.calls) != 0 {
		t.Fatalf("changed marker reached mkfs: err=%v calls=%#v", err, env.calls)
	}
}

func TestGuestFormatterStopsAfterDiskBecomesMounted(t *testing.T) {
	changed := testGuestDisk()
	changed.Mounted = true
	env := &guestEnvironmentFake{snapshots: [][]GuestDisk{{testGuestDisk()}, {testGuestDisk()}, {changed}}, uuid: testFSUUID}
	if _, err := formatInLinuxGuest(t.Context(), testGuestRequest(), env); err == nil || len(env.calls) != 1 || env.calls[0][0] != "/usr/sbin/mkfs.ext4" {
		t.Fatalf("mounted disk reached verification: err=%v calls=%#v", err, env.calls)
	}
}

func TestGuestCommandCaptureDetectsOverflow(t *testing.T) {
	var output boundedGuestOutput
	if n, err := output.Write([]byte(strings.Repeat(" ", 4096))); err != nil || n != 4096 || output.overflow {
		t.Fatalf("exact-size output = %d, %v, overflow=%v", n, err, output.overflow)
	}
	if n, err := output.Write([]byte("ext4")); err != nil || n != 4 || !output.overflow || output.Len() != 4096 {
		t.Fatalf("overflow output = %d, %v, overflow=%v length=%d", n, err, output.overflow, output.Len())
	}
}
