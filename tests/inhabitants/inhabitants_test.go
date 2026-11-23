package inhabitants

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
	"github.com/pihme/hermetarium/tests/harness"
)

func TestOfficialCLIs(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("claude-code", func(t *testing.T) {
		probeOfficial(t, root, harness.InhabitantClaude, []string{"/usr/local/bin/claude", "--version"}, supervisor.ClaudeHost)
	})
	t.Run("grok-build", func(t *testing.T) {
		probeOfficial(t, root, harness.InhabitantGrok, []string{"/usr/local/bin/grok", "--version"}, supervisor.GrokHost)
	})
	t.Run("opencode", func(t *testing.T) {
		probeOfficial(t, root, harness.InhabitantOpenCode, []string{"/usr/local/bin/opencode", "--version"}, supervisor.ModelHost)
	})
}

func probeOfficial(t *testing.T, root, name string, versionCmd []string, vendorHost string) {
	t.Helper()
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inhabitant %s weak %s", name, id)
	inst, err := harness.CreateWeakInhabitant(t, root, id, name, true)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyWeak(id)

	out, errOut, code := supervisor.ExecWeak(inst, []string{"test", "-s", "/etc/hermetarium/day-one.md"})
	if code != 0 {
		t.Fatalf("day-one guide missing: %s %s", out, errOut)
	}
	out, errOut, code = supervisor.ExecWeak(inst, []string{"grep", "-F", "/etc/ssl/hermetarium-mitm.crt", "/etc/hermetarium/day-one.md"})
	if code != 0 {
		t.Fatalf("day-one guide missing CA path: %s %s", out, errOut)
	}
	out, errOut, code = supervisor.ExecWeak(inst, versionCmd)
	if code != 0 {
		t.Fatalf("official CLI missing: %s %s", out, errOut)
	}
	t.Logf("official CLI: %s", strings.TrimSpace(out+" "+errOut))
	harness.AssertNoKeyInBox(t, inst)
	harness.AssertInboundNoVendor(t, inst.InboundURL, vendorHost)
	t.Log("inbound port does not forward the vendor host")

	got, err := supervisor.CallTimeout(inst.InboundURL, "Run id -u and reply with only the number.", 20*time.Second)
	if err != nil {
		t.Logf("mock vendor API rejected by official CLI (expected): %v %s", err, got)
		t.Log("chat/root through the real model is tests/inhabitants live suite (make test-live)")
		return
	}
	t.Logf("mock accepted: %q", got)
	harness.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath, vendorHost)
}

func TestDayOneGuide(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	guide := filepath.Join(root, "inhabitants", "day-one.md")
	body, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"/etc/ssl/hermetarium-mitm.crt",
		"openssl x509",
		"CARGO_HTTP_CAINFO",
		"keytool",
		"SSL_CERT_FILE",
		"adduser",
		"--network host",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("day-one guide missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(text), "io.jsonl") || strings.Contains(text, "hermetarium logs") {
		t.Fatal("day-one guide must not tell the inhabitant how to read the I/O log")
	}
	for _, name := range []string{"claude-code", "grok-build", "opencode"} {
		df, err := os.ReadFile(filepath.Join(root, "inhabitants", name, "Dockerfile"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(df), "COPY day-one.md /etc/hermetarium/day-one.md") {
			t.Fatalf("%s Dockerfile does not install the day-one guide", name)
		}
	}
}
