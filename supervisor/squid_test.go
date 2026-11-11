package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSquidDockerfile(t *testing.T) {
	if SquidImage != "hermetarium-squid:local" {
		t.Fatalf("SquidImage = %q", SquidImage)
	}
	if !strings.Contains(squidDockerfile, "FROM ubuntu/squid:") {
		t.Fatalf("dockerfile base:\n%s", squidDockerfile)
	}
	if !strings.Contains(squidDockerfile, "squid-openssl") {
		t.Fatal("dockerfile does not install squid-openssl")
	}
	if !strings.Contains(squidDockerfile, "/usr/lib/squid/security_file_certgen") {
		t.Fatal("dockerfile does not require the certificate helper")
	}
}

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

func TestInstallACLRemovesStalePid(t *testing.T) {
	dir := t.TempDir()
	acl := filepath.Join(dir, "in.conf")
	pid := filepath.Join(dir, "squid.pid")
	if err := os.WriteFile(acl, []byte("http_port 3128\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pid, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallACL(dir, acl); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pid); !os.IsNotExist(err) {
		t.Fatalf("squid.pid should be gone, stat err: %v", err)
	}
}
