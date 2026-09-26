package sessionruntime

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// Delete admits the exact host toolchain before a stopped-only Tart deletion.
// Workspace metadata and disk retention are reconciled by the common driver.
func Delete(ctx context.Context, loaded config.Config, selected config.Domain, name string) error {
	admittedDomain, err := loaded.Domain(string(selected.ID))
	if err != nil || admittedDomain != selected {
		return fmt.Errorf("deletion requires exact configured domain")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return err
	}
	request := hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable,
		TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource}
	if _, err := hostx.NewSystemDoctor().CheckRuntime(ctx, request); err != nil {
		return fmt.Errorf("admit host toolchain before deletion: %w", err)
	}
	runner := execx.OSRunner{MaxOutputBytes: 1 << 20}
	observer := tart.NewQualifiedObserver(runner, host.Host.TartExecutable, host.Host.TartHome)
	service := session.NewDeleteService(selected, session.DeleteDependencies{
		Observer: observer,
		Deleter:  tart.NewDeleter(runner, host.Host.TartExecutable, host.Host.TartHome),
		Gate:     workspacex.WithStoppedDeleteGate,
		Finalize: workspacex.FinalizeDeletedSession,
	})
	return service.Delete(ctx, name)
}
