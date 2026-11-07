package main

import (
	"slices"
	"strings"
	"testing"
)

func TestCmdFirstTurn(t *testing.T) {
	cases := []struct {
		harness string
		want    []string
	}{
		{"claude", []string{"-p", "--output-format", "text", "--dangerously-skip-permissions", "hi"}},
		{"grok", []string{"--always-approve", "-p", "hi"}},
		{"dsh", []string{"--profile", "headless", "hi"}},
	}
	for _, c := range cases {
		s := &server{harness: c.harness}
		cmd, err := s.cmd("hi")
		if err != nil {
			t.Fatalf("%s: %v", c.harness, err)
		}
		if !slices.Equal(cmd.Args[1:], c.want) {
			t.Fatalf("%s: got %v want %v", c.harness, cmd.Args[1:], c.want)
		}
		if c.harness == "claude" && !strings.Contains(strings.Join(cmd.Env, "\n"), "CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1") {
			t.Fatalf("claude env missing skip-permissions variable: %v", cmd.Env)
		}
	}
}

// claude and grok carry --continue once a turn has already run, so a later
// POST continues the same session (SPEC.md §9c). dsh --profile headless has
// no resume flag and stays one-shot per turn regardless of s.started.
func TestCmdContinuesSession(t *testing.T) {
	cases := []struct {
		harness string
		want    []string
	}{
		{"claude", []string{"--continue", "-p", "--output-format", "text", "--dangerously-skip-permissions", "hi"}},
		{"grok", []string{"--always-approve", "-p", "hi", "--continue"}},
		{"dsh", []string{"--profile", "headless", "hi"}},
	}
	for _, c := range cases {
		s := &server{harness: c.harness, started: true}
		cmd, err := s.cmd("hi")
		if err != nil {
			t.Fatalf("%s: %v", c.harness, err)
		}
		if !slices.Equal(cmd.Args[1:], c.want) {
			t.Fatalf("%s: got %v want %v", c.harness, cmd.Args[1:], c.want)
		}
	}
}

func TestCmdUnknownHarness(t *testing.T) {
	s := &server{harness: "other"}
	if _, err := s.cmd("hi"); err == nil {
		t.Fatal("expected error for unknown HARNESS")
	}
}
