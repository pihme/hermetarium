package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWaitGuestUname(t *testing.T) {
	t.Run("serial", func(t *testing.T) {
		dir := t.TempDir()
		serial := filepath.Join(dir, "serial.log")
		if err := os.WriteFile(serial, []byte("GUEST_UNAME=6.1.188\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := waitGuestUname(dir, time.Second, func() (string, bool) {
			b, err := os.ReadFile(serial)
			if err != nil {
				t.Fatal(err)
			}
			return string(b), true
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dead", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "serial.log"), []byte("still booting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		const reason = "rejected machine config vcpu_count"
		if err := os.WriteFile(filepath.Join(dir, "fc.log"), []byte(reason+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err := waitGuestUname(dir, time.Minute, func() (string, bool) {
			b, readErr := os.ReadFile(filepath.Join(dir, "serial.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			return string(b), false
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("dead container waited %s", time.Since(start))
		}
		if !strings.Contains(err.Error(), reason) {
			t.Fatal(err)
		}
	})

	t.Run("mount", func(t *testing.T) {
		err := waitGuestUname(t.TempDir(), time.Minute, func() (string, bool) {
			return "DATA_MOUNT_FAIL\n", false
		})
		if err == nil || !strings.Contains(err.Error(), "did not mount") {
			t.Fatal(err)
		}
		if strings.Contains(err.Error(), "guest did not boot") {
			t.Fatal(err)
		}
	})
}

func TestFcHelperDirExtractsWithoutTree(t *testing.T) {
	root := t.TempDir()
	dir, err := fcHelperDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"entrypoint.sh", "pack-oci.sh"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() == 0 || st.Mode()&0o111 == 0 {
			t.Fatalf("%s: size=%d mode=%v", p, st.Size(), st.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "firecracker-helper")); !os.IsNotExist(err) {
		t.Fatalf("extract wrote into tree path: %v", err)
	}
}

func TestEnsureDataDisk(t *testing.T) {
	dir := t.TempDir()
	colon := filepath.Join(dir, "disk:ext4")
	if err := ensureDataDisk(colon, 32); err == nil || !strings.Contains(err.Error(), ":") {
		t.Fatalf("colon: %v", err)
	}
	if _, err := os.Stat(colon); !os.IsNotExist(err) {
		t.Fatalf("colon path created: %v", err)
	}

	if err := ensureDataDisk(dir, 32); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatalf("directory: %v", err)
	}

	bad := filepath.Join(dir, "not-ext4")
	if err := os.WriteFile(bad, []byte("not a filesystem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureDataDisk(bad, 32); err == nil || !strings.Contains(err.Error(), "not ext4") {
		t.Fatalf("non-ext4: %v", err)
	}

	good := filepath.Join(dir, "ok.ext4")
	f, err := os.OpenFile(good, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0x53, 0xEF}, 0x438); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDataDisk(good, 32); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("existing file resized from %d to %d", before.Size(), after.Size())
	}
}

func TestRefusedDataDiskLeavesNoLock(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "not-ext4")
	body := []byte("not a filesystem")
	if err := os.WriteFile(bad, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockDataDisk(bad); err == nil || !strings.Contains(err.Error(), "not ext4") {
		t.Fatalf("non-ext4: %v", err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != "not-ext4" {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Fatalf("directory after refuse: %v", names)
	}
	got, err := os.ReadFile(bad)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("file changed: %q", got)
	}

	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lockDataDisk(sub); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatalf("directory: %v", err)
	}
	if _, err := os.Stat(sub + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock left beside directory: %v", err)
	}

	good := filepath.Join(dir, "ok.ext4")
	f, err := os.OpenFile(good, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0x53, 0xEF}, 0x438); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	held, err := lockDataDisk(good)
	if err != nil {
		t.Fatal(err)
	}
	held.Close()
	if _, err := os.Stat(good + ".lock"); err != nil {
		t.Fatalf("valid ext4 did not lock: %v", err)
	}
}

func TestDataMountFailureIncludesGuestReason(t *testing.T) {
	reason := "mount: mounting /dev/vdb on /data failed: Invalid argument\nDATA_MOUNT_FAIL\n"
	logs := strings.Repeat("kernel line\n", 800) + reason
	err := dataMountFailure(logs)
	msg := err.Error()
	if !strings.Contains(msg, "did not mount") || !strings.Contains(msg, "DATA_MOUNT_FAIL") || !strings.Contains(msg, "Invalid argument") {
		t.Fatal(msg)
	}
	if strings.Contains(msg, strings.Repeat("kernel line\n", 800)) {
		t.Fatal("error included the whole serial log")
	}
	short := dataMountFailure("mount: failed\nDATA_MOUNT_FAIL\n")
	if !strings.Contains(short.Error(), "mount: failed") || !strings.Contains(short.Error(), "DATA_MOUNT_FAIL") {
		t.Fatal(short)
	}
}

func TestDataDiskLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.ext4")
	heldFlock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(heldFlock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := lockDataDisk(path); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("flock held: %v", err)
	}
	heldFlock.Close()

	held, err := lockDataDisk(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDataDiskLockChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "DATA_LOCK_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.Contains(string(out), "SKIP") {
		t.Fatalf("child: %v\n%s", err, out)
	}
}

func TestDataDiskLockChild(t *testing.T) {
	path := os.Getenv("DATA_LOCK_PATH")
	if path == "" {
		t.Skip()
	}
	if _, err := lockDataDisk(path); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("lock: %v", err)
	}
}
