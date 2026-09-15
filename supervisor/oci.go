package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	dest, err := cacheFile(root, "rootfs-"+id+".ext4")
	if err != nil {
		return "", err
	}
	script := filepath.Join(root, "firecracker-helper", "pack-oci.sh")
	if st, err := os.Stat(dest); err == nil && st.Size() > 10_000 {
		if sc, err := os.Stat(script); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
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
	_, err = Docker(3*time.Minute, "run", "--rm", "--privileged",
		"-e", fmt.Sprintf("SIZE_MB=%d", sizeMB),
		"-v", script+":/pack-oci.sh:ro",
		"-v", work+":/in",
		"-v", work+":/out",
		"alpine:3.20", "sh", "/pack-oci.sh",
	)
	if err != nil {
		return "", err
	}
	packed := filepath.Join(work, "rootfs.ext4")
	_ = os.Remove(dest)
	if err := os.Rename(packed, dest); err != nil {
		b, rerr := os.ReadFile(packed)
		if rerr != nil {
			return "", err
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return "", err
		}
	}
	return dest, nil
}
