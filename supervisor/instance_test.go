package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDestroyStaysOnWall(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("strong", func(t *testing.T) {
		id := newTestID(t)
		dir := filepath.Join(VarDir(root), id)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// An empty saved name is the weak bridge cleanup's error.
		if err := os.WriteFile(filepath.Join(dir, "bridge.name"), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeInstance(&Instance{ID: id, Wall: "strong", FirecrackerContainer: ContainerName(id, "fc"), Dir: dir}); err != nil {
			t.Fatal(err)
		}
		if err := Destroy(root, id); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "bridge.name")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("weak", func(t *testing.T) {
		id := newTestID(t)
		dir := filepath.Join(VarDir(root), id)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		rootfs := filepath.Join(dir, "rootfs.ext4")
		if err := os.WriteFile(rootfs, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeInstance(&Instance{ID: id, Wall: "weak", Dir: dir}); err != nil {
			t.Fatal(err)
		}
		if err := Destroy(root, id); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(rootfs)
		if err != nil || string(got) != "keep" {
			t.Fatalf("private rootfs = %q, err %v", got, err)
		}
	})
}
