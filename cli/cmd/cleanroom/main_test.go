package main

import (
	"strings"
	"testing"
)

func TestRunCommandRejectsFixtureRelabeledAsLive(t *testing.T) {
	err := runCommand([]string{"--mode", "live"})
	if err == nil || !strings.Contains(err.Error(), "separate controlled-account runner") {
		t.Fatalf("runCommand live error = %v", err)
	}
}

func TestRunCommandRejectsRequireLiveInFixtureRunner(t *testing.T) {
	err := runCommand([]string{"--require-live"})
	if err == nil || !strings.Contains(err.Error(), "cannot be used with the fixture runner") {
		t.Fatalf("runCommand require-live error = %v", err)
	}
}
