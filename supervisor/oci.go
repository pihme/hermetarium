package supervisor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func EnsureImageRootfs(root, image string, sizeMB int) (string, error) {
	if sizeMB <= 0 {
		sizeMB = 256
	}
	id, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", image)
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		id = strings.TrimPrefix(id, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
	}
	// Size is part of the name so a later --disk does not reuse another size.
	dest, err := cacheFile(root, fmt.Sprintf("rootfs-%s-%d.ext4", id, sizeMB))
	if err != nil {
		return "", err
	}
	helpers, err := fcHelperDir(root)
	if err != nil {
		return "", err
	}
	script := filepath.Join(helpers, "pack-oci.sh")
	if rootfsFresh(dest, script, sizeMB) {
		return dest, nil
	}
	cmd, err := Docker(15*time.Second, "inspect", "-f",
		`{{range .Config.Entrypoint}}{{.}} {{end}}{{range .Config.Cmd}}{{.}} {{end}}`, image)
	if err != nil {
		return "", err
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("image %s has empty entrypoint/cmd", image)
	}
	cid, err := Docker(30*time.Second, "create", image)
	if err != nil {
		return "", err
	}
	cid = strings.TrimSpace(cid)
	defer DockerIgnore("rm", "-f", cid)

	work := filepath.Join(CacheDir(root), "oci-"+id)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	tarPath := filepath.Join(work, "rootfs.tar")
	if _, err := Docker(3*time.Minute, "export", "-o", tarPath, cid); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(work, "fc-cmd"), []byte(cmd+"\n"), 0o755); err != nil {
		return "", err
	}
	env, err := Docker(15*time.Second, "inspect", "-f", `{{range .Config.Env}}{{println .}}{{end}}`, image)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(work, "fc-env"), []byte(env), 0o644); err != nil {
		return "", err
	}
	// Size in the directory so two --disk packs do not share one temp file.
	stage := filepath.Join(work, fmt.Sprintf("rootfs-%d.ext4.d", sizeMB))
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}
	// mke2fs -d copies a directory into the ext4 file. Mounting that file
	// would need a privileged container, which is host root outside the wall.
	_, err = Docker(3*time.Minute, "run", "--rm",
		"-e", fmt.Sprintf("SIZE_MB=%d", sizeMB),
		"-v", script+":/pack-oci.sh:ro",
		"-v", work+":/in",
		"-v", stage+":/out",
		AlpineImage, "sh", "/pack-oci.sh",
	)
	if err != nil {
		return "", err
	}
	packed := filepath.Join(stage, "rootfs.ext4")
	if err := os.Rename(packed, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func rootfsFresh(dest, script string, sizeMB int) bool {
	st, err := os.Stat(dest)
	if err != nil || st.Size() != int64(sizeMB)<<20 {
		return false
	}
	sc, err := os.Stat(script)
	if err != nil || st.ModTime().Before(sc.ModTime()) {
		return false
	}
	return true
}

const (
	// Linux lseek(2) whence values. The syscall package does not name them.
	seekData = 3
	seekHole = 4
)

// habitatRootfsPath is the private disk CreateStrong bind-mounts. It is not
// the shared cache (rootfs-<image>-<size>.ext4).
func habitatRootfsPath(dir string) string {
	return filepath.Join(dir, "rootfs.ext4")
}

// habitatRootfs is one habitat's disk. The cache from EnsureImageRootfs is
// keyed only by image and size, so mounting it would publish guest writes
// to every later habitat that shares that image.
//
// The bytes go to rootfs.ext4.partial and are renamed into place. Opening
// the destination with O_TRUNC would follow a symlink onto the cache, and
// would shrink a disk the helper already has open.
func habitatRootfs(dir, cached string) (string, error) {
	dest := habitatRootfsPath(dir)
	partial := dest + ".partial"
	if err := copySparse(cached, partial); err != nil {
		return "", err
	}
	if err := os.Rename(partial, dest); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	return dest, nil
}

// copySparse keeps holes. A large --disk is mostly empty; copying the holes
// would write the whole disk out. dst is created with O_EXCL. On failure
// only that name is removed.
func copySparse(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("copy rootfs: %s is not a regular file", src)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("stale %s; run hermetarium destroy <id> first: %w", filepath.Base(dst), err)
		}
		return err
	}
	defer func() {
		cErr := out.Close()
		if err == nil {
			err = cErr
		}
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	if err = out.Truncate(st.Size()); err != nil {
		return err
	}
	return copyDataExtents(in, out, st.Size())
}

func copyDataExtents(in, out *os.File, size int64) error {
	var off int64
	for off < size {
		data, err := in.Seek(off, seekData)
		if err != nil {
			if errors.Is(err, syscall.ENXIO) {
				return nil
			}
			return err
		}
		if data >= size {
			return nil
		}
		hole, err := in.Seek(data, seekHole)
		if err != nil {
			return err
		}
		if hole > size {
			hole = size
		}
		if hole <= data {
			return fmt.Errorf("copy rootfs: sparse seek did not advance")
		}
		n, err := io.Copy(io.NewOffsetWriter(out, data), io.NewSectionReader(in, data, hole-data))
		if err != nil {
			return err
		}
		if n != hole-data {
			return io.ErrUnexpectedEOF
		}
		off = hole
	}
	return nil
}
