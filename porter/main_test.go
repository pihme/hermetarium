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
		{"opencode", []string{"run", "--auto", "--", "hi"}},
		{"pi", []string{"-p", "--model", "hermetarium/mock", "hi"}},
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
		if c.harness == "opencode" {
			env := strings.Join(cmd.Env, "\n")
			if strings.Contains(env, "OPENCODE_DISABLE_AUTOUPDATE=1") {
				t.Fatal("porter disables OpenCode autoupdate")
			}
			if !strings.Contains(env, "OPENCODE_DISABLE_MODELS_FETCH=1") {
				t.Fatalf("opencode env missing fetch lock: %v", cmd.Env)
			}
		}
		if c.harness == "pi" {
			if !strings.Contains(strings.Join(cmd.Env, "\n"), "PI_TELEMETRY=0") {
				t.Fatalf("pi env missing telemetry off: %v", cmd.Env)
			}
		}
	}
}

// claude, grok, and opencode carry --continue once a turn has already run, so a later
// POST continues the same session (SPEC.md §9c).
func TestCmdContinuesSession(t *testing.T) {
	cases := []struct {
		harness string
		want    []string
	}{
		{"claude", []string{"--continue", "-p", "--output-format", "text", "--dangerously-skip-permissions", "hi"}},
		{"grok", []string{"--always-approve", "-p", "hi", "--continue"}},
		{"opencode", []string{"run", "--auto", "--continue", "--", "hi"}},
		{"pi", []string{"-p", "--model", "hermetarium/mock", "--continue", "hi"}},
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
