package supervisor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCPUs(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-cpus:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\n" +
		"RUN mkdir -p /www \\\n" +
		" && echo ok > /www/index.html \\\n" +
		" && printf '%s\\n' '#!/bin/sh' 'echo CPUS=$(grep -c processor /proc/cpuinfo)' 'exec httpd -f -p 8080 -h /www' > /start.sh \\\n" +
		" && chmod +x /start.sh\n" +
		"CMD [\"/start.sh\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	acl := filepath.Join(root, "examples", "echo", "squid.conf")
	opts := CreateOpts{Image: image, ACL: acl, CPUs: 4, MemMiB: 256, DiskMB: 256}

	t.Run("weak", func(t *testing.T) {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = DestroyWeak(id) })
		if _, err := CreateWeak(root, id, opts); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := ExecWeak(&WeakInstance{Box: "htm-box-" + id}, []string{"cat", "/sys/fs/cgroup/cpu.max"})
		if code != 0 {
			t.Fatalf("cpu.max code=%d %s %s", code, stdout, stderr)
		}
		if got := cpuQuota(stdout); got != 4 {
			t.Fatalf("cpu quota %d from %q", got, stdout)
		}
	})

	t.Run("strong", func(t *testing.T) {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = DestroyStrong(id) })
		inst, err := CreateStrong(root, id, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(helperLogs(inst.Helper), "CPUS=4") {
			waitGuestCPUs(t, inst.Helper, "CPUS=4")
		}
	})
}

func cpuQuota(out string) int {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return 0
	}
	quota, err1 := strconv.Atoi(fields[0])
	period, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil || period == 0 {
		return 0
	}
	return quota / period
}

func waitGuestCPUs(t *testing.T, helper, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		logs = helperLogs(helper)
		if strings.Contains(logs, want) {
			return
		}
		if strings.Contains(logs, "CPUS=") && !strings.Contains(logs, want) {
			t.Fatalf("guest CPUs %q", logs)
		}
		if !helperRunning(helper) {
			t.Fatalf("helper exited\n%s", logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("guest did not report CPUs\n%s", logs)
}

func TestSparseRootDisk(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureImageExists(BusyboxImage); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	small, err := EnsureImageRootfs(root, BusyboxImage, 1024)
	if err != nil {
		t.Fatal(err)
	}
	smallDur := time.Since(start)
	start = time.Now()
	big, err := EnsureImageRootfs(root, BusyboxImage, 51200)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(big) })
	bigDur := time.Since(start)
	st, err := os.Stat(big)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 51200*1024*1024 {
		t.Fatalf("logical size %d", st.Size())
	}
	alloc, err := allocatedBytes(big)
	if err != nil {
		t.Fatal(err)
	}
	if alloc > 64<<20 {
		t.Fatalf("50GB disk allocated %d bytes", alloc)
	}
	if bigDur > smallDur+15*time.Second {
		t.Fatalf("50GB took %s, 1GB took %s (%s)", bigDur, smallDur, small)
	}
}

func allocatedBytes(path string) (int64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Blocks * 512, nil
}
