package opencodetui

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	modelKey    = "htm-model-key"
	githubToken = "htm-github-token"
	boxToken    = "box-token"
	modelHost   = "upstream.example"
	modelPrefix = "/openai"
	modelPeer   = "127.0.0.1 parent 18082 0 no-query no-digest originserver login=PASSTHRU name=model"
	systemCA    = "/etc/ssl/certs/ca-certificates.crt"
	peerPort    = "18082"
)

var gitBasic = "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+githubToken))

// passthroughHosts is every name the shipped gate file splices. Each keeps
// its own certificate. The Docker Hub chain lets dockerd in the box pull.
// The package registries install without the gate CA.
var passthroughHosts = []string{
	"pinned.example",
	"registry-1.docker.io", "auth.docker.io", "production.cloudfront.docker.com", "production.cloudflare.docker.com",
	"registry.npmjs.org",
	"repo.maven.apache.org", "repo1.maven.org",
	"crates.io", "static.crates.io", "index.crates.io",
	"pypi.org", "files.pythonhosted.org",
	"deb.debian.org", "security.debian.org",
}

func TestOpenCodeTUIFiles(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "omakase", "opencode-tui", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	for _, want := range []string{
		"inhabitants/opencode",
		"--image opencode",
		"--wall strong",
		"--no-inbound",
		"htm-<id>-fc",
		"htm-<id>-gate",
		"Porter",
		"OSC 52",
		"not found or inactive",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(text), "assistant") {
		t.Fatal("README calls the inhabitant an assistant")
	}
	if _, err := os.Stat(filepath.Join(root, "omakase", "opencode-tui", "Dockerfile")); err == nil {
		t.Fatal("omakase/opencode-tui has a second OpenCode image")
	}
	df, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	image := string(df)
	for _, want := range []string{
		"https://opencode.ai/v2/install",
		"git",
		"ripgrep",
		"tmux",
		"openssh-server",
		"build-essential",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_API_KEY=not-the-supervisor-key",
		"docker.io",
		"gh_2.104.0_linux_amd64.tar.gz",
		"auto-models.ts",
		"bash_profile",
		"bashrc",
		"nano",
		"bash-completion",
		"opencode-start",
	} {
		if !strings.Contains(image, want) {
			t.Fatalf("OpenCode image missing %q", want)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "auto-models.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), "if (output === undefined) output = Math.min(context ?? 8192, 8192)") {
		t.Fatal("model plugin does not fill a missing output limit")
	}
	profile, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "bash_profile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENCODE_API_KEY=not-the-supervisor-key", "GH_TOKEN=not-the-supervisor-key", "cd /work", ". /root/.bashrc"} {
		if !strings.Contains(string(profile), want) {
			t.Fatalf("login profile missing %q", want)
		}
	}
	bashrc, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`PS1='\w \$ '`, "HISTFILE=/root/.bash_history", "set -o emacs", "bash_completion"} {
		if !strings.Contains(string(bashrc), want) {
			t.Fatalf("interactive shell missing %q", want)
		}
	}
	cfg, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "opencode-auto-models") {
		t.Fatal("image config installs the model plugin from npm")
	}
	if !strings.Contains(string(cfg), `"update": "auto"`) {
		t.Fatal("OpenCode autoupdate is off")
	}
	if !strings.Contains(string(plugin), "Plugin.define") {
		t.Fatal("model plugin is not an OpenCode 2 plugin")
	}
	if strings.Contains(image, "OPENCODE_DISABLE_AUTOUPDATE") || strings.Contains(string(profile), "OPENCODE_DISABLE_AUTOUPDATE") || strings.Contains(image, "anomalyco/opencode/releases/download/v") {
		t.Fatal("image pins OpenCode or disables autoupdate")
	}
	daemon, err := os.ReadFile(filepath.Join(root, "inhabitants", "opencode", "daemon.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(daemon), `"iptables": false`) {
		t.Fatal("dockerd would take over iptables")
	}
	conf, err := os.ReadFile(filepath.Join(root, "omakase", "opencode-tui", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	gate := string(conf)
	for _, want := range []string{"__MODEL_PEER__", "__MODEL_HOST__", "__MODEL_PREFIX__", "__MODEL_KEY__", "__GITHUB_TOKEN__", "__GITHUB_GIT_BASIC__"} {
		if !strings.Contains(gate, want) {
			t.Fatalf("gate file missing %s", want)
		}
	}
	if strings.Contains(gate, "acl gh_repo") || strings.Contains(gate, "acl gh_pr") {
		t.Fatal("omakase gate file still filters GitHub calls")
	}
	openweb, err := os.ReadFile(filepath.Join(root, "examples", "openweb", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(openweb, []byte("acl gh_repo")) || !bytes.Contains(openweb, []byte("http_access deny githubhost")) {
		t.Fatal("openweb allow-list was removed")
	}
	if strings.Contains(gate, "18081") || strings.Contains(gate, "name=inbound") || strings.Contains(gate, "name=inhabitant") {
		t.Fatal("gate file still has an inbound port")
	}
	if strings.Contains(gate, "DONT_VERIFY_PEER") || strings.Contains(gate, "dns_v4_first") {
		t.Fatal("gate file is not hardened")
	}
	if !supervisor.ACLWantsHTTPS(conf) {
		t.Fatal("gate file does not decrypt HTTPS")
	}
	if !supervisor.ACLOpensWeb(conf) {
		t.Fatal("gate file would not resolve outside names")
	}
	if !squidLine(gate, "ssl_bump splice passthrough") || !squidLine(gate, "request_header_access Authorization deny modelhost") || !squidLine(gate, "request_header_access Authorization deny githubhost") || !squidLine(gate, "request_header_access Authorization deny githost") || !squidLine(gate, `request_header_add Authorization "Basic __GITHUB_GIT_BASIC__" githost`) || !squidLine(gate, "acl gh_traverse urlpath_regex -i ^[^?]*\\.\\. ^[^?]*%2e%2e ^[^?]*%2e\\. ^[^?]*\\.%2e") || !squidLine(gate, "http_access allow !githubhost !githost") || !squidLine(gate, "always_direct allow !modelhost") {
		t.Fatal("gate file lost the model or GitHub header rules")
	}
	if strings.Contains(gate, `urlpath_regex \.\. %`) {
		t.Fatal("gate file still denies every percent")
	}
	// A spliced host keeps its own certificate, so the gate cannot set a
	// token there. The list is exact: nothing else is passed through.
	got := aclValues(gate, "passthrough", "ssl::server_name")
	if strings.Join(got, " ") != strings.Join(passthroughHosts, " ") {
		t.Fatalf("pass-through list\n got %q\nwant %q", got, passthroughHosts)
	}
}

// aclValues joins the values of every active "acl NAME TYPE ..." line.
func aclValues(conf, name, typ string) []string {
	var out []string
	for _, line := range strings.Split(conf, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "acl" && f[1] == name && f[2] == typ {
			out = append(out, f[3:]...)
		}
	}
	return out
}

func TestOpenCodeTUI(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	acl := filepath.Join(t.TempDir(), "squid.conf")
	src, err := os.ReadFile(filepath.Join(root, "omakase", "opencode-tui", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(src)
	// The comment names the same CA file. Only the active outgoing line trusts the mock.
	oldCA := "tls_outgoing_options cafile=" + systemCA
	if strings.Count(conf, oldCA) != 1 {
		t.Fatalf("outgoing CA line\n%s", conf)
	}
	conf = strings.Replace(conf, oldCA, "tls_outgoing_options cafile=/log/mockca.pem", 1)
	conf = strings.ReplaceAll(conf, "__MODEL_PEER__", modelPeer)
	conf = strings.ReplaceAll(conf, "__MODEL_PREFIX__", modelPrefix)
	conf = strings.ReplaceAll(conf, "__MODEL_HOST__", modelHost)
	conf = strings.ReplaceAll(conf, "__MODEL_KEY__", modelKey)
	conf = strings.ReplaceAll(conf, "__GITHUB_TOKEN__", githubToken)
	conf = strings.ReplaceAll(conf, "__GITHUB_GIT_BASIC__", strings.TrimPrefix(gitBasic, "Basic "))
	conf = strings.Replace(conf, "cache deny all\n", "cache deny all\nhosts_file /log/hosts\n", 1)
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "__MODEL_") || strings.Contains(line, "__GITHUB_") {
			t.Fatalf("unfilled gate file:\n%s", conf)
		}
	}
	// login=PASSTHRU is on the test peer: without the deny line the box Authorization is forwarded.
	if !squidLine(conf, "request_header_access Authorization deny modelhost") {
		t.Fatal("gate file does not deny the box Authorization")
	}
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	image := buildImage(t, root)
	mockBin := buildBin(t, root, "github-mock", "./tests/githubmock")

	for _, wall := range []string{"weak", "strong"} {
		wall := wall
		t.Run(wall, func(t *testing.T) {
			report := t.TempDir()
			if err := writeOriginCert(report); err != nil {
				t.Fatal(err)
			}
			id, err := supervisor.NewID()
			if err != nil {
				t.Fatal(err)
			}
			instDir, err := supervisor.InstanceDir(root, id)
			if err != nil {
				t.Fatal(err)
			}
			// Squid refuses an intercepted name whose address is not the
			// destination the box connected to. The box uses the gate address.
			gateIP := supervisor.PlanFor(wall, id).Gate
			hosts := gateIP + " " + strings.Join(append([]string{"api.github.com", "github.com", "other.example"}, passthroughHosts...), " ") + "\n"
			if err := os.WriteFile(filepath.Join(instDir, "hosts"), []byte(hosts), 0o644); err != nil {
				t.Fatal(err)
			}
			ca, err := os.ReadFile(filepath.Join(report, "ca.pem"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(instDir, "mockca.pem"), ca, 0o644); err != nil {
				t.Fatal(err)
			}
			label := supervisor.TrackTestContainers(t)
			supervisor.OnGateReady(id, func(string, string) error {
				if err := startModelMock(root, id, report, label); err != nil {
					return err
				}
				return startGitHubMock(mockBin, id, report, label)
			})
			opts := supervisor.CreateOpts{
				Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128,
				TestLabel: label,
			}
			var dir string
			if wall == "weak" {
				// htm-gh shares the gate netns. Destroy removes that container by name.
				t.Cleanup(func() {
					_, _ = supervisor.Docker(30*time.Second, "rm", "-f", "htm-gh-"+id)
					_ = supervisor.DestroyWeak(id)
				})
				inst, err := supervisor.CreateWeak(root, id, supervisor.WithTestLabel(t, opts))
				if err != nil {
					t.Fatal(err)
				}
				dir = inst.Dir
			} else {
				// htm-gh shares the Firecracker container netns. Destroy removes that container by name.
				t.Cleanup(func() {
					_, _ = supervisor.Docker(30*time.Second, "rm", "-f", "htm-gh-"+id)
					_ = supervisor.DestroyStrong(id)
				})
				inst, err := supervisor.CreateStrong(root, id, supervisor.WithTestLabel(t, opts))
				if err != nil {
					t.Fatal(err)
				}
				dir = inst.Dir
			}
			if _, err := supervisor.LoadInboundURL(root, id); err == nil || err.Error() != "this habitat has no inbound address" {
				t.Fatalf("url: %v", err)
			}
			logs := waitMarker(t, wall, id, "TUI_DONE")
			if strings.Contains(logs, "CLIENT_FAIL") || strings.Contains(logs, "HTTP_FAIL") || strings.Contains(logs, "PAGE_FAIL") || strings.Contains(logs, "CERT_FAIL") {
				t.Fatalf("client\n%s\n%s", logs, diag(id, dir))
			}
			if !strings.Contains(logs, "HTTP 200") || !strings.Contains(logs, "STREAM_DONE") || !strings.Contains(logs, "PAGE_DONE") {
				t.Fatalf("client\n%s\n%s", logs, diag(id, dir))
			}
			if streamedGaps(logs) < 2 {
				t.Fatalf("stream arrived in one piece\n%s", logs)
			}
			if markerValue(logs, "BOX_ID") != "0" {
				t.Fatalf("not root\n%s", logs)
			}
			env := between(logs, "ENV_BEGIN", "ENV_END")
			if strings.Contains(env, modelKey) || strings.Contains(env, githubToken) {
				t.Fatalf("secret in environment\n%s", env)
			}
			if markerValue(logs, "DNS_DIRECT") != "FAIL" {
				t.Fatalf("query to 1.1.1.1 was answered\n%s", logs)
			}
			if markerValue(logs, "LEAK") != "FAIL" {
				t.Fatalf("ping left the box\n%s", logs)
			}
			if strings.Contains(logs, "UPSTREAM_OK") || !strings.Contains(logs, "UPSTREAM_FAIL") {
				t.Fatalf("model peer was reachable directly\n%s", logs)
			}
			for _, host := range passthroughHosts {
				if !strings.Contains(logs, "CERT "+host+" subject="+host+" issuer=Mock Origin") {
					t.Fatalf("pass-through certificate %s\n%s", host, logs)
				}
				if strings.Contains(betweenCert(logs, host), "Hermetarium") {
					t.Fatalf("pass-through %s saw the gate certificate\n%s", host, logs)
				}
			}
			for _, want := range []string{
				"HTTP GET https://other.example/hello 200",
				"HTTP GET https://github.com/octo/hello.git/info/refs?service=git-upload-pack 200",
				"HTTP POST https://github.com/octo/hello.git/git-receive-pack 200",
				"HTTP POST https://api.github.com/user/repos 200",
				"HTTP DELETE https://api.github.com/repos/octo/hello 200",
				"HTTP POST https://api.github.com/graphql 200",
				"HTTP PATCH https://api.github.com/repos/octo/hello 200",
				"HTTP POST https://api.github.com/repos/octo/hello/git/refs 200",
				"HTTP PUT https://api.github.com/repos/octo/hello/contents/README.md 200",
				"HTTP GET https://api.github.com/repos/octo/hello/actions/runs?branch=feature%2Fx 200",
				"HTTP GET https://api.github.com/search/issues?q=repo%3Apihme%2Fhermetarium 200",
				"HTTP GET https://api.github.com/repos/octo/hello/branches/feature%2Fx 200",
				"HTTP GET https://api.github.com/search/issues?q=a..b 200",
				"HTTP POST https://api.github.com/repos/octo/../pulls 403",
				"HTTP POST https://api.github.com/repos/octo/%2e%2e/pulls 403",
				"HTTP POST https://api.github.com/repos/octo/%2E%2E/pulls 403",
				"HTTP POST https://api.github.com/repos/octo/%2e./pulls 403",
				"HTTP POST https://api.github.com/repos/octo/.%2E/pulls 403",
				"HTTP POST https://api.github.com/repos/octo/a%25b/pulls 200",
				"HTTP POST https://github.com/octo/../hello.git/git-receive-pack 403",
				"HTTP POST https://github.com/octo/%2E%2E/hello.git/git-receive-pack 403",
				"HTTP GET http://api.github.com/user 403",
				"HTTP GET http://github.com/octo/hello.git/info/refs?service=git-upload-pack 403",
			} {
				if !strings.Contains(logs, want) {
					t.Fatalf("missing %s\n%s\n%s", want, logs, diag(id, dir))
				}
			}
			modelGot, err := os.ReadFile(filepath.Join(report, "model"))
			if err != nil {
				t.Fatal(err)
			}
			auths := reportFields(string(modelGot), "auth")
			if len(auths) != 1 || auths[0] != "Bearer "+modelKey || strings.Contains(string(modelGot), githubToken) || strings.Contains(string(modelGot), boxToken) {
				t.Fatalf("model auth %q", modelGot)
			}
			gotHosts := reportFields(string(modelGot), "host")
			paths := reportFields(string(modelGot), "path")
			if len(gotHosts) != 1 || gotHosts[0] != modelHost || len(paths) != 1 || paths[0] != modelPrefix+"/v1/chat/completions" {
				t.Fatalf("upstream %s", modelGot)
			}
			gh, err := os.ReadFile(filepath.Join(report, "github"))
			if err != nil {
				t.Fatal(err)
			}
			ghText := string(gh)
			for _, sub := range []string{
				"path=/user/repos",
				"path=/graphql",
				"method=DELETE",
				"method=PATCH",
				"path=/repos/octo/hello/git/refs",
				"path=/repos/octo/hello/contents/README.md",
			} {
				auth, ok := fieldAuth(ghText, sub)
				if !ok || auth != "Bearer "+githubToken || strings.Contains(auth, boxToken) || strings.Contains(auth, modelKey) {
					t.Fatalf("github auth %s %q\n%s", sub, auth, ghText)
				}
			}
			for _, sub := range []string{
				"path=/octo/hello.git/info/refs",
				"path=/octo/hello.git/git-receive-pack",
			} {
				auth, ok := fieldAuth(ghText, sub)
				if !ok || auth != gitBasic || strings.Contains(auth, boxToken) || strings.Contains(auth, modelKey) || strings.Contains(auth, githubToken) {
					t.Fatalf("git auth %s %q\n%s", sub, auth, ghText)
				}
			}
			if strings.Count(ghText, "path=/octo/hello.git/info/refs") != 1 {
				t.Fatalf("cleartext git reached the mock\n%s", ghText)
			}
			pageAuth, ok := fieldAuth(ghText, "path=/hello")
			if !ok || pageAuth != "Bearer "+boxToken || strings.Contains(pageAuth, githubToken) || strings.Contains(pageAuth, modelKey) {
				t.Fatalf("page auth %q\n%s", pageAuth, ghText)
			}
			for _, sub := range []string{
				"path=/repos/octo/a%25b/pulls",
				"branch=feature%2Fx",
				"q=repo%3Apihme%2Fhermetarium",
				"path=/repos/octo/hello/branches/feature%2Fx",
				"q=a..b",
			} {
				if !strings.Contains(ghText, sub) {
					t.Fatalf("gh read missing %s\n%s", sub, ghText)
				}
			}
			if strings.Contains(ghText, "path=/user host=") || pathHasTraversal(ghText) {
				t.Fatalf("denied request reached the mock\n%s", ghText)
			}
			if err := supervisor.SyncInstanceLog(dir); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "io.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(modelKey)) || bytes.Contains(raw, []byte(githubToken)) {
				t.Fatal("secret in the I/O log")
			}
			recs, err := supervisor.ReadIoLog(filepath.Join(dir, "io.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			page, ok := findRec(recs, "GET", "/hello")
			if !ok || !page.Allowed || page.Destination != "other.example" || page.Path != "/hello" || page.Protocol != "https" || page.Port != 443 || page.TLS != "decrypted" {
				t.Fatalf("page %+v\n%s", page, diag(id, dir))
			}
			for _, allow := range []struct{ method, path, dest string }{
				{"PUT", "/contents/", "api.github.com"},
				{"POST", "/graphql", "api.github.com"},
				{"PATCH", "/repos/octo/hello", "api.github.com"},
				{"POST", "/git/refs", "api.github.com"},
				{"DELETE", "/repos/octo/hello", "api.github.com"},
				{"GET", "hello.git/info/refs", "github.com"},
				{"POST", "git-receive-pack", "github.com"},
				{"GET", "/actions/runs", "api.github.com"},
				{"GET", "/search/issues", "api.github.com"},
				// The log parser decodes %2F, so the branch segment is feature/x.
				{"GET", "branches/feature/x", "api.github.com"},
				{"POST", "/a%b/", "api.github.com"},
			} {
				rec, found := findGitHub(recs, allow.method, allow.path, allow.dest, "https")
				if !found || !rec.Allowed || rec.TLS != "decrypted" || rec.Port != 443 {
					t.Fatalf("%s %s %+v\n%s", allow.method, allow.path, rec, diag(id, dir))
				}
			}
			for _, deny := range []struct{ method, path, dest, protocol string }{
				{"POST", "..", "api.github.com", "https"},
				{"POST", "..", "github.com", "https"},
				{"GET", "/user", "api.github.com", "http"},
				{"GET", "hello.git/info/refs", "github.com", "http"},
			} {
				rec, found := findGitHub(recs, deny.method, deny.path, deny.dest, deny.protocol)
				if !found || rec.Allowed {
					t.Fatalf("%s %s %+v\n%s", deny.method, deny.path, rec, diag(id, dir))
				}
				if deny.protocol == "http" && rec.Port != 80 {
					t.Fatalf("cleartext %+v\n%s", rec, diag(id, dir))
				}
			}
			for _, host := range passthroughHosts {
				if !hasTLS(recs, host, "passthrough") || hasTLS(recs, host, "decrypted") {
					t.Fatalf("pass-through log %s %+v\n%s", host, recs, diag(id, dir))
				}
			}
			if wall == "weak" {
				assertBoxHasNoSecret(t, id)
			} else if rootfsHas(t, id, modelKey) || rootfsHas(t, id, githubToken) {
				t.Fatal("secret in the guest rootfs")
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func pathHasTraversal(report string) bool {
	for _, line := range strings.Split(report, "\n") {
		i := strings.Index(line, "path=")
		if i < 0 {
			continue
		}
		path, _, _ := strings.Cut(line[i+len("path="):], " host=")
		path, _, _ = strings.Cut(path, "?")
		low := strings.ToLower(path)
		if strings.Contains(path, "..") || strings.Contains(low, "%2e%2e") || strings.Contains(low, "%2e.") || strings.Contains(low, ".%2e") {
			return true
		}
	}
	return false
}

func squidLine(conf, want string) bool {
	for _, line := range strings.Split(conf, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func buildImage(t *testing.T, root string) string {
	t.Helper()
	ctx := t.TempDir()
	modelBin := buildBin(t, root, "modelclient", "./tests/modelclient")
	pageBin := buildBin(t, root, "page", "./tests/opencode-tui/page")
	for _, name := range []string{"modelclient", "page"} {
		src := modelBin
		if name == "page" {
			src = pageBin
		}
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(modelKey)) || bytes.Contains(b, []byte(githubToken)) {
			t.Fatalf("%s contains a gate secret", name)
		}
		if err := os.WriteFile(filepath.Join(ctx, name), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Any HTTP status counts as reached. wget exits non-zero on a 404, which
	// would hide a direct connection to the mock.
	script := `#!/bin/sh
echo BOX_ID=$(id -u)
echo ENV_BEGIN
printenv
echo ENV_END
if timeout 3 nslookup example.com 1.1.1.1 >/dev/null 2>&1; then echo DNS_DIRECT=OK; else echo DNS_DIRECT=FAIL; fi
if ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then echo LEAK=OK; else echo LEAK=FAIL; fi
gw=$(awk '/probe.hermetarium.test/ {print $1; exit}' /etc/hosts)
echo GATE "$gw"
echo "$gw api.github.com github.com other.example ` + strings.Join(passthroughHosts, " ") + `" >> /etc/hosts
/modelclient
reached() {
  out=$(wget -T 3 -S -O /dev/null "$1" 2>&1) || true
  printf '%s\n' "$out" | grep -q 'HTTP/'
}
if [ -z "$gw" ] || reached "http://$gw:` + peerPort + `/"; then
  echo UPSTREAM_OK
else
  echo UPSTREAM_FAIL
fi
/page ` + strings.Join(passthroughHosts, " ") + `
echo TUI_DONE
exec sleep 3600
`
	if bytes.Contains([]byte(script), []byte(modelKey)) || bytes.Contains([]byte(script), []byte(githubToken)) {
		t.Fatal("probe script contains a gate secret")
	}
	if err := os.WriteFile(filepath.Join(ctx, "probe.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	df := "FROM " + supervisor.AlpineImage + "\nCOPY modelclient /modelclient\nCOPY page /page\nCOPY probe.sh /probe.sh\nCMD [\"/probe.sh\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-opencode-tui:local"
	if _, err := supervisor.Docker(5*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	return image
}

func buildBin(t *testing.T, root, name, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

func startModelMock(root, id, report, label string) error {
	bin := filepath.Join(report, "vendor-mock")
	cmd := exec.Command("go", "build", "-o", bin, "./tests/vendormock")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errString(out, err)
	}
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	args := []string{
		"-e", "MOCK_EXPECT_KEY=" + modelKey,
		"-e", "MOCK_REPORT=/report/model",
		"-e", "LISTEN=:" + peerPort,
		"-v", bin + ":/usr/local/bin/vendor-mock:ro",
		"-v", report + ":/report",
	}
	args = append(args, supervisor.TestLabelArgs(label)...)
	args = append(args, supervisor.AlpineImage, "/usr/local/bin/vendor-mock")
	return supervisor.JoinGate(id, "htm-mock-"+id, args...)
}

func startGitHubMock(bin, id, report, label string) error {
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	args := []string{
		"-e", "MOCK_REPORT=/report/github",
		"-e", "CERT_DIR=/report",
		"-v", bin + ":/usr/local/bin/github-mock:ro",
		"-v", report + ":/report",
	}
	args = append(args, supervisor.TestLabelArgs(label)...)
	args = append(args, supervisor.AlpineImage, "/usr/local/bin/github-mock")
	return supervisor.JoinGate(id, "htm-gh-"+id, args...)
}

func errString(out []byte, err error) error {
	return &buildError{err: err, out: out}
}

type buildError struct {
	err error
	out []byte
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + string(e.out)
}

func assertBoxHasNoSecret(t *testing.T, id string) {
	t.Helper()
	for _, secret := range []string{modelKey, githubToken} {
		out, _, code := supervisor.DockerExec(30*time.Second, "exec", supervisor.ContainerName(id, "box"),
			"grep", "-R", "-a", "-F", "-e", secret,
			"/etc", "/usr", "/bin", "/lib", "/root", "/tmp", "/modelclient", "/page", "/probe.sh")
		if code == 0 || strings.Contains(out, secret) {
			t.Fatalf("secret in box filesystem\n%s", out)
		}
	}
}

func rootfsHas(t *testing.T, id, token string) bool {
	t.Helper()
	src, err := supervisor.Docker(10*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Destination "/opt/rootfs.ext4"}}{{.Source}}{{end}}{{end}}`,
		supervisor.ContainerName(id, "fc"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(strings.TrimSpace(src))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Contains(b, []byte(token))
}

func waitMarker(t *testing.T, wall, id, marker string) string {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	name := supervisor.ContainerName(id, "box")
	if wall != "weak" {
		name = supervisor.ContainerName(id, "fc")
	}
	var logs string
	for time.Now().Before(deadline) {
		logs, _ = supervisor.Docker(10*time.Second, "logs", name)
		if strings.Contains(logs, marker) {
			return logs
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("missing %s\n%s", marker, logs)
	return ""
}

func markerValue(logs, key string) string {
	p := key + "="
	i := strings.LastIndex(logs, p)
	if i < 0 {
		return ""
	}
	rest := logs[i+len(p):]
	if n := strings.IndexAny(rest, "\r\n"); n >= 0 {
		rest = rest[:n]
	}
	return strings.TrimSpace(rest)
}

func between(logs, start, end string) string {
	i := strings.LastIndex(logs, start)
	if i < 0 {
		return ""
	}
	rest := logs[i+len(start):]
	j := strings.Index(rest, end)
	if j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func reportFields(text, key string) []string {
	var out []string
	prefix := key + "="
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, strings.TrimPrefix(line, prefix))
		}
	}
	return out
}

func fieldAuth(text, sub string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, sub) {
			continue
		}
		_, auth, ok := strings.Cut(line, " auth=")
		if ok {
			return auth, true
		}
	}
	return "", false
}

func findGitHub(recs []supervisor.Record, method, pathSub, dest, protocol string) (supervisor.Record, bool) {
	for _, r := range recs {
		if r.Method == method && r.Destination == dest && r.Protocol == protocol && strings.Contains(r.Path, pathSub) {
			return r, true
		}
	}
	return supervisor.Record{}, false
}

func findRec(recs []supervisor.Record, method, pathSub string) (supervisor.Record, bool) {
	for _, r := range recs {
		if r.Method == method && strings.Contains(r.Path, pathSub) {
			return r, true
		}
	}
	return supervisor.Record{}, false
}

func hasTLS(recs []supervisor.Record, host, mode string) bool {
	for _, r := range recs {
		if r.Destination == host && r.TLS == mode {
			return true
		}
	}
	return false
}

func betweenCert(logs, host string) string {
	i := strings.Index(logs, "CERT "+host)
	if i < 0 {
		return ""
	}
	rest := logs[i:]
	if j := strings.Index(rest, "\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func streamedGaps(logs string) int {
	n := 0
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "CHUNK ") || !strings.Contains(line, "data:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "CHUNK" {
			continue
		}
		ms, err := strconv.Atoi(f[1])
		if err == nil && ms >= 250 {
			n++
		}
	}
	return n
}

func diag(id, dir string) string {
	access, _ := supervisor.Docker(10*time.Second, "exec", supervisor.ContainerName(id, "gate"), "cat", "/log/access.log")
	cache, _ := os.ReadFile(filepath.Join(dir, "cache.log"))
	return "ACCESS\n" + access + "\nCACHE\n" + string(cache)
}

func writeOriginCert(dir string) error {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Mock Origin"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), caPEM, 0o644); err != nil {
		return err
	}
	for i, name := range append([]string{"api.github.com", "github.com", "other.example"}, passthroughHosts...) {
		leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return err
		}
		leafTpl := &x509.Certificate{
			SerialNumber: big.NewInt(int64(i + 2)),
			Subject:      pkix.Name{CommonName: name},
			DNSNames:     []string{name},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leafTpl, caTpl, &leafKey.PublicKey, caKey)
		if err != nil {
			return err
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
		certPEM = append(certPEM, caPEM...)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
		if err := os.WriteFile(filepath.Join(dir, name+".crt"), certPEM, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".key"), keyPEM, 0o600); err != nil {
			return err
		}
	}
	return nil
}
