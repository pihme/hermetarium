package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRunNotPrivileged(t *testing.T) {
	// A privileged pack container is host root outside the wall.
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(root, "supervisor"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "--privileged") {
			t.Fatalf("%s contains --privileged", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
