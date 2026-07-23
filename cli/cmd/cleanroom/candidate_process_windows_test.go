//go:build windows

package main

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCandidateProcessTreeStartsAndCloses(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
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
	if err := tree.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeCandidateProcessThreadsRejectsMissingPrimaryThread(t *testing.T) {
	previousCreate, previousFirst, previousNext := createThreadSnapshot, firstThread, nextThread
	t.Cleanup(func() { createThreadSnapshot, firstThread, nextThread = previousCreate, previousFirst, previousNext })
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(event) })
	createThreadSnapshot = func(uint32, uint32) (windows.Handle, error) { return event, nil }
	firstThread = func(_ windows.Handle, entry *windows.ThreadEntry32) error {
		entry.OwnerProcessID = 7
		return nil
	}
	nextThread = func(windows.Handle, *windows.ThreadEntry32) error { return windows.ERROR_NO_MORE_FILES }
	if err := resumeCandidateProcessThreads(42); err == nil || errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		t.Fatalf("missing-thread error = %v", err)
	}
}
