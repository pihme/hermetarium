package inhabitants

import (
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
	"github.com/pihme/hermetarium/tests/harness"
)

func TestOfficialCLIs(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("claude-code", func(t *testing.T) {
		probeOfficial(t, root, supervisor.ExampleClaude, []string{"/usr/local/bin/claude", "--version"}, supervisor.ClaudeHost)
	})
	t.Run("grok-build", func(t *testing.T) {
		probeOfficial(t, root, supervisor.ExampleGrok, []string{"/usr/local/bin/grok", "--version"}, supervisor.GrokHost)
	})
	t.Run("deepseek-harness", func(t *testing.T) {
		probeOfficial(t, root, supervisor.ExampleDeepseek, []string{"/usr/local/bin/dsh", "--help"}, supervisor.DeepseekHost)
	})
}

func probeOfficial(t *testing.T, root, name string, versionCmd []string, vendorHost string) {
	t.Helper()
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inhabitant %s weak %s", name, id)
	inst, err := supervisor.CreateWeakInhabitant(root, id, name)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyWeak(id)

	out, errOut, code := supervisor.ExecWeak(inst, versionCmd)
	if code != 0 {
		t.Fatalf("official CLI missing: %s %s", out, errOut)
	}
	t.Logf("official CLI: %s", strings.TrimSpace(out+" "+errOut))
	harness.AssertNoKeyInBox(t, inst)

	got, err := supervisor.CallTimeout(inst.InboundURL, "Run id -u and reply with only the number.", 20*time.Second)
	if err != nil {
		t.Logf("mock vendor API rejected by official CLI (expected): %v %s", err, got)
		t.Log("chat/root through the real model is tests/inhabitants live suite (make test-live)")
		return
	}
	t.Logf("mock accepted: %q", got)
	harness.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath, vendorHost)
}
