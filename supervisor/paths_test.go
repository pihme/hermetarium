package supervisor

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRootEnvWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERMETARIUM_ROOT", dir)
	root, err := Root()
	if err != nil || root != dir {
		t.Fatalf("root=%q err=%v", root, err)
	}
	if CacheDir(root) != filepath.Join(dir, ".cache") {
		t.Fatalf("cache %s", CacheDir(root))
	}
}

func TestRootCheckoutFromCwd(t *testing.T) {
	t.Setenv("HERMETARIUM_ROOT", "")
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if !isCheckout(root) {
		t.Fatalf("expected checkout, got %s", root)
	}
	if CacheDir(root) != filepath.Join(root, ".cache") {
		t.Fatalf("cache %s", CacheDir(root))
	}
}

func TestRootXDGWhenNoCheckout(t *testing.T) {
	t.Setenv("HERMETARIUM_ROOT", "")
	dataHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Chdir(t.TempDir())
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "hermetarium")
	if root != want {
		t.Fatalf("root=%s want %s", root, want)
	}
	if CacheDir(root) != filepath.Join(cacheHome, "hermetarium") {
		t.Fatalf("cache %s", CacheDir(root))
	}
	if _, err := SquidTemplate(root); err == nil {
		t.Fatal("expected missing template")
	} else if !strings.Contains(err.Error(), "squid ACL template not found") {
		t.Fatal(err)
	}
}

func TestCacheDirExplicitRoot(t *testing.T) {
	root := t.TempDir()
	if CacheDir(root) != filepath.Join(root, ".cache") {
		t.Fatalf("CacheDir should follow the passed root, got %s", CacheDir(root))
	}
}

func TestWriteSquidACLMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	err := WriteSquidACL(dir, dir, "10.20.0.3", CreateOpts{})
	if err == nil || !strings.Contains(err.Error(), "squid ACL template not found") {
		t.Fatalf("got %v", err)
	}
}
