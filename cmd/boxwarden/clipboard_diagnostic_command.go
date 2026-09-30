//go:build n1clipboarddiagnostic && !n1candidate

package main

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"io"
	"path/filepath"
)

type diagnosticCommandRequest struct {
	Version   int                     `json:"version"`
	Session   string                  `json:"session"`
	Operation clipboarddiag.Operation `json:"operation"`
}
type diagnosticCollectionCommandRequest struct {
	Version   int                     `json:"version"`
	Session   string                  `json:"session"`
	Operation clipboarddiag.Operation `json:"operation"`
	CLI       clipboarddiag.Fragment  `json:"cli"`
}

func runDiagnosticCommand(ctx context.Context, action string, raw []byte, output io.Writer) error {
	return runDiagnosticCommandWith(ctx, action, raw, output, config.Load, newDiagnosticService)
}
func newDiagnosticService(ctx context.Context, loaded config.Config, selected config.Domain) (*session.ClipboardDiagnosticService, error) {
	admitted, err := loaded.Domain("n1qualification")
	if err != nil || admitted != selected || len(loaded.Domains()) != 1 {
		return nil, clipboardx.ErrAdmission
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return nil, clipboardx.ErrAdmission
	}
	doctor := hostx.NewSystemDoctor()
	request := hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable, TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource}
	if _, err = doctor.CheckRuntime(ctx, request); err != nil {
		return nil, clipboardx.ErrAdmission
	}
	endpoint, err := supervisor.NewExactClipboardDiagnosticController(filepath.Join(selected.StateRoot, "runtime"))
	if err != nil {
		return nil, clipboardx.ErrAdmission
	}
	return session.NewClipboardDiagnosticService(selected, endpoint)
}
func runDiagnosticCommandWith(ctx context.Context, action string, raw []byte, output io.Writer, load func(string) (config.Config, error), factory func(context.Context, config.Config, config.Domain) (*session.ClipboardDiagnosticService, error)) error {
	var request diagnosticCommandRequest
	var cli clipboarddiag.Fragment
	if action == "invoke" {
		if clipboarddiag.StrictDecode(raw, &request, clipboarddiag.MaxHeaderBytes) != nil {
			return clipboardx.ErrRequest
		}
	} else if action == "collect" {
		var c diagnosticCollectionCommandRequest
		if clipboarddiag.StrictDecode(raw, &c, clipboarddiag.MaxFragmentBytes) != nil {
			return clipboardx.ErrRequest
		}
		request = diagnosticCommandRequest{c.Version, c.Session, c.Operation}
		cli = c.CLI
	} else {
		return clipboardx.ErrRequest
	}
	if request.Version != 1 || request.Operation.Validate(action == "invoke") != nil {
		return clipboardx.ErrRequest
	}
	if _, err := session.ParseName(request.Session); err != nil {
		return clipboardx.ErrRequest
	}
	loaded, err := load(clipboardDiagnosticConfigPath)
	if err != nil {
		return clipboardx.ErrAdmission
	}
	if len(loaded.Domains()) != 1 {
		return clipboardx.ErrAdmission
	}
	selected, err := loaded.Domain("n1qualification")
	if err != nil {
		return clipboardx.ErrAdmission
	}
	service, err := factory(ctx, loaded, selected)
	if err != nil || service == nil {
		return clipboardx.ErrAdmission
	}
	var encoded []byte
	if action == "invoke" {
		receipt, err := service.Invoke(ctx, request.Session, request.Operation)
		if err != nil {
			return clipboardx.ErrAdmission
		}
		encoded, err = json.Marshal(receipt)
		if err != nil || len(encoded)+1 > clipboarddiag.MaxFragmentBytes {
			return clipboarddiag.ErrMetadata
		}
	} else {
		receipt, err := service.Collect(ctx, request.Session, request.Operation, cli)
		if err != nil {
			return clipboarddiag.ErrMetadata
		}
		encoded, err = json.Marshal(receipt)
		if err != nil || len(encoded)+1 > clipboarddiag.MaxCollectionBytes {
			return clipboarddiag.ErrMetadata
		}
	}
	n, err := output.Write(append(encoded, '\n'))
	if err != nil || n != len(encoded)+1 {
		return clipboarddiag.ErrMetadata
	}
	return nil
}
