//go:build live

package claudecode

import (
	"os"
	"strings"
	"testing"

	"hermetarium/supervisor"
)

func TestClaudeCodeLive(t *testing.T) {
	if os.Getenv("HERMETARIUM_ANTHROPIC_API_KEY") == "" {
		t.Skip("HERMETARIUM_ANTHROPIC_API_KEY unset")
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst, err := supervisor.CreateWeakExample(root, id, supervisor.ExampleClaude)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyWeak(id)
	got, err := supervisor.Call(inst.InboundURL, "Run the shell command id -u and reply with only the number.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "0") {
		t.Fatalf("live reply %q", got)
	}
}
