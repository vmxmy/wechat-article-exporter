//go:build unix

package main

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestUnixCandidateProcessTreeCloseTerminatesProcessGroupIdempotently(t *testing.T) {
	command := exec.Command("sh", "-c", "sleep 30 & wait")
	configureCandidateProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	tree, err := attachCandidateProcessTree(command.Process)
	if err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	errorsSeen := make([]error, 4)
	for index := range errorsSeen {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			errorsSeen[index] = tree.Close()
		}(index)
	}
	waitGroup.Wait()
	for index, closeErr := range errorsSeen {
		if closeErr != nil {
			t.Fatalf("Close[%d] = %v", index, closeErr)
		}
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case waitErr := <-done:
		var exitError *exec.ExitError
		if !errors.As(waitErr, &exitError) {
			t.Fatalf("Wait = %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("candidate process group was not terminated")
	}
	if err := syscall.Kill(-command.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains after Close: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("repeated Close = %v", err)
	}
}

func TestUnixCandidateProcessTreeCloseToleratesExitedProcess(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 0")
	configureCandidateProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	tree, err := attachCandidateProcessTree(command.Process)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	tree.MarkExited()
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after exit = %v", err)
	}
}
