package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallACL(t *testing.T) {
	dir := t.TempDir()
	if err := InstallACL(dir, ""); err == nil {
		t.Fatal("expected missing ACL path")
	}
	src := filepath.Join(t.TempDir(), "acl.conf")
	if err := os.WriteFile(src, []byte("http_access deny all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallACL(dir, src); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "http_access deny all") {
		t.Fatal(string(b))
	}
}
