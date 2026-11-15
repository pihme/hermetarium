package fchelper

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	ep := filepath.Join(dir, "entrypoint.sh")
	pack := filepath.Join(dir, "pack-oci.sh")
	for _, p := range []string{ep, pack} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", p, st.Mode())
		}
	}
	b, err := os.ReadFile(ep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "fc-helper") {
		t.Fatalf("entrypoint.sh missing fc-helper marker:\n%s", b)
	}
	if strings.Contains(string(b), "--no-seccomp") {
		t.Fatal("entrypoint.sh disables Firecracker seccomp")
	}
	b, err = os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "rootfs.ext4") {
		t.Fatalf("pack-oci.sh missing rootfs.ext4:\n%s", b)
	}
	if !strings.Contains(string(b), "mke2fs -t ext4 -F -d ") || !strings.Contains(string(b), "lazy_itable_init=1,lazy_journal_init=1") {
		t.Fatal("pack-oci.sh must build a sparse ext4 image with mke2fs -d")
	}
	if strings.Contains(string(b), "umount") {
		t.Fatal("pack-oci.sh mounts the root disk")
	}
	if !strings.Contains(string(b), "/bin/busybox wget") || !strings.Contains(string(b), "timeout 3 curl") {
		t.Fatal("pack-oci.sh CA fetch must not call curl without a timeout")
	}
	if strings.Contains(string(b), "|| echo DATA_MOUNT_FAIL") || !strings.Contains(string(b), "echo DATA_MOUNT_FAIL\n    exit 1") {
		t.Fatal("data mount failure must stop before the inhabitant")
	}
	if !strings.Contains(string(b), "echo DATA_MOUNT_OK") {
		t.Fatal("pack-oci.sh missing DATA_MOUNT_OK")
	}
	if !strings.Contains(string(b), "GUEST_RNG=") || !strings.Contains(string(b), "/fc-getrandom") || !strings.Contains(string(b), "echo GUEST_GETRANDOM_OK") || !strings.Contains(string(b), "echo GUEST_GETRANDOM_SKIPPED") {
		t.Fatal("pack-oci.sh must record virtio-rng and a bounded getrandom")
	}
	if !strings.Contains(string(b), "9b: 75 03") || !strings.Contains(string(b), "jne 0xa0") {
		t.Fatal("pack-oci.sh getrandom listing is not the failure path")
	}
	var blob string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "f0VMRg") {
			blob = line
			break
		}
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	// Listing: jne at file offset 0x9b is 75 03, and 0xa0 is mov rax, 0x3c.
	if len(raw) < 0xa8 || raw[0x9b] != 0x75 || raw[0x9c] != 0x03 || raw[0xa0] != 0x48 || raw[0xa1] != 0xc7 {
		t.Fatalf("getrandom failure path: %x", raw)
	}

	st1, err := os.Stat(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(ep)
	if err != nil {
		t.Fatal(err)
	}
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("second Extract changed mtime of unchanged script")
	}
}
