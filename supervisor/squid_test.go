package supervisor

import (
	"fmt"
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

func stubDocker(t *testing.T, fn func(time.Duration, ...string) (string, error)) {
	t.Helper()
	prev := dockerFn
	dockerFn = fn
	t.Cleanup(func() { dockerFn = prev })
}

func writeGateFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWaitSquidRetriesMissingLogUntilDeadline(t *testing.T) {
	dir := t.TempDir()
	writeGateFile(t, dir, "squid.pid")
	writeGateFile(t, dir, "access.log")
	var inspects, chmods int
	stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			inspects++
			return "true 0", nil
		}
		if strings.Contains(strings.Join(args, " "), "chmod") {
			chmods++
			t.Errorf("chmod ran before cache.log existed")
		}
		return "", nil
	})
	start := time.Now()
	err := WaitSquid(dir, "gate", 600*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error while cache.log is missing")
	}
	if time.Since(start) < 450*time.Millisecond {
		t.Fatalf("returned in %s, want to keep retrying until the deadline", time.Since(start))
	}
	if inspects < 2 {
		t.Fatalf("inspects = %d, want retries inside the deadline", inspects)
	}
	if chmods != 0 {
		t.Fatalf("chmod calls = %d, want none until both logs exist", chmods)
	}
}

func TestWaitSquidReturnsChmodError(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"squid.pid", "access.log", "cache.log"} {
		writeGateFile(t, dir, name)
	}
	var chmods int
	stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			return "true 0", nil
		}
		if strings.Contains(strings.Join(args, " "), "chmod") {
			chmods++
			return "", fmt.Errorf("docker [exec -u root gate chmod a+r /log/access.log /log/cache.log]\nError response from daemon: No such container: gate\nexit status 1")
		}
		return "", nil
	})
	err := WaitSquid(dir, "gate", 600*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Fatalf("got %v", err)
	}
	if chmods < 2 {
		t.Fatalf("chmod calls = %d, want a retry after the first failure", chmods)
	}
}

func TestWaitSquidChmodSucceedsOnlyWhenBothLogsExist(t *testing.T) {
	dir := t.TempDir()
	writeGateFile(t, dir, "squid.pid")
	writeGateFile(t, dir, "access.log")
	cache := filepath.Join(dir, "cache.log")
	wrote := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		wrote <- os.WriteFile(cache, nil, 0o644)
	}()
	var chmods int
	stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			return "true 0", nil
		}
		if strings.Contains(strings.Join(args, " "), "chmod") {
			for _, name := range []string{"access.log", "cache.log"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					return "", fmt.Errorf("chmod before %s exists", name)
				}
			}
			chmods++
			return "", nil
		}
		return "", nil
	})
	err := WaitSquid(dir, "gate", 2*time.Second)
	if werr := <-wrote; werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if chmods != 1 {
		t.Fatalf("chmod calls = %d, want one call after both logs exist", chmods)
	}
}

func TestEnsureNetToolsStamp(t *testing.T) {
	t.Run("rebuilds when the stamp differs", func(t *testing.T) {
		root := t.TempDir()
		stamp := filepath.Join(CacheDir(root), "nettools-build", "Dockerfile")
		if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
			t.Fatal(err)
		}
		old := "FROM alpine:old\nRUN apk add --no-cache iptables iproute2\n"
		if err := os.WriteFile(stamp, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
		var builds int
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "sha", nil
			}
			if len(args) > 0 && args[0] == "build" {
				builds++
				b, err := os.ReadFile(stamp)
				if err != nil {
					t.Errorf("read stamp during build: %v", err)
				} else if !strings.Contains(string(b), "busybox-extras") {
					t.Errorf("build context:\n%s", b)
				}
				return "ok", nil
			}
			return "", fmt.Errorf("unexpected docker %v", args)
		})
		if err := EnsureNetTools(root); err != nil {
			t.Fatal(err)
		}
		if builds != 1 {
			t.Fatalf("builds = %d, want 1", builds)
		}
		b, err := os.ReadFile(stamp)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) == old || !strings.Contains(string(b), "busybox-extras") {
			t.Fatalf("stamp after success:\n%s", b)
		}
		if err := EnsureNetTools(root); err != nil {
			t.Fatal(err)
		}
		if builds != 1 {
			t.Fatalf("builds = %d, want no second build once the stamp matches", builds)
		}
	})

	t.Run("restores the stamp when the build fails", func(t *testing.T) {
		root := t.TempDir()
		stamp := filepath.Join(CacheDir(root), "nettools-build", "Dockerfile")
		if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
			t.Fatal(err)
		}
		old := "FROM alpine:old\n"
		if err := os.WriteFile(stamp, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "sha", nil
			}
			if len(args) > 0 && args[0] == "build" {
				return "", fmt.Errorf("build failed")
			}
			return "", fmt.Errorf("unexpected docker %v", args)
		})
		if err := EnsureNetTools(root); err == nil || !strings.Contains(err.Error(), "build failed") {
			t.Fatalf("got %v", err)
		}
		b, err := os.ReadFile(stamp)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != old {
			t.Fatalf("stamp = %q, want previous %q", b, old)
		}
	})

	t.Run("removes a new stamp when the first build fails", func(t *testing.T) {
		root := t.TempDir()
		stamp := filepath.Join(CacheDir(root), "nettools-build", "Dockerfile")
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "", fmt.Errorf("no such image")
			}
			if len(args) > 0 && args[0] == "build" {
				return "", fmt.Errorf("build failed")
			}
			return "", fmt.Errorf("unexpected docker %v", args)
		})
		if err := EnsureNetTools(root); err == nil || !strings.Contains(err.Error(), "build failed") {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(stamp); !os.IsNotExist(err) {
			t.Fatalf("stamp should be removed, stat err: %v", err)
		}
	})
}
