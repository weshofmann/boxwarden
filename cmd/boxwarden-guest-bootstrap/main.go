package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "boxwarden-guest-bootstrap:", err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output, _ io.Writer, bootstrapper *guestproto.Bootstrapper) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: boxwarden-guest-bootstrap serial-bootstrap|management")
	}
	if bootstrapper == nil {
		bootstrapper = guestproto.NewBootstrapper("/", guestproto.ExecRunner{})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch args[0] {
	case "serial-bootstrap":
		request, err := guestproto.DecodeSerialRequest(input)
		if err != nil {
			return err
		}
		result, err := bootstrapper.Serial(ctx, request)
		if err != nil {
			return err
		}
		begin, end, err := guestproto.EncodeSerialFrame(request, result)
		if err != nil {
			return err
		}
		if err := writeLine(output, begin); err != nil {
			return err
		}
		return writeLine(output, end)
	case "management":
		request, err := guestproto.DecodeManagementRequest(input)
		if err != nil {
			return err
		}
		result, err := bootstrapper.Management(ctx, request)
		if err != nil {
			return err
		}
		if len(result) > guestproto.MaxResponseBytes {
			return fmt.Errorf("management response exceeds bound")
		}
		if err := writeExact(output, result); err != nil {
			return err
		}
		return writeExact(output, []byte("\n"))
	default:
		return fmt.Errorf("unsupported helper mode")
	}
}
func writeLine(output io.Writer, line string) error {
	return writeExact(output, []byte(line+"\n"))
}

func writeExact(output io.Writer, data []byte) error {
	written, err := output.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
