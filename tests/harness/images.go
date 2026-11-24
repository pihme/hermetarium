package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	EchoImage       = "hermetarium-echo:local"
	AgentdImage     = "hermetarium-agentd:local"
	InClaudeImage   = "hermetarium-in-claude:local"
	InGrokImage     = "hermetarium-in-grok:local"
	InOpenCodeImage = "hermetarium-in-opencode:local"
	InPiImage       = "hermetarium-in-pi:local"

	InhabitantClaude   = "claude-code"
	InhabitantGrok     = "grok-build"
	InhabitantOpenCode = "opencode"
	InhabitantPi       = "pi"

	// DayOnePath is where every inhabitant image reads its static guide.
	DayOnePath = "/etc/hermetarium/day-one.md"
)

// ImageFor is the tag the suite builds for one official inhabitant.
func ImageFor(name string) string {
	switch name {
	case InhabitantClaude:
		return InClaudeImage
	case InhabitantGrok:
		return InGrokImage
	case InhabitantOpenCode:
		return InOpenCodeImage
	case InhabitantPi:
		return InPiImage
	}
	return ""
}

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
	// Every inhabitant Dockerfile in this checkout COPYs this one file. It sits
	// beside each Dockerfile for a hand build; here it rides in the context.
	guide, err := os.ReadFile(filepath.Join(root, "inhabitants", "day-one.md"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "day-one.md"), guide, 0o644); err != nil {
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

// AssertDayOneInImage reads the static guide out of a built image. No habitat
// and no Docker daemon started this container: `docker create` only takes the
// image, and `docker export` writes its filesystem, so an image mounts or
// copies nothing per habitat to be seen here.
func AssertDayOneInImage(t *testing.T, image string) {
	t.Helper()
	cid, err := supervisor.Docker(time.Minute, "create", image)
	if err != nil {
		t.Fatalf("%s: docker create: %v", image, err)
	}
	cid = strings.TrimSpace(cid)
	defer supervisor.DockerIgnore("rm", "-fv", cid)

	dir, err := os.MkdirTemp("", "htm-day-one-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	src := strings.TrimPrefix(DayOnePath, "/")
	tarPath := filepath.Join(dir, "rootfs.tar")
	if _, err := supervisor.Docker(3*time.Minute, "export", "-o", tarPath, cid); err != nil {
		t.Fatalf("%s: docker export: %v", image, err)
	}
	text, err := exec.Command("tar", "-xOf", tarPath, src).Output()
	if err != nil {
		t.Fatalf("%s: %s is not in the image (%v)", image, DayOnePath, err)
	}
	if len(text) == 0 {
		t.Fatalf("%s: %s is empty", image, DayOnePath)
	}
	if !strings.Contains(string(text), "Fail-by-design") {
		t.Fatalf("%s: %s is not the day-one guide", image, DayOnePath)
	}
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
