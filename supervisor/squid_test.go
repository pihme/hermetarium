package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRetryUntilSucceedsAfterFailure(t *testing.T) {
	start := time.Unix(0, 0)
	now := start
	var calls int
	err := retryUntil(start.Add(time.Second), func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }, func() error {
		calls++
		if calls == 1 {
			return errors.New("chmod: no such file")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRetryUntilReturnsDockerError(t *testing.T) {
	start := time.Unix(0, 0)
	now := start
	want := errors.New("docker [run --rm hermetarium-nettools:local chmod]: Unable to find image 'hermetarium-nettools:local' locally")
	var calls int
	err := retryUntil(start.Add(time.Second), func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }, func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want a retry after the first failure", calls)
	}
}

func TestChmodLogsMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := chmodLogs("no-such-gate", dir, time.Second)
	if err == nil {
		t.Fatal("expected missing cache.log to fail")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("got %v, want not-exist before any docker call", err)
	}
}
