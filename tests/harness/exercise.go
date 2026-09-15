package harness

import (
	"strings"
	"testing"

	"hermetarium/supervisor"
)

// Exercise is the mock coding-agent check: two turns, root via bash, inbound log.
func Exercise(t *testing.T, inboundURL, dir, logPath, vendorHost string) {
	t.Helper()
	got, err := supervisor.Call(inboundURL, "hello")
	if err != nil {
		t.Fatalf("turn 1: %v %s", err, got)
	}
	t.Logf("turn 1: %q", got)
	got, err = supervisor.Call(inboundURL, "root")
	if err != nil {
		t.Fatalf("turn 2: %v %s", err, got)
	}
	t.Logf("turn 2: %q", got)
	if !strings.Contains(got, "0") {
		t.Fatalf("expected uid 0 in reply, got %q", got)
	}
	t.Log("coding-agent: open session + root shell ok")
	if err := supervisor.SyncInstanceLog(dir); err != nil {
		t.Fatal(err)
	}
	if !supervisor.LogHasDirection(logPath, "in") {
		t.Fatalf("missing inbound in %s", logPath)
	}
	if vendorHost != "" && !supervisor.LogHasDestination(logPath, vendorHost) {
		t.Fatalf("I/O log missing vendor host %s in %s", vendorHost, logPath)
	}
	t.Log("coding-agent: I/O log recorded")
}

func AssertNoKeyInBox(t *testing.T, inst *supervisor.WeakInstance) {
	t.Helper()
	out, _, _ := supervisor.ExecWeak(inst, []string{"sh", "-c", "printenv; test -f /usr/local/bin/agentd"})
	if strings.Contains(out, supervisor.TestVendorKey) {
		t.Fatalf("test vendor key leaked into inhabitant env")
	}
}
