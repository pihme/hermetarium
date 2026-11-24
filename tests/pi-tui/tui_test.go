package pitui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// afterHeader returns the config from the first directive line on, dropping
// the leading comment/blank header. The header text is allowed to differ.
func afterHeader(conf string) string {
	lines := strings.Split(conf, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return strings.Join(lines[i:], "\n")
	}
	return ""
}

// TestPiTUIFiles checks the Pi terminal setup files without Docker, the way
// TestOpenCodeTUIFiles checks omakase/opencode-tui.
func TestPiTUIFiles(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "omakase", "pi-tui", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	for _, want := range []string{
		"inhabitants/pi",
		"--image pi",
		"--wall strong",
		"--no-inbound",
		"htm-<id>-fc",
		"htm-<id>-gate",
		"Porter",
		"/tmp/agent-trigger.txt",
		"file-trigger",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(text), "assistant") {
		t.Fatal("README calls the inhabitant an assistant")
	}
	if _, err := os.Stat(filepath.Join(root, "omakase", "pi-tui", "Dockerfile")); err == nil {
		t.Fatal("omakase/pi-tui has a second Pi image")
	}

	// The omakase gate file is identical to opencode-tui except the header
	// comment; the body from the first directive on must match exactly.
	oct, err := os.ReadFile(filepath.Join(root, "omakase", "opencode-tui", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := os.ReadFile(filepath.Join(root, "omakase", "pi-tui", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	octBody := afterHeader(string(oct))
	ptBody := afterHeader(string(pt))
	if ptBody != octBody {
		t.Fatal("pi gate body differs from opencode-tui; the wall config must match")
	}

	// The inhabitant Dockerfile has the terminal image pieces.
	df, err := os.ReadFile(filepath.Join(root, "inhabitants", "pi", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	image := string(df)
	for _, want := range []string{
		"node:22-bookworm-slim",
		"@earendil-works/pi-coding-agent",
		"@earendil-works/pi-durable",
		"tini",
		"tmux",
		"openssh-server",
		"build-essential",
		"docker.io",
		"gh_2.104.0_linux_amd64.tar.gz",
		"file-trigger.ts",
		"models.json",
		"bash_profile",
		"bashrc",
		"nano",
		"bash-completion",
		"pi-start",
		"/usr/bin/tini",
	} {
		if !strings.Contains(image, want) {
			t.Fatalf("Pi image missing %q", want)
		}
	}

	// The wake extension uses pi.sendMessage with triggerTurn.
	ft, err := os.ReadFile(filepath.Join(root, "inhabitants", "pi", "file-trigger.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fs.watch", "pi.sendMessage", "triggerTurn", "/tmp/agent-trigger.txt"} {
		if !strings.Contains(string(ft), want) {
			t.Fatalf("file-trigger.ts missing %q", want)
		}
	}

	// The model file points at the gate host.
	mj, err := os.ReadFile(filepath.Join(root, "inhabitants", "pi", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"model.hermetarium.test", "hermetarium", "openai-completions"} {
		if !strings.Contains(string(mj), want) {
			t.Fatalf("models.json missing %q", want)
		}
	}

	profile, err := os.ReadFile(filepath.Join(root, "inhabitants", "pi", "bash_profile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENAI_API_KEY=not-the-supervisor-key", "GH_TOKEN=not-the-supervisor-key"} {
		if !strings.Contains(string(profile), want) {
			t.Fatalf("login profile missing %q", want)
		}
	}
	daemon, err := os.ReadFile(filepath.Join(root, "inhabitants", "pi", "daemon.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(daemon), `"iptables": false`) {
		t.Fatal("dockerd would take over iptables")
	}
}
