package supervisor

import (
	"bytes"
	"encoding/json"
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
	write := func(name, body string) string {
		t.Helper()
		sub := filepath.Join(dir, name)
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "instance.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return sub
	}

	_, err := InboundURLFromDir(write("flag", `{"noInbound":true,"inboundUrl":""}`))
	if !errors.Is(err, errNoInbound) || err.Error() != "this habitat has no inbound address" {
		t.Fatalf("no-inbound: %v", err)
	}

	got, err := InboundURLFromDir(write("url", `{"noInbound":false,"inboundUrl":"http://127.0.0.1:9/"}`))
	if err != nil || got != "http://127.0.0.1:9/" {
		t.Fatalf("url: %q %v", got, err)
	}

	_, err = InboundURLFromDir(write("empty", `{"inboundUrl":""}`))
	if !errors.Is(err, errEmptyInboundURL) {
		t.Fatalf("empty url: %v", err)
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

func TestNoInboundRejectedMachineConfig(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("/dev/kvm not available")
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureImageExists(BusyboxImage); err != nil {
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
	id := newTestID(t)
	t.Cleanup(func() { _ = DestroyStrong(id) })
	_, err = CreateStrong(root, id, CreateOpts{
		Image: BusyboxImage, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128,
		AfterGate: func(string, string) error {
			// Firecracker reads vm.json only after squid.pid appears, which
			// is after this hook. vcpu_count 0 is rejected before the guest
			// can print GUEST_UNAME=.
			p := filepath.Join(VarDir(root), id, "vm.json")
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			var vm map[string]any
			if err := json.Unmarshal(b, &vm); err != nil {
				return err
			}
			mc, ok := vm["machine-config"].(map[string]any)
			if !ok {
				return errors.New("vm.json has no machine-config")
			}
			mc["vcpu_count"] = 0
			raw, err := json.MarshalIndent(vm, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(p, append(raw, '\n'), 0o644)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "guest did not boot") {
		t.Fatal(err)
	}
	fc, readErr := os.ReadFile(filepath.Join(VarDir(root), id, "fc.log"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(fc)), "\n")
	last := lines[len(lines)-1]
	if last == "" || !strings.Contains(err.Error(), last) {
		t.Fatalf("error missing fc.log\nerr=%v\nfc.log=%s", err, fc)
	}
	left, err := Docker(15*time.Second, "ps", "-a", "--filter", "name="+id, "--format", "{{.Names}}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(left) != "" {
		t.Fatalf("containers left: %s", left)
	}
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
