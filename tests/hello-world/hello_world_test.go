package helloworld

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
	"github.com/pihme/hermetarium/tests/echo"
	"github.com/pihme/hermetarium/tests/harness"
)

func TestHelloWorld(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("weak", func(t *testing.T) { testWeak(t, root) })
	t.Run("strong", func(t *testing.T) { testStrong(t, root) })
	t.Run("image", func(t *testing.T) { testArbitraryImage(t, root) })
}

func testArbitraryImage(t *testing.T, root string) {
	if err := harness.EnsureEchoImage(root); err != nil {
		t.Fatal(err)
	}
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("arbitrary --image %s create %s", harness.EchoImage, id)
	inst, err := supervisor.CreateWeak(root, id, supervisor.CreateOpts{
		Image:  harness.EchoImage,
		ACL:    filepath.Join(root, "examples", "echo", "squid.conf"),
		MemMiB: 128, DiskMB: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyWeak(id)
	echo.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath)
}

func testWeak(t *testing.T, root string) {
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("weak wall: create %s", id)
	inst, err := harness.CreateWeakEcho(root, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.DestroyWeak(id)
		t.Log("weak wall: destroyed")
	}()

	out, _, code := supervisor.ExecWeak(inst, []string{"ping", "-c", "1", "-W", "2", "1.1.1.1"})
	if code == 0 {
		t.Fatalf("weak leak: expected failure, got code 0 %s", out)
	}
	t.Log("weak wall: direct egress failed (fail-closed)")

	out, errs, code := supervisor.ExecWeak(inst, []string{
		"curl", "-sS", "--max-time", "8", "http://" + supervisor.ProbeHost + "/hello",
	})
	if code != 0 || !strings.Contains(out, "hermetarium-ok") {
		t.Fatalf("weak probe failed: %s %s", errs, out)
	}
	t.Log("weak wall: probe through gate ok")

	if err := supervisor.SyncInstanceLog(inst.Dir); err != nil {
		t.Fatal(err)
	}
	if !supervisor.LogHasDestination(inst.LogPath, supervisor.ProbeHost) &&
		!supervisor.LogHasDestination(inst.LogPath, inst.GateIP) {
		t.Fatalf("weak I/O log missing probe destination in %s", inst.LogPath)
	}
	t.Log("weak wall: I/O log recorded traffic")
	echo.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath)
}

func testStrong(t *testing.T, root string) {
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("strong wall: create %s", id)
	inst, err := harness.CreateStrongEcho(root, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.DestroyStrong(id)
		t.Log("strong wall: destroyed")
	}()

	serial, waitErr := supervisor.WaitStrongSerial(inst, 90*time.Second)
	for _, line := range strings.Split(serial, "\n") {
		if strings.Contains(line, "GUEST_") || strings.Contains(line, "PROBE") || strings.Contains(line, "LEAK") {
			t.Log(line)
		}
	}
	if waitErr != nil {
		t.Fatalf("%v\n%s", waitErr, tail(serial, 4000))
	}
	host, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		t.Fatal(err)
	}
	hostRelease := strings.TrimSpace(string(host))
	m := regexp.MustCompile(`GUEST_UNAME=(\S+)`).FindStringSubmatch(serial)
	if m == nil {
		t.Fatalf("strong wall: no GUEST_UNAME in serial\n%s", tail(serial, 2000))
	}
	if m[1] == hostRelease {
		t.Fatalf("strong wall: guest kernel %s equals host %s", m[1], hostRelease)
	}
	t.Logf("strong wall: guest kernel %s ≠ host %s", m[1], hostRelease)
	if !strings.Contains(serial, "PROBE_OK") {
		t.Fatalf("strong wall: probe failed\n%s", tail(serial, 2000))
	}
	t.Log("strong wall: probe through TAP/Squid ok")
	if !strings.Contains(serial, "LEAK_FAIL") {
		t.Fatalf("strong wall: leak did not fail\n%s", tail(serial, 2000))
	}
	t.Log("strong wall: direct egress failed (fail-closed)")
	if err := supervisor.SyncInstanceLog(inst.Dir); err != nil {
		t.Fatal(err)
	}
	if !supervisor.LogHasDestination(inst.LogPath, supervisor.ProbeHost) &&
		!supervisor.LogHasDestination(inst.LogPath, "172.16.0.1") {
		t.Fatalf("strong I/O log missing probe in %s", inst.LogPath)
	}
	t.Log("strong wall: I/O log recorded traffic")
	echo.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
