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
	b, err = os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "rootfs.ext4") {
		t.Fatalf("pack-oci.sh missing rootfs.ext4:\n%s", b)
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
