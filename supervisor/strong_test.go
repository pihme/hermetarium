package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

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
