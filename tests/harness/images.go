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

	InhabitantClaude   = "claude-code"
	InhabitantGrok     = "grok-build"
	InhabitantOpenCode = "opencode"

	// DayOnePath is where every inhabitant image reads its static guide.
	DayOnePath = "/etc/hermetarium/day-one.md"

	// CaProfilePath is where every inhabitant image installs the CA-trust vars
	// for login shells (the day-one guide's "the gate sets these variables").
	CaProfilePath = "/etc/profile.d/hermetarium-ca.sh"
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
	// The CA trust profile, copied the same way as the guide above.
	ca, err := os.ReadFile(filepath.Join(root, "inhabitants", "profile.d-ca.sh"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "profile.d-ca.sh"), ca, 0o644); err != nil {
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

// AssertDayOneInImage reads the static guide and the CA-trust profile out of a
// built image. No habitat and no Docker daemon started this container:
// `docker create` only takes the image, and `docker export` writes its
// filesystem, so an image mounts or copies nothing per habitat to be seen here.
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

	tarPath := filepath.Join(dir, "rootfs.tar")
	if _, err := supervisor.Docker(3*time.Minute, "export", "-o", tarPath, cid); err != nil {
		t.Fatalf("%s: docker export: %v", image, err)
	}
	read := func(path string) []byte {
		t.Helper()
		text, err := exec.Command("tar", "-xOf", tarPath, strings.TrimPrefix(path, "/")).Output()
		if err != nil {
			t.Fatalf("%s: %s is not in the image (%v)", image, path, err)
		}
		return text
	}
	guide := read(DayOnePath)
	if len(guide) == 0 {
		t.Fatalf("%s: %s is empty", image, DayOnePath)
	}
	if !strings.Contains(string(guide), "Fail-by-design") {
		t.Fatalf("%s: %s is not the day-one guide", image, DayOnePath)
	}
	// The guide promises the gate sets these CA-trust vars. The image must
	// install them in a login profile, or an interactive attach shell would
	// not have them (only PID 1 gets the guest-init exports).
	for _, v := range []string{
		"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE",
		"CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "PIP_CERT",
	} {
		want := "export " + v + "=/etc/ssl/hermetarium-mitm.crt"
		if !strings.Contains(string(read(CaProfilePath)), want) {
			t.Fatalf("%s: %s does not set %s", image, CaProfilePath, want)
		}
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
