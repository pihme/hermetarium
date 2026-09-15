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
	if ex.Kind == "inhabitant" {
		return EnsureOfficialImage(root, ex)
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

func EnsurePorterBinary(root string) (string, error) {
	dest := filepath.Join(CacheDir(root), "porter-build", "porter")
	src := filepath.Join(root, "porter", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, "./porter")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build porter: %s%s", out, err)
	}
	return dest, nil
}

func EnsureOfficialImage(root string, ex Example) error {
	bin, err := EnsurePorterBinary(root)
	if err != nil {
		return err
	}
	ctx := filepath.Join(CacheDir(root), "porter-build-"+ex.Name)
	if err := os.MkdirAll(ctx, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "porter"), b, 0o755); err != nil {
		return err
	}
	srcDir := filepath.Join(root, "inhabitants", ex.Name)
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == "Dockerfile" {
			continue
		}
		in := filepath.Join(srcDir, e.Name())
		raw, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(ctx, e.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	df := filepath.Join(srcDir, "Dockerfile")
	_, err = Docker(10*time.Minute, "build", "-t", ex.Image, "-f", df, ctx)
	return err
}

func EnsureImageExists(name string) error {
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", name); err == nil {
		return nil
	}
	_, err := Docker(5*time.Minute, "pull", name)
	return err
}

func EnsureInhabitant(root string, ex Example) error {
	if ex.SkipBuild {
		if err := EnsureImageExists(ex.Image); err != nil {
			return err
		}
	} else if err := EnsureExampleImage(root, ex); err != nil {
		return err
	}
	return EnsureSquidImage(root)
}
