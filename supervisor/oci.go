package supervisor

import (
	"bytes"
	"crypto/sha256"
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
	// A new attach program has to land in a rootfs the pack script did not touch.
	sum := sha256.Sum256(fcAttachBin)
	if rootfsFresh(dest, script, sizeMB, sum[:]) {
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
	if err := os.WriteFile(filepath.Join(work, "fc-attach"), fcAttachBin, 0o755); err != nil {
		return "", err
	}
	// Size in the directory so two --disk packs do not share one temp file.
	stage := filepath.Join(work, fmt.Sprintf("rootfs-%d.ext4.d", sizeMB))
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}
	_, err = Docker(3*time.Minute, "run", "--rm", "--privileged",
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
	if err := os.WriteFile(dest+".attachsha", sum[:], 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func rootfsFresh(dest, script string, sizeMB int, sum []byte) bool {
	st, err := os.Stat(dest)
	if err != nil || st.Size() != int64(sizeMB)<<20 {
		return false
	}
	sc, err := os.Stat(script)
	if err != nil || st.ModTime().Before(sc.ModTime()) {
		return false
	}
	old, err := os.ReadFile(dest + ".attachsha")
	return err == nil && bytes.Equal(old, sum)
}
