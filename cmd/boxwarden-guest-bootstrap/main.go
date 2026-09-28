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
		return fmt.Errorf("usage: boxwarden-guest-bootstrap serial-bootstrap|management|action|clipboard")
	}
	if bootstrapper == nil {
		bootstrapper = guestproto.NewBootstrapper("/", guestproto.ExecRunner{})
	}
	timeout := 30 * time.Second
	if args[0] == "action" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	case "clipboard":
		// Decode on a bounded-lived worker so incomplete SSH stdin cannot keep
		// this fixed helper alive beyond the operation deadline.
		type decodedClipboard struct {
			request guestproto.ClipboardRequest
			payload []byte
			err     error
		}
		decoded := make(chan decodedClipboard, 1)
		go func() {
			request, payload, err := guestproto.DecodeClipboardRequest(input)
			decoded <- decodedClipboard{request, payload, err}
		}()
		var message decodedClipboard
		select {
		case message = <-decoded:
		case <-ctx.Done():
			return fmt.Errorf("clipboard request timeout")
		}
		if message.err != nil {
			return fmt.Errorf("invalid clipboard request")
		}
		response, payload, err := bootstrapper.Clipboard(ctx, message.request, message.payload)
		if err != nil {
			return fmt.Errorf("clipboard target unavailable")
		}
		encoded, err := guestproto.EncodeClipboardResponse(message.request, response, payload)
		if err != nil {
			return fmt.Errorf("invalid clipboard response")
		}
		if err := writeExact(output, encoded); err != nil {
			return fmt.Errorf("clipboard acknowledgement unavailable")
		}
		return nil
	case "action":
		request, err := guestproto.DecodeActionRequest(input)
		if err != nil {
			return err
		}
		receipt, err := bootstrapper.ExecuteAction(ctx, request)
		if err != nil {
			return err
		}
		encoded, err := guestproto.EncodeActionReceipt(request, receipt)
		if err != nil {
			return err
		}
		if err := writeExact(output, encoded); err != nil {
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
