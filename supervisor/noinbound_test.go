package supervisor

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoInboundURLMessage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), []byte(`{"inboundUrl":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := InboundURLFromDir(dir)
	if !errors.Is(err, errNoInbound) {
		t.Fatalf("got %v", err)
	}
}

func TestNoInbound(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-sleep:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\nCMD [\"sleep\", \"3600\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	acl := filepath.Join(t.TempDir(), "squid.conf")
	conf := "" +
		"http_port 127.0.0.1:3129\n" +
		"http_port 0.0.0.0:3128 intercept\n" +
		"visible_hostname hermetarium\n" +
		"pid_filename /log/squid.pid\n" +
		"coredump_dir /tmp\n" +
		"cache_log /log/cache.log\n" +
		"logformat htm %ts.%03tu %6tr %>a %Ss/%03>Hs %<st %rm %ru %[un %Sh/%<a %mt %>lp\n" +
		"access_log stdio:/log/access.log htm\n" +
		"logfile_rotate 0\n" +
		"shutdown_lifetime 1 seconds\n" +
		"cache_effective_user proxy\n" +
		"dns_v4_first on\n" +
		"cache deny all\n" +
		"http_access deny all\n"
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := CreateOpts{Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128}

	t.Run("weak", func(t *testing.T) {
		id := newTestID(t)
		t.Cleanup(func() { _ = DestroyWeak(id) })
		inst, err := CreateWeak(root, id, opts)
		if err != nil {
			t.Fatal(err)
		}
		if inst.InboundURL != "" {
			t.Fatalf("inbound url %q", inst.InboundURL)
		}
		assertNoPublishedPorts(t, "htm-gate-"+id, "htm-box-"+id)
		assertRunning(t, "htm-box-"+id)
		assertURLSaysNone(t, id)
	})

	t.Run("strong", func(t *testing.T) {
		id := newTestID(t)
		t.Cleanup(func() { _ = DestroyStrong(id) })
		inst, err := CreateStrong(root, id, opts)
		if err != nil {
			t.Fatal(err)
		}
		if inst.InboundURL != "" {
			t.Fatalf("inbound url %q", inst.InboundURL)
		}
		assertNoPublishedPorts(t, "htm-fc-"+id, "htm-gate-"+id)
		assertRunning(t, "htm-fc-"+id)
		waitGuestCmd(t, "htm-fc-"+id, "sleep")
		assertURLSaysNone(t, id)
	})
}

func newTestID(t *testing.T) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertNoPublishedPorts(t *testing.T, containers ...string) {
	t.Helper()
	for _, c := range containers {
		out, err := Docker(10*time.Second, "inspect", "-f", "{{json .HostConfig.PortBindings}}", c)
		if err != nil {
			t.Fatal(err)
		}
		out = strings.TrimSpace(out)
		if out != "{}" && out != "null" {
			t.Fatalf("%s published %s", c, out)
		}
	}
}

func assertRunning(t *testing.T, container string) {
	t.Helper()
	out, err := Docker(10*time.Second, "inspect", "-f", "{{.State.Running}}", container)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Fatalf("%s is not running: %s", container, out)
	}
}

func assertURLSaysNone(t *testing.T, id string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := Run([]string{"url", id})
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	if code != 0 || !bytes.Contains(b, []byte(errNoInbound.Error())) {
		t.Fatalf("code=%d out=%q", code, b)
	}
}

func waitGuestCmd(t *testing.T, helper, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		logs = helperLogs(helper)
		if strings.Contains(logs, "FC_CMD=") {
			if !strings.Contains(logs, want) {
				t.Fatalf("guest command %q does not contain %q", logs, want)
			}
			return
		}
		if !helperRunning(helper) {
			t.Fatalf("helper exited\n%s", logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("guest did not start\n%s", logs)
}
