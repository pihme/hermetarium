package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func echoBinary(root string) string {
	return filepath.Join(CacheDir(root), "echo-service")
}

func EnsureEchoBinary(root string) (string, error) {
	dest := echoBinary(root)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	src := filepath.Join(root, "examples", "echo", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, "./examples/echo")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build echo-service: %s%s", out, err)
	}
	return dest, nil
}

func EnsureEchoImage(root string) error {
	bin, err := EnsureEchoBinary(root)
	if err != nil {
		return err
	}
	ctx := filepath.Join(CacheDir(root), "echo-build")
	if err := os.MkdirAll(ctx, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(ctx, "echo-service")
	b, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, b, 0o755); err != nil {
		return err
	}
	_, err = Docker(3*time.Minute, "build", "-t", EchoImage,
		"-f", filepath.Join(root, "examples", "echo", "Dockerfile"),
		ctx,
	)
	return err
}

func EnsureInhabitant(root string) error {
	if err := EnsureEchoImage(root); err != nil {
		return err
	}
	return EnsureSquidImage(root)
}

func waitInbound(rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		_, err := Call(rawURL, "")
		if err == nil {
			return nil
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil {
		last = errWait("inbound not ready: " + rawURL)
	}
	return errWait("inbound not ready: " + rawURL + ": " + last.Error())
}
