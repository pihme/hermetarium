package echo

import (
	"testing"

	"hermetarium/supervisor"
)

// Exercise is the inbound echo example: one call-and-wait, then a second
// request to the same running server. Traffic must show up as direction=in.
func Exercise(t *testing.T, inboundURL, dir, logPath string) {
	t.Helper()
	got, err := supervisor.Call(inboundURL, "ping")
	if err != nil || got != "ping" {
		t.Fatalf("echo call-and-wait: got %q err %v", got, err)
	}
	t.Log("echo example: call-and-wait ok")
	got, err = supervisor.Call(inboundURL, "pong")
	if err != nil || got != "pong" {
		t.Fatalf("echo running server: got %q err %v", got, err)
	}
	t.Log("echo example: running server ok")
	if err := supervisor.SyncInstanceLog(dir); err != nil {
		t.Fatal(err)
	}
	if !supervisor.LogHasDirection(logPath, "in") {
		t.Fatalf("echo I/O log missing inbound in %s", logPath)
	}
	t.Log("echo example: I/O log recorded inbound")
}
