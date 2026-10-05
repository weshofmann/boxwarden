package app

import (
	"context"
	"errors"
	"flag"
	"io"
)

type SetupPrepareInput struct {
	PackageRoot string
	ISOPath     string
	CheckerPath string
	GoBinary    string
	ZstdBinary  string
	OpenSSLPath string
	XorrisoPath string
}

type SetupPrepareFunc func(context.Context, string, SetupPrepareInput, io.Writer) (SetupInspection, bool, error)

func runSetupPrepare(ctx context.Context, args []string, o Options) (handled bool, err error) {
	globals := flag.NewFlagSet("setup", flag.ContinueOnError)
	globals.SetOutput(io.Discard)
	path := globals.String("config", o.ConfigPath, "configuration")
	domain := globals.String("domain", "", "domain")
	if globals.Parse(args) != nil {
		return false, nil
	}
	rest := globals.Args()
	if len(rest) < 2 || rest[0] != "setup" || rest[1] != "prepare" {
		return false, nil
	}
	if o.Output == nil {
		return true, errors.New("command output is required")
	}
	stream := &projectJSON{output: o.Output, operation: "setup.prepare"}
	uncertain := false
	defer func() {
		if err != nil {
			err = errors.Join(err, stream.emit("error", err.Error(), map[string]any{"uncertain": uncertain}))
		}
	}()
	explicitDomain := false
	explicitConfig := false
	globals.Visit(func(f *flag.Flag) {
		if f.Name == "domain" {
			explicitDomain = true
		}
		if f.Name == "config" {
			explicitConfig = true
		}
	})
	if explicitDomain || *domain != "" {
		return true, errors.New("setup prepare is alpha-scoped and does not accept --domain")
	}
	var input SetupPrepareInput
	flags := flag.NewFlagSet("setup prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonMode := flags.Bool("json", false, "structured output")
	flags.StringVar(&input.PackageRoot, "package", "", "extracted current package")
	flags.StringVar(&input.ISOPath, "iso", "", "pinned Ubuntu installer")
	flags.StringVar(&input.CheckerPath, "checker", "", "pinned e2fsck package")
	flags.StringVar(&input.GoBinary, "go", "", "exact Go executable")
	flags.StringVar(&input.ZstdBinary, "zstd", "", "exact zstd executable")
	flags.StringVar(&input.OpenSSLPath, "openssl", "", "SHA-512 OpenSSL executable")
	flags.StringVar(&input.XorrisoPath, "xorriso", "", "xorriso executable")
	if err = flags.Parse(rest[2:]); err != nil {
		return true, err
	}
	if !*jsonMode || len(flags.Args()) != 0 {
		return true, errors.New("setup prepare requires --json and explicit package/iso/checker/go/zstd/openssl/xorriso paths")
	}
	for _, v := range []string{input.PackageRoot, input.ISOPath, input.CheckerPath, input.GoBinary, input.ZstdBinary, input.OpenSSLPath, input.XorrisoPath} {
		if v == "" {
			return true, errors.New("setup prepare requires every asset path")
		}
	}
	if *path == "" && explicitConfig {
		return true, errors.New("configuration path is required")
	}
	if *path == "" {
		*path, err = DefaultConfigPath()
		if err != nil {
			return true, err
		}
	}
	if o.SetupPrepare == nil {
		return true, errors.New("package setup preparation is unavailable")
	}
	var result SetupInspection
	result, uncertain, err = o.SetupPrepare(ctx, *path, input, stream)
	if err != nil {
		return true, err
	}
	return true, stream.emit("result", "", result)
}
