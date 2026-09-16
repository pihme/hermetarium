//go:build live

package agentd

import (
	"os"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
	"github.com/pihme/hermetarium/tests/harness"
)

func TestAgentdLive(t *testing.T) {
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
	inst, err := harness.CreateWeakAgentd(root, id, false)
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
