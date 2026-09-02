package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: boxwarden-guest-bootstrap serial-bootstrap|management")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bootstrapper := guestproto.NewBootstrapper("/", guestproto.ExecRunner{})
	switch os.Args[1] {
	case "serial-bootstrap":
		request, err := guestproto.DecodeSerialRequest(os.Stdin)
		if err != nil {
			fail(err.Error())
		}
		result, err := bootstrapper.Serial(ctx, request)
		if err != nil {
			fail(err.Error())
		}
		begin, end, err := guestproto.EncodeSerialFrame(request, result)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println(begin)
		fmt.Println(end)
	case "management":
		request, err := guestproto.DecodeManagementRequest(os.Stdin)
		if err != nil {
			fail(err.Error())
		}
		result, err := bootstrapper.Management(ctx, request)
		if err != nil {
			fail(err.Error())
		}
		if len(result) > guestproto.MaxResponseBytes {
			fail("management response exceeds bound")
		}
		_, _ = os.Stdout.Write(result)
		_, _ = os.Stdout.Write([]byte("\n"))
	default:
		fail("unsupported helper mode")
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, "boxwarden-guest-bootstrap:", message); os.Exit(1) }
