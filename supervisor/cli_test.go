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
