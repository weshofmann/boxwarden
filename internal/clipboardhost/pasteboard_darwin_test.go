//go:build darwin && cgo

package clipboardhost

import (
	"bytes"
	"context"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"os"
	"testing"
	"time"
)

func TestPrivatePasteboardExactTextAndInvalidPreservation(t *testing.T) {
	board := privateBoardForTest(fmt.Sprintf("org.boxwarden.test.%d.%d", os.Getpid(), time.Now().UnixNano()))
	defer releasePrivateBoard(board)
	if _, err := board.ReadText(t.Context()); err != clipboardx.ErrUnavailable {
		t.Fatalf("empty private board: %v", err)
	}
	for _, data := range [][]byte{{}, []byte(" \t☃\r\n\n"), bytes.Repeat([]byte("a"), clipboardx.MaxTextBytes)} {
		out, err := board.WriteText(t.Context(), data)
		if err != nil || out != clipboardx.Committed {
			t.Fatalf("write %v %v", out, err)
		}
		got, err := board.ReadText(t.Context())
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("text roundtrip failed")
		}
	}
	good := []byte("synthetic stable text")
	board.WriteText(t.Context(), good)
	for _, bad := range [][]byte{[]byte{0}, []byte{255}, bytes.Repeat([]byte("a"), clipboardx.MaxTextBytes+1)} {
		out, err := board.WriteText(t.Context(), bad)
		if err == nil || out != clipboardx.Unchanged {
			t.Fatal("invalid write accepted")
		}
		got, err := board.ReadText(t.Context())
		if err != nil || !bytes.Equal(got, good) {
			t.Fatal("invalid write mutated board")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err := board.WriteText(ctx, []byte("replacement"))
	if out != clipboardx.Unchanged || err != clipboardx.ErrCancelled {
		t.Fatal("cancelled write")
	}
	got, _ := board.ReadText(t.Context())
	if !bytes.Equal(got, good) {
		t.Fatal("cancelled write mutated board")
	}
}

func TestPrivatePromisedPasteboardReadHonorsDeadline(t *testing.T) {
	name := fmt.Sprintf("org.boxwarden.test.delayed.%d.%d", os.Getpid(), time.Now().UnixNano())
	board := privateBoardForTest(name)
	defer releasePrivateBoard(board)
	seedPromised(name)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := board.ReadText(ctx)
	if err != clipboardx.ErrCancelled || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("promised read deadline: elapsed=%v err=%v", time.Since(start), err)
	}
}
