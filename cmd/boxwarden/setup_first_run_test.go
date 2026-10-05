package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/hostx"
)

func firstRunFixture(t *testing.T) (app.FirstRunInput, firstRunDependencies) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	data := filepath.Join(root, "data")
	for _, path := range []string{home, data} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	input := app.FirstRunInput{SetupID: "00112233-4455-6677-8899-aabbccddeeff", DataLocation: data, PackageRoot: "/package", ISOPath: "/installer.iso"}
	d := firstRunDependencies{
		home: func() (string, error) { return home, nil },
		location: func(_ string, ancestor string) (hostidentity.LocationObservation, error) {
			dataInfo, _ := os.Stat(data)
			outputInfo, _ := os.Stat(ancestor)
			return hostidentity.LocationObservation{MountPoint: data, VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff", FileID: uint64(dataInfo.Sys().(*syscall.Stat_t).Ino), OutputVolumeUUID: "ffeeddcc-bbaa-9988-7766-554433221100", OutputFileID: uint64(outputInfo.Sys().(*syscall.Stat_t).Ino), AvailableBytes: 32 << 30}, nil
		},
		observe: func(file *os.File) (hostidentity.Identity, error) {
			info, err := file.Stat()
			if err != nil {
				return hostidentity.Identity{}, err
			}
			dataInfo, err := os.Stat(data)
			if err != nil {
				return hostidentity.Identity{}, err
			}
			uuid := "ffeeddcc-bbaa-9988-7766-554433221100"
			if os.SameFile(info, dataInfo) {
				uuid = "00112233-4455-6677-8899-aabbccddeeff"
			}
			return hostidentity.Identity{VolumeUUID: uuid, FileID: uint64(info.Sys().(*syscall.Stat_t).Ino)}, nil
		},
		alternatives: func(string) ([]string, error) { return []string{data}, nil },
		host: func(context.Context, string, string) (config.Host, hostx.Report, string, error) {
			return config.Host{TartExecutable: "/operator/tart", TartHome: "/operator/tart-home", SoftnetSource: "/Library/softnet"}, hostx.Report{Status: hostx.Healthy}, "host1", nil
		},
		resources: func(context.Context, app.FirstRunInput) (app.SetupPrepareInput, string, error) {
			return app.SetupPrepareInput{PackageRoot: "/package", ISOPath: "/installer.iso"}, "resources1", nil
		},
		publish: func(expected hostidentity.StorageExpectation, raw []byte) error {
			f, err := os.OpenFile(expected.ConfigPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(raw)
			return err
		},
		prepare: func(_ context.Context, path string, _ app.SetupPrepareInput, _ io.Writer) (app.SetupInspection, bool, error) {
			return app.SetupInspection{Version: 1, Scope: "alpha_project_setup", ConfigPath: path, Status: "ready", SelectionAcceptable: true, ConfigValid: true}, true, nil
		},
	}
	return input, d
}

func TestFirstRunPlanningIsReadOnlyAndReturnsCanonicalFreshTargets(t *testing.T) {
	input, d := firstRunFixture(t)
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "ready" || len(plan.ExpectedDigest) != 64 || plan.ExistingEntries != 0 {
		t.Fatalf("plan=%#v", plan)
	}
	if !strings.Contains(plan.ConfigPath, "/Library/Application Support/boxwarden/setups/"+input.SetupID+"/config.json") {
		t.Fatalf("path=%s", plan.ConfigPath)
	}
	if _, err := os.Lstat(filepath.Dir(plan.ConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created directories: %v", err)
	}
	if _, err := os.Lstat(plan.StateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created root: %v", err)
	}
}

func TestFirstRunCreateRechecksStalePlanBeforeAnyWrite(t *testing.T) {
	input, d := firstRunFixture(t)
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedDigest = plan.ExpectedDigest
	d.resources = func(context.Context, app.FirstRunInput) (app.SetupPrepareInput, string, error) {
		return app.SetupPrepareInput{}, "changed", nil
	}
	_, uncertain, err := createFirstRunWithDependencies(context.Background(), input, io.Discard, d)
	if err == nil || uncertain || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("create=%v,%v", uncertain, err)
	}
	if _, err := os.Lstat(filepath.Dir(plan.ConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale plan created directories: %v", err)
	}
}

func TestFirstRunCreatePublishesFreshPrivateConfigAndPreservesExistingEntries(t *testing.T) {
	input, d := firstRunFixture(t)
	sentinel := filepath.Join(input.DataLocation, "unrelated")
	if err := os.WriteFile(sentinel, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedDigest = plan.ExpectedDigest
	result, _, err := createFirstRunWithDependencies(context.Background(), input, io.Discard, d)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConfigPath != plan.ConfigPath || result.Status != "ready" {
		t.Fatalf("result=%#v", result)
	}
	for _, path := range []string{filepath.Dir(plan.ConfigPath), plan.StateRoot} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("private dir %s: %v %v", path, info, err)
		}
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "retain" {
		t.Fatalf("unrelated=%q,%v", raw, err)
	}
	_, uncertain, err := createFirstRunWithDependencies(context.Background(), input, io.Discard, d)
	if err == nil || uncertain {
		t.Fatalf("collision retry=%v,%v", uncertain, err)
	}
}

func TestFirstRunCreateFailureRetainsTruthfulUncertainty(t *testing.T) {
	input, d := firstRunFixture(t)
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedDigest = plan.ExpectedDigest
	d.publish = func(expected hostidentity.StorageExpectation, raw []byte) error {
		if err := os.WriteFile(expected.ConfigPath, raw, 0600); err != nil {
			return err
		}
		return errors.New("directory sync failed")
	}
	_, uncertain, err := createFirstRunWithDependencies(context.Background(), input, io.Discard, d)
	if err == nil || !uncertain || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("publication failure=%v,%v", uncertain, err)
	}
	if _, err := os.Stat(plan.ConfigPath); err != nil {
		t.Fatalf("uncertain config not retained: %v", err)
	}
}

func TestFirstRunDirectoriesRejectChangedPinnedDirectoryBeforeWrites(t *testing.T) {
	input, d := firstRunFixture(t)
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(input.DataLocation, input.DataLocation+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(input.DataLocation, 0700); err != nil {
		t.Fatal(err)
	}
	changed := false
	if err := createFirstRunDirectories(context.Background(), plan, d.observe, &changed); err == nil || changed {
		t.Fatalf("changed directory admission=%v, changed=%v", err, changed)
	}
	if _, err := os.Lstat(filepath.Dir(plan.ConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement caused output writes: %v", err)
	}
}

func TestFirstRunDirectoriesRejectChangedVolumeBeforeWrites(t *testing.T) {
	input, d := firstRunFixture(t)
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	// A matching inode is not authority when its APFS volume changed.
	plan.VolumeUUID = "11112233-4455-6677-8899-aabbccddeeff"
	changed := false
	if err := createFirstRunDirectories(context.Background(), plan, d.observe, &changed); err == nil || changed {
		t.Fatalf("changed-volume same-file-ID admission=%v, changed=%v", err, changed)
	}
	if _, err := os.Lstat(filepath.Dir(plan.ConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed volume caused config writes: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(plan.StateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed volume caused data writes: %v", err)
	}
}

func TestFirstRunCreateRejectsChangedOpenedAPFSIdentitiesWithoutEffects(t *testing.T) {
	for _, change := range []string{"data_volume", "output_volume", "data_permanent_id", "output_permanent_id", "observation_error", "inspection_missing"} {
		t.Run(change, func(t *testing.T) {
			input, d := firstRunFixture(t)
			plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
			if err != nil {
				t.Fatal(err)
			}
			input.ExpectedDigest = plan.ExpectedDigest
			originalObserve := d.observe
			d.observe = func(file *os.File) (hostidentity.Identity, error) {
				identity, err := originalObserve(file)
				if err != nil {
					return identity, err
				}
				data := identity.VolumeUUID == plan.VolumeUUID
				switch {
				case change == "observation_error":
					return hostidentity.Identity{}, errors.New("synthetic APFS observation unavailable")
				case change == "data_volume" && data, change == "output_volume" && !data:
					identity.VolumeUUID = "11112233-4455-6677-8899-aabbccddeeff"
				case change == "data_permanent_id" && data, change == "output_permanent_id" && !data:
					identity.FileID++
				}
				return identity, nil
			}
			if change == "inspection_missing" {
				d.observe = nil
			}
			d.publish = func(hostidentity.StorageExpectation, []byte) error {
				t.Fatal("identity failure reached config publication")
				return nil
			}
			d.prepare = func(context.Context, string, app.SetupPrepareInput, io.Writer) (app.SetupInspection, bool, error) {
				t.Fatal("identity failure reached preparation")
				return app.SetupInspection{}, false, nil
			}
			_, uncertain, err := createFirstRunWithDependencies(context.Background(), input, io.Discard, d)
			if err == nil || uncertain {
				t.Fatalf("identity change %s admitted: uncertain=%v err=%v", change, uncertain, err)
			}
			home, _ := d.home()
			if _, err := os.Lstat(filepath.Join(home, "Library")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("identity change created config hierarchy: %v", err)
			}
			entries, err := os.ReadDir(input.DataLocation)
			if err != nil || len(entries) != 0 {
				t.Fatalf("identity change created data entries: %v, %v", entries, err)
			}
		})
	}
}

func TestFirstRunPlanReserveIncludesExistingStorageFloorAndInitialWorkspace(t *testing.T) {
	input, d := firstRunFixture(t)
	d.location = func(string, string) (hostidentity.LocationObservation, error) {
		return hostidentity.LocationObservation{MountPoint: input.DataLocation, VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff", CapacityBytes: 300 << 30, AvailableBytes: 30 << 30}, nil
	}
	plan, _, _, err := buildFirstRunPlan(context.Background(), input, d)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "insufficient_space" || plan.ReserveBytes != (30<<30)+(512<<20) {
		t.Fatalf("reserve plan=%#v", plan)
	}
}

func TestFirstRunPlanBudgetsColdRecipeBeforeCreatingConfig(t *testing.T) {
	input, d := firstRunFixture(t)
	d.location = func(string, string) (hostidentity.LocationObservation, error) {
		return hostidentity.LocationObservation{MountPoint: input.DataLocation, VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff", AvailableBytes: 22 << 30, CapacityBytes: 128 << 30}, nil
	}
	d.resources = func(context.Context, app.FirstRunInput) (app.SetupPrepareInput, string, error) {
		return app.SetupPrepareInput{PackageRoot: "/package", ISOPath: "/installer.iso", BootstrapBytes: 7 << 30}, "resources1", nil
	}
	plan, _, _, err := buildFirstRunPlan(t.Context(), input, d)
	if err != nil || plan.Status != "insufficient_space" || plan.ReserveBytes != (27<<30)+(512<<20) {
		t.Fatalf("cold preparation budget: %#v,%v", plan, err)
	}
	if _, err := os.Lstat(plan.StateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plan created state: %v", err)
	}
}

func TestFirstRunPrerequisiteFailuresAndCancellationDoNotWrite(t *testing.T) {
	for _, failure := range []string{"storage", "host_missing", "host_unsafe", "resources", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			input, d := firstRunFixture(t)
			ctx := context.Background()
			switch failure {
			case "storage":
				d.location = func(string, string) (hostidentity.LocationObservation, error) {
					return hostidentity.LocationObservation{}, errors.New("mode 775")
				}
			case "host_missing":
				d.host = func(context.Context, string, string) (config.Host, hostx.Report, string, error) {
					return config.Host{}, hostx.Report{Status: hostx.Missing}, "", nil
				}
			case "host_unsafe":
				d.host = func(context.Context, string, string) (config.Host, hostx.Report, string, error) {
					return config.Host{}, hostx.Report{Status: hostx.Drifted}, "", nil
				}
			case "resources":
				d.resources = func(context.Context, app.FirstRunInput) (app.SetupPrepareInput, string, error) {
					return app.SetupPrepareInput{}, "", errors.New("missing prerequisite")
				}
			case "cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			plan, _, _, err := buildFirstRunPlan(ctx, input, d)
			if failure != "cancel" && (err != nil || plan.Status == "ready") {
				t.Fatalf("failure=%s plan=%#v err=%v", failure, plan, err)
			}
			input.ExpectedDigest = strings.Repeat("a", 64)
			_, uncertain, err := createFirstRunWithDependencies(ctx, input, io.Discard, d)
			if err == nil || uncertain {
				t.Fatalf("failure=%s create=%v,%v", failure, uncertain, err)
			}
		})
	}
}
