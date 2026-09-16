package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	EchoImage       = "hermetarium-echo:local"
	AgentdImage     = "hermetarium-agentd:local"
	InClaudeImage   = "hermetarium-in-claude:local"
	InGrokImage     = "hermetarium-in-grok:local"
	InDeepseekImage = "hermetarium-in-dsh:local"

	InhabitantClaude   = "claude-code"
	InhabitantGrok     = "grok-build"
	InhabitantDeepseek = "deepseek-harness"
)

func EnsureEchoImage(root string) error {
	bin, err := goBuild(root, filepath.Join(supervisor.CacheDir(root), "echo-service"), "./examples/echo")
	if err != nil {
		return err
	}
	ctx := filepath.Join(supervisor.CacheDir(root), "echo-build")
	if err := os.MkdirAll(ctx, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "echo-service"), b, 0o755); err != nil {
		return err
	}
	_, err = supervisor.Docker(3*time.Minute, "build", "-t", EchoImage,
		"-f", filepath.Join(root, "examples", "echo", "Dockerfile"),
		ctx,
	)
	return err
}

func EnsureAgentdImage(root string) error {
	bin, err := goBuild(root, filepath.Join(supervisor.CacheDir(root), "agentd"), "./examples/agentd")
	if err != nil {
		return err
	}
	ctx := filepath.Join(supervisor.CacheDir(root), "agentd-build")
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
	_, err = supervisor.Docker(3*time.Minute, "build", "-t", AgentdImage,
		"-f", filepath.Join(root, "examples", "agentd", "Dockerfile"),
		ctx,
	)
	return err
}

func EnsureOfficialImage(root, name, image string) error {
	bin, err := goBuild(root, filepath.Join(supervisor.CacheDir(root), "porter-build", "porter"), "./porter")
	if err != nil {
		return err
	}
	ctx := filepath.Join(supervisor.CacheDir(root), "porter-build-"+name)
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
	srcDir := filepath.Join(root, "inhabitants", name)
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == "Dockerfile" || e.Name() == "squid.conf" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(ctx, e.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	_, err = supervisor.Docker(10*time.Minute, "build", "-t", image,
		"-f", filepath.Join(srcDir, "Dockerfile"),
		ctx,
	)
	return err
}

func goBuild(root, dest, pkg string) (string, error) {
	src := filepath.Join(root, pkg, "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build %s: %s%s", pkg, out, err)
	}
	return dest, nil
}
