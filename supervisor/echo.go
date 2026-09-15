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

func EnsureAgentdBinary(root string) (string, error) {
	dest := filepath.Join(CacheDir(root), "agentd")
	src := filepath.Join(root, "examples", "agentd", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, "./examples/agentd")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build agentd: %s%s", out, err)
	}
	return dest, nil
}

func EnsureExampleImage(root string, ex Example) error {
	if ex.Name == ExampleEcho || ex.Name == "" {
		return EnsureEchoImage(root)
	}
	bin, err := EnsureAgentdBinary(root)
	if err != nil {
		return err
	}
	ctx := filepath.Join(CacheDir(root), "agentd-build-"+ex.Name)
	if err := os.MkdirAll(ctx, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "agentd"), b, 0o755); err != nil {
		return err
	}
	df := filepath.Join(root, "examples", ex.Name, "Dockerfile")
	_, err = Docker(3*time.Minute, "build", "-t", ex.Image, "-f", df, ctx)
	return err
}

func EnsureInhabitant(root string, ex Example) error {
	if err := EnsureExampleImage(root, ex); err != nil {
		return err
	}
	return EnsureSquidImage(root)
}
