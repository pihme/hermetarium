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
	ex, err := ResolveCreate([]string{"--image", "myorg/box:1"})
	if err != nil || ex.Image != "myorg/box:1" || ex.VendorHost != "" {
		t.Fatalf("image only %+v %v", ex, err)
	}
	ex, err = ResolveCreate([]string{"--image", "my-claude:dev", "--vendor", "claude"})
	if err != nil || ex.Image != "my-claude:dev" || ex.VendorHost != ClaudeHost {
		t.Fatalf("vendor %+v %v", ex, err)
	}
	if _, err = ResolveCreate([]string{"--image", "x", "--vendor", "nope"}); err == nil {
		t.Fatal("expected unknown vendor")
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
