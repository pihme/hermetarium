package fchelper

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed entrypoint.sh pack-oci.sh
var Files embed.FS

// Extract writes the helper scripts into destDir. Unchanged files keep their mtime
// so packed rootfs caches can still key off pack-oci.sh.
func Extract(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." || d.IsDir() {
			return nil
		}
		b, err := Files.ReadFile(path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if old, err := os.ReadFile(dest); err == nil && bytes.Equal(old, b) {
			return nil
		}
		return os.WriteFile(dest, b, 0o755)
	})
}
