package fchelper

import (
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
	if !strings.Contains(string(b), "vsock.sock") {
		t.Fatal("entrypoint.sh does not publish the vsock socket")
	}
	b, err = os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "rootfs.ext4") {
		t.Fatalf("pack-oci.sh missing rootfs.ext4:\n%s", b)
	}
	if !strings.Contains(string(b), "/bin/busybox wget") || !strings.Contains(string(b), "timeout 3 curl") {
		t.Fatal("pack-oci.sh CA fetch must not call curl without a timeout")
	}
	if !strings.Contains(string(b), "/dev/pts") || !strings.Contains(string(b), "/fc-attach") {
		t.Fatal("pack-oci.sh does not start the attach terminal")
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
