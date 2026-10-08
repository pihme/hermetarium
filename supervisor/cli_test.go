package supervisor

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestResolveCreate(t *testing.T) {
	if _, err := ResolveCreate(nil); err == nil {
		t.Fatal("expected --image required")
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1"}); err == nil {
		t.Fatal("expected --acl required")
	}
	ex, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf"})
	if err != nil || ex.Image != "myorg/box:1" || ex.ACL != "/tmp/acl.conf" {
		t.Fatalf("got %+v %v", ex, err)
	}
	if ex.MemMiB != 512 || ex.DiskMB != 1024 {
		t.Fatalf("defaults mem=%d disk=%d", ex.MemMiB, ex.DiskMB)
	}
	sized, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--mem", "2048", "--disk", "4096"})
	if err != nil || sized.MemMiB != 2048 || sized.DiskMB != 4096 {
		t.Fatalf("got %+v %v", sized, err)
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--mem", "0"}); err == nil {
		t.Fatal("expected --mem to reject 0")
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--disk", "nope"}); err == nil {
		t.Fatal("expected --disk to reject a non-integer")
	}
}

func TestCreateRejectsSizeFlagsOnWeakWall(t *testing.T) {
	code := Run([]string{"create", "--wall", "weak", "--image", "hermetarium/echo:dev", "--acl", "/tmp/acl.conf", "--mem", "2048"})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestVersionCommand(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := Run([]string{"version"})
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	if code != 0 || !bytes.Contains(b, []byte(Version)) {
		t.Fatalf("code=%d out=%q", code, b)
	}
}
