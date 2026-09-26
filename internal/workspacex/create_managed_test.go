package workspacex

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

type managedFormatter struct {
	checks, formats     int
	checkErr, formatErr error
}

func (f *managedFormatter) Check(context.Context) error {
	f.checks++
	return f.checkErr
}

func (f *managedFormatter) FormatAndVerify(_ context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
	f.formats++
	if f.formatErr != nil {
		return workspaceformat.FormatEvidence{}, f.formatErr
	}
	if err := writeFixtureExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
		return workspaceformat.FormatEvidence{}, err
	}
	return workspaceformat.FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
}

func managedRequest() workspaceformat.Request {
	return workspaceformat.Request{Domain: domain.ID("work"), VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", SizeBytes: 16 << 20}
}

func TestCreateManagedWorkspacePromotesAndRetriesWithoutReformat(t *testing.T) {
	root := privateRoot(t)
	request := managedRequest()
	formatter := &managedFormatter{}
	record, err := CreateManaged(t.Context(), root, request, formatter)
	if err != nil || record.State != StateAvailable || record.Disk == nil || formatter.checks != 1 || formatter.formats != 1 {
		t.Fatalf("create = %#v, %v; formatter = %#v", record, err, formatter)
	}
	formatter.checkErr = errors.New("bundle no longer available")
	again, err := CreateManaged(t.Context(), root, request, formatter)
	if err != nil || again.Disk == nil || *again.Disk != *record.Disk || formatter.checks != 1 || formatter.formats != 1 {
		t.Fatalf("exact retry = %#v, %v; formatter = %#v", again, err, formatter)
	}
	request.FilesystemUUID = "20213243-5465-4768-899a-bbccddeeff00"
	if _, err := CreateManaged(t.Context(), root, request, formatter); err == nil || formatter.formats != 1 {
		t.Fatalf("changed identity accepted: %v; formatter = %#v", err, formatter)
	}
}

func TestCreateManagedWorkspaceRecoversVerifiedJournalWithoutRecord(t *testing.T) {
	root := privateRoot(t)
	request := managedRequest()
	formatter := &managedFormatter{}
	if _, err := workspaceformat.Create(t.Context(), root, request, formatter); err != nil {
		t.Fatal(err)
	}
	formatter.checkErr = errors.New("old bundle unavailable")
	record, err := CreateManaged(t.Context(), root, request, formatter)
	if err != nil || record.State != StateAvailable || formatter.checks != 0 || formatter.formats != 1 {
		t.Fatalf("verified journal recovery = %#v, %v; formatter = %#v", record, err, formatter)
	}
}

func TestCreateManagedWorkspacePreflightAndFailedFormatStayUnqualified(t *testing.T) {
	root := privateRoot(t)
	request := managedRequest()
	formatter := &managedFormatter{checkErr: errors.New("unadmitted formatter")}
	if _, err := CreateManaged(t.Context(), root, request, formatter); err == nil || formatter.formats != 0 {
		t.Fatalf("unadmitted bundle reached formatter: %v; %#v", err, formatter)
	}
	if _, err := os.Stat(root + "/volumes/" + request.VolumeID + ".raw"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight reserved raw disk: %v", err)
	}
	formatter.checkErr = nil
	formatter.formatErr = errors.New("guest e2fsck failed")
	if _, err := CreateManaged(t.Context(), root, request, formatter); err == nil || formatter.formats != 1 {
		t.Fatalf("failed format result: %v; %#v", err, formatter)
	}
	formatter.formatErr = nil
	if _, err := CreateManaged(t.Context(), root, request, formatter); err == nil || formatter.formats != 1 {
		t.Fatalf("failed journal retried format: %v; %#v", err, formatter)
	}
}
