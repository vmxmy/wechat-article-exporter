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

func TestAssembleSetRequiresExactlyFiveReceipts(t *testing.T) {
	err := assembleSetCommand([]string{
		"--output", "set.json", "--repository", "owner/repository", "--tag", "wechat-article-v2.0.1",
		"--commit", "0123456789012345678901234567890123456789", "--version", "2.0.1",
		"--checksum-manifest-sha256", strings.Repeat("a", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "exactly five receipt") {
		t.Fatalf("assembleSetCommand error = %v", err)
	}
}
