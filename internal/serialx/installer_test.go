package serialx

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestInstallerRuntimeFixedOrderedExchange(t *testing.T) {
	host, guest := net.Pipe()
	defer guest.Close()
	runtime := newRuntimeKind(host, "attempt-1", "run-1")
	prompt := "boxwarden@boxwarden-task0-run-1:"
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err == nil {
		t.Fatal("finalizer accepted before installed prompt")
	}
	if err := runtime.InstallerWaitFor(ctx, "arbitrary marker"); err == nil {
		t.Fatal("arbitrary marker accepted")
	}
	if _, err := guest.Write([]byte(prompt)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerSendLine(ctx, "echo arbitrary"); err == nil {
		t.Fatal("arbitrary serial command accepted")
	}
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err == nil {
		t.Fatal("finalizer accepted before guest preparation")
	}
	reader := bufio.NewReader(guest)
	command := make(chan string, 1)
	go func() { line, _ := reader.ReadString('\n'); command <- line }()
	if err := runtime.InstallerSendLine(ctx, installerPrepare); err != nil {
		t.Fatal(err)
	}
	if got := <-command; got != installerPrepare+"\n" {
		t.Fatalf("prepare bytes = %q", got)
	}
	if err := runtime.InstallerSendLine(ctx, installerPrepare); err == nil {
		t.Fatal("second prepare accepted")
	}
	if _, err := guest.Write([]byte(installerPreparedMarker)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, installerPreparedMarker); err != nil {
		t.Fatal(err)
	}
	go func() { line, _ := reader.ReadString('\n'); command <- line }()
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err != nil {
		t.Fatal(err)
	}
	if got := <-command; got != installerFinalizer+"\n" {
		t.Fatalf("finalizer bytes = %q", got)
	}
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err == nil {
		t.Fatal("second finalizer accepted")
	}
	if _, err := guest.Write([]byte(installerReadyMarker)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, installerReadyMarker); err != nil {
		t.Fatal(err)
	}
	go func() { line, _ := reader.ReadString('\n'); command <- line }()
	if err := runtime.InstallerSendLine(ctx, installerPoweroff); err != nil {
		t.Fatal(err)
	}
	if got := <-command; got != installerPoweroff+"\n" {
		t.Fatalf("poweroff bytes = %q", got)
	}
}

func TestInstallerRuntimeIgnoresMarkerBeforeFinalizer(t *testing.T) {
	host, guest := net.Pipe()
	defer guest.Close()
	runtime := newRuntimeKind(host, "attempt-1", "run-1")
	prompt := "boxwarden@boxwarden-task0-run-1:"
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := guest.Write([]byte(prompt + installerPreparedMarker + installerReadyMarker)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(guest)
	readCommand := make(chan string, 2)
	go func() {
		for range 2 {
			line, _ := reader.ReadString('\n')
			readCommand <- line
		}
	}()
	if err := runtime.InstallerSendLine(ctx, installerPrepare); err != nil {
		t.Fatal(err)
	}
	if got := <-readCommand; got != installerPrepare+"\n" {
		t.Fatalf("prepare bytes = %q", got)
	}
	if _, err := guest.Write([]byte(installerPreparedMarker)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, installerPreparedMarker); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err != nil {
		t.Fatal(err)
	}
	if got := <-readCommand; got != installerFinalizer+"\n" {
		t.Fatalf("finalizer bytes = %q", got)
	}
	short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if err := runtime.InstallerWaitFor(short, installerReadyMarker); err == nil {
		t.Fatal("pre-finalizer marker accepted")
	}
}

func TestInstallerRuntimeIgnoresPrepareMarkerBeforeCommand(t *testing.T) {
	host, guest := net.Pipe()
	defer guest.Close()
	runtime := newRuntimeKind(host, "attempt-1", "run-1")
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	prompt := "boxwarden@boxwarden-task0-run-1:"
	if _, err := guest.Write([]byte(prompt + installerPreparedMarker)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	go bufio.NewReader(guest).ReadString('\n')
	if err := runtime.InstallerSendLine(ctx, installerPrepare); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if err := runtime.InstallerWaitFor(short, installerPreparedMarker); err == nil {
		t.Fatal("pre-command preparation marker accepted")
	}
}

func TestInstallerRuntimeReportsGuestPreparationFailureWithoutWaitingForDeadline(t *testing.T) {
	host, guest := net.Pipe()
	defer guest.Close()
	runtime := newRuntimeKind(host, "attempt-1", "run-1")
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	prompt := "boxwarden@boxwarden-task0-run-1:"
	if _, err := guest.Write([]byte(prompt)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	go bufio.NewReader(guest).ReadString('\n')
	if err := runtime.InstallerSendLine(ctx, installerPrepare); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.Write([]byte("boxwarden recipe prepare failed: apt-install exited nonzero\n")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, installerPreparedMarker); err == nil || !strings.Contains(err.Error(), "guest preparation reported failure") {
		t.Fatalf("guest preparation failure was not reported directly: %v", err)
	}
	if err := runtime.InstallerSendLine(ctx, installerFinalizer); err == nil {
		t.Fatal("finalizer accepted after guest preparation failure")
	}
}

func TestInstallerRuntimeBindsFreshExactRunPrompt(t *testing.T) {
	for _, invalid := range []string{"run-3", "run-0123456789AB", "run-0123456789a", "run-0123456789abc"} {
		if validInstallerRunID(invalid) {
			t.Fatalf("invalid run ID accepted: %q", invalid)
		}
	}
	const runID = "run-0123456789ab"
	if !validInstallerRunID(runID) {
		t.Fatal("fresh run ID rejected")
	}
	host, guest := net.Pipe()
	defer guest.Close()
	runtime := newRuntimeKind(host, "attempt-1", runID)
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	prompt := "boxwarden@boxwarden-task0-" + runID + ":"
	if err := runtime.InstallerWaitFor(ctx, "boxwarden@boxwarden-task0-run-1:"); err == nil {
		t.Fatal("unbound legacy prompt accepted")
	}
	if _, err := guest.Write([]byte(prompt[:18])); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.Write([]byte(prompt[18:])); err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallerWaitFor(ctx, prompt); err != nil {
		t.Fatal(err)
	}
}
