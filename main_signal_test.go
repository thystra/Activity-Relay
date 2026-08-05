package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

const serverSignalHelperEnvironment = "ACTIVITY_RELAY_SERVER_SIGNAL_HELPER"

func TestServerLifecycleContextReceivesSIGTERM(t *testing.T) {
	if os.Getenv(serverSignalHelperEnvironment) == "1" {
		ctx, stop := serverLifecycleContext(context.Background())
		defer stop()
		fmt.Fprintln(os.Stdout, "ready")
		select {
		case <-ctx.Done():
			os.Exit(0)
		case <-time.After(15 * time.Second):
			os.Exit(2)
		}
	}

	command := exec.Command(os.Args[0], "-test.run=^TestServerLifecycleContextReceivesSIGTERM$")
	command.Env = append(os.Environ(), serverSignalHelperEnvironment+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := false
	t.Cleanup(func() {
		if !exited && command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	ready := make(chan string, 1)
	scanErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
			return
		}
		scanErr <- scanner.Err()
	}()

	select {
	case line := <-ready:
		if line != "ready" {
			t.Fatalf("helper readiness = %q", line)
		}
	case err := <-scanErr:
		t.Fatalf("helper readiness failed: %v; stderr=%s", err, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("helper did not become ready; stderr=%s", stderr.String())
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		exited = true
		if err != nil {
			t.Fatalf("helper exit = %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("helper did not exit after SIGTERM; stderr=%s", stderr.String())
	}
}
