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
	inst, err := supervisor.CreateWeak(root, id, supervisor.WithTestLabel(t, supervisor.CreateOpts{
		Image:  harness.EchoImage,
		ACL:    filepath.Join(root, "examples", "echo", "squid.conf"),
		MemMiB: 128, DiskMB: 256,
	}))
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
	inst, err := harness.CreateWeakEcho(t, root, id)
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

	// The gate, unlike the box, must reach the Internet (Squid forwards to the vendor).
	if _, err := supervisor.Docker(20*time.Second, "exec", inst.Gate, "bash", "-c", "echo > /dev/tcp/1.1.1.1/443"); err != nil {
		t.Fatalf("weak gate cannot reach the Internet: %v", err)
	}
	t.Log("weak wall: gate reaches the Internet")

	harness.AssertNeighborBlocked(t, inst.Gate)
	t.Log("weak wall: a neighbor container on the default bridge cannot reach the gate")

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
	inst, err := harness.CreateStrongEcho(t, root, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.DestroyStrong(id)
		t.Log("strong wall: destroyed")
	}()

	priv, err := supervisor.Docker(10*time.Second, "inspect", "-f", "{{.HostConfig.Privileged}}", inst.Helper)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(priv) != "false" {
		t.Fatalf("helper is privileged: %q", priv)
	}

	serial, waitErr := supervisor.WaitStrongSerial(inst, 90*time.Second)
	for _, line := range strings.Split(serial, "\n") {
		if strings.Contains(line, "GUEST_") || strings.Contains(line, "LEAK") {
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
	if !strings.HasPrefix(m[1], "6.1.188") {
		t.Fatalf("strong wall: guest kernel %s is not the pinned 6.1.188", m[1])
	}
	t.Logf("strong wall: guest kernel %s ≠ host %s", m[1], hostRelease)
	// rng_current is the virtio-rng device. A getrandom return is not: this
	// kernel can seed itself from the CPU with no entropy device.
	rng := regexp.MustCompile(`GUEST_RNG=(\S+)`).FindStringSubmatch(serial)
	if rng == nil || !strings.Contains(rng[1], "virtio_rng") {
		got := "missing"
		if rng != nil {
			got = rng[1]
		}
		t.Fatalf("strong wall: rng_current %s is not virtio_rng\n%s", got, tail(serial, 2000))
	}
	t.Logf("strong wall: rng_current %s", rng[1])
	// Echo has busybox, so the call is made. Skipped is not this image.
	if !strings.Contains(serial, "GUEST_GETRANDOM_OK") {
		t.Fatalf("strong wall: getrandom did not return within 2s\n%s", tail(serial, 2000))
	}
	t.Log("strong wall: getrandom returned")
	if !strings.Contains(serial, "LEAK_FAIL") {
		t.Fatalf("strong wall: leak did not fail\n%s", tail(serial, 2000))
	}
	t.Log("strong wall: direct egress failed (fail-closed)")
	harness.AssertNeighborBlocked(t, inst.Helper)
	t.Log("strong wall: a neighbor container on the default bridge cannot reach the gate")
	if err := supervisor.SyncInstanceLog(inst.Dir); err != nil {
		t.Fatal(err)
	}
	echo.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
