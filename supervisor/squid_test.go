package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSquidACL(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("product", func(t *testing.T) {
		dir := t.TempDir()
		ex := Example{Name: "image", Image: "x", SkipBuild: true}
		if err := WriteSquidACL(root, dir, "10.20.0.3", ex); err != nil {
			t.Fatal(err)
		}
		s := readACL(t, dir)
		if strings.Contains(s, ProbeHost) {
			t.Fatal("product ACL should not allowlist probe")
		}
		if !strings.Contains(s, "http_access deny all") {
			t.Fatal(s)
		}
		if !strings.Contains(s, "cache_effective_user proxy") {
			t.Fatal(s)
		}
	})
	t.Run("fail-closed", func(t *testing.T) {
		t.Setenv("HERMETARIUM_ANTHROPIC_API_KEY", "")
		dir := t.TempDir()
		ex := Example{VendorHost: ClaudeHost, KeyEnv: "HERMETARIUM_ANTHROPIC_API_KEY"}
		if err := WriteSquidACL(root, dir, "10.20.0.3", ex); err == nil {
			t.Fatal("expected fail closed without key")
		}
	})
	t.Run("mock", func(t *testing.T) {
		t.Setenv("HERMETARIUM_ANTHROPIC_API_KEY", "")
		dir := t.TempDir()
		ex := Example{
			VendorHost: ClaudeHost, KeyEnv: "HERMETARIUM_ANTHROPIC_API_KEY",
			UseMock: true, Probe: true,
		}
		if err := WriteSquidACL(root, dir, "10.20.0.3", ex); err != nil {
			t.Fatal(err)
		}
		s := readACL(t, dir)
		if !strings.Contains(s, ProbeHost) {
			t.Fatal(s)
		}
		if !strings.Contains(s, "127.0.0.1") {
			t.Fatal(s)
		}
		if strings.Contains(s, "api.anthropic.com") {
			t.Fatal(s)
		}
	})
}

func readACL(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
