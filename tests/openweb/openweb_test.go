package openweb

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	gateToken = "htm-github-token"
	boxToken  = "box-token"
	systemCA  = "/etc/ssl/certs/ca-certificates.crt"
)

func TestOpenWeb(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	image := buildWebImage(t, root)
	mockBin := buildMock(t, root)
	acl := filepath.Join(t.TempDir(), "squid.conf")
	src, err := os.ReadFile(filepath.Join(root, "examples", "openweb", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	shipped := string(src)
	if !strings.Contains(shipped, "tls_outgoing_options cafile="+systemCA) || strings.Contains(shipped, "cache_peer") || strings.Contains(shipped, "never_direct allow githubhost") {
		t.Fatal("gate file does not use the shipped origin route")
	}
	// Trust the mock CA. The route stays the one in the shipped file.
	conf := strings.Replace(shipped, systemCA, "/log/mockca.pem", 1)
	conf = strings.ReplaceAll(conf, "__GITHUB_TOKEN__", gateToken)
	// Squid checks that an intercepted name resolves to the address the box used.
	conf = strings.Replace(conf, "cache deny all\n", "cache deny all\nhosts_file /log/hosts\n", 1)
	if strings.Contains(conf, "__GITHUB_TOKEN__") || strings.Contains(conf, systemCA) || strings.Contains(conf, "cache_peer") {
		t.Fatalf("unfilled gate file:\n%s", conf)
	}
	if strings.Count(conf, "tls_outgoing_options cafile=/log/mockca.pem") != 1 || !strings.Contains(conf, "hosts_file /log/hosts") {
		t.Fatalf("mock CA was not the only routing change:\n%s", conf)
	}
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
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
			hosts := gateIP + " pinned.example api.github.com other.example\n"
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
			opts := supervisor.CreateOpts{
				Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128,
				TestLabel: label,
				AfterGate: func(hid, netns string) error {
					return startGitHubMock(mockBin, hid, netns, report, label)
				},
			}
			var dir string
			if wall == "weak" {
				t.Cleanup(func() { _ = supervisor.DestroyWeak(id) })
				inst, err := supervisor.CreateWeak(root, id, supervisor.WithTestLabel(t, opts))
				if err != nil {
					t.Fatal(err)
				}
				dir = inst.Dir
			} else {
				t.Cleanup(func() { _ = supervisor.DestroyStrong(id) })
				inst, err := supervisor.CreateStrong(root, id, supervisor.WithTestLabel(t, opts))
				if err != nil {
					t.Fatal(err)
				}
				dir = inst.Dir
			}
			logs := waitWeb(t, wall, id)
			if strings.Contains(logs, "CERT_FAIL") || strings.Contains(logs, "HTTP_FAIL") || !strings.Contains(logs, "WEB_DONE") {
				t.Fatalf("client\n%s\n%s", logs, diag(id, dir))
			}
			if !strings.Contains(betweenCert(logs, "api.github.com"), "issuer=Hermetarium") {
				t.Fatalf("github certificate\n%s", logs)
			}
			if !strings.Contains(logs, "CERT pinned.example subject=pinned.example issuer=Mock Origin") {
				t.Fatalf("pass-through certificate\n%s", logs)
			}
			if strings.Contains(betweenCert(logs, "pinned.example"), "Hermetarium") {
				t.Fatalf("pass-through saw the gate certificate\n%s", logs)
			}
			for _, want := range []string{
				"HTTP POST https://api.github.com/user/repos 200",
				"HTTP POST https://api.github.com/graphql 403",
				"HTTP PATCH https://api.github.com/repos/octo/hello 403",
				"HTTP POST https://api.github.com/repos/octo/hello/git/refs 403",
				"HTTP POST https://api.github.com/repos/octo/../pulls 403",
				"HTTP POST https://api.github.com/repos/octo/a%25b/pulls 403",
				"HTTP DELETE https://api.github.com/repos/octo/hello 403",
				"HTTP PUT https://api.github.com/repos/octo/hello/contents/README.md 403",
				"HTTP GET http://api.github.com/user 403",
				"HTTP GET https://other.example/ 200",
			} {
				if !strings.Contains(logs, want) {
					t.Fatalf("missing %s\n%s\n%s", want, logs, diag(id, dir))
				}
			}
			if err := supervisor.SyncInstanceLog(dir); err != nil {
				t.Fatal(err)
			}
			recs, err := supervisor.ReadIoLog(filepath.Join(dir, "io.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			post, ok := findRec(recs, "POST", "/user/repos")
			if !ok || !post.Allowed || post.TLS != "decrypted" || post.Destination != "api.github.com" {
				t.Fatalf("create repo %+v\n%s", post, diag(id, dir))
			}
			del, ok := findRec(recs, "DELETE", "/repos/octo/hello")
			if !ok || del.Allowed || del.Destination != "api.github.com" {
				t.Fatalf("delete %+v\n%s", del, diag(id, dir))
			}
			put, ok := findRec(recs, "PUT", "/contents/")
			if !ok || put.Allowed {
				t.Fatalf("contents %+v\n%s", put, diag(id, dir))
			}
			for _, deny := range []struct{ method, path string }{
				{"POST", "/graphql"},
				{"PATCH", "/repos/octo/hello"},
				{"POST", "/git/refs"},
				{"POST", ".."},
				{"POST", "%"},
				{"GET", "/user"},
			} {
				rec, found := findRec(recs, deny.method, deny.path)
				if !found || rec.Allowed || rec.Destination != "api.github.com" {
					t.Fatalf("%s %s %+v\n%s", deny.method, deny.path, rec, diag(id, dir))
				}
				if deny.path == "/user" && (rec.Path != "/user" || rec.Protocol != "http" || rec.Port != 80) {
					t.Fatalf("cleartext %+v\n%s", rec, diag(id, dir))
				}
			}
			if !hasTLS(recs, "pinned.example", "passthrough") {
				t.Fatalf("pass-through log %+v\n%s", recs, diag(id, dir))
			}
			got, err := os.ReadFile(filepath.Join(report, "got"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			reposAuth, ok := fieldAuth(text, "path=/user/repos")
			if !ok || reposAuth != "Bearer "+gateToken || strings.Contains(reposAuth, boxToken) {
				t.Fatalf("github auth %q\n%s", reposAuth, text)
			}
			otherAuth, ok := fieldAuth(text, "host=other.example")
			if !ok || otherAuth != "Bearer "+boxToken+", bearer "+boxToken || strings.Contains(otherAuth, gateToken) {
				t.Fatalf("other host auth %q\n%s", otherAuth, text)
			}
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, gateToken) && !strings.Contains(line, "path=/user/repos") {
					t.Fatalf("gate token on the wrong request\n%s", text)
				}
			}
			if strings.Contains(text, "method=DELETE") || strings.Contains(text, "/contents/") || strings.Contains(text, "path=/graphql") || strings.Contains(text, "method=PATCH") || strings.Contains(text, "/git/refs") || strings.Contains(text, "path=/user host") || strings.Contains(text, "..") || strings.Contains(text, "%") {
				t.Fatalf("denied request reached the mock\n%s", text)
			}
			if wall == "weak" {
				out, _, code := supervisor.DockerExec(30*time.Second, "exec", "htm-box-"+id,
					"grep", "-R", "-a", "-F", "-e", gateToken,
					"/etc", "/usr", "/bin", "/lib", "/root", "/tmp", "/webclient", "/probe.sh")
				if code == 0 || strings.Contains(out, gateToken) {
					t.Fatalf("token in box\n%s", out)
				}
			} else if rootfsHas(t, id, gateToken) {
				t.Fatal("token in the guest rootfs")
			}
		})
	}
}

func buildWebImage(t *testing.T, root string) string {
	t.Helper()
	ctx := t.TempDir()
	client := filepath.Join(ctx, "webclient")
	cmd := exec.Command("go", "build", "-o", client, "./tests/webclient")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("client: %v\n%s", err, out)
	}
	b, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(gateToken)) {
		t.Fatal("client binary contains the gate token")
	}
	script := "#!/bin/sh\n" +
		"gw=$(awk '/probe.hermetarium.test/ {print $1; exit}' /etc/hosts)\n" +
		"echo \"$gw api.github.com other.example pinned.example\" >> /etc/hosts\n" +
		"/webclient\n" +
		"echo WEB_DONE\n" +
		"exec sleep 3600\n"
	if err := os.WriteFile(filepath.Join(ctx, "probe.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	df := "FROM " + supervisor.AlpineImage + "\nCOPY webclient /webclient\nCOPY probe.sh /probe.sh\nCMD [\"/probe.sh\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-openweb:local"
	if _, err := supervisor.Docker(5*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	return image
}

func buildMock(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "github-mock")
	cmd := exec.Command("go", "build", "-o", bin, "./tests/githubmock")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock: %v\n%s", err, out)
	}
	return bin
}

func startGitHubMock(bin, id, netns, report, label string) error {
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	args := []string{
		"run", "-d", "--name", "htm-mock-" + id,
		"--network", "container:" + netns,
		"-e", "MOCK_REPORT=/report/got",
		"-e", "CERT_DIR=/report",
		"-v", bin + ":/usr/local/bin/github-mock:ro",
		"-v", report + ":/report",
	}
	args = append(args, supervisor.TestLabelArgs(label)...)
	args = append(args, supervisor.AlpineImage, "/usr/local/bin/github-mock")
	_, err := supervisor.Docker(30*time.Second, args...)
	return err
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
	for i, name := range []string{"pinned.example", "api.github.com", "other.example"} {
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

func waitWeb(t *testing.T, wall, id string) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	name := "htm-box-" + id
	if wall != "weak" {
		name = "htm-fc-" + id
	}
	var logs string
	for time.Now().Before(deadline) {
		logs, _ = supervisor.Docker(10*time.Second, "logs", name)
		if strings.Contains(logs, "WEB_DONE") {
			return logs
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("missing WEB_DONE\n%s", logs)
	return ""
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

func diag(id, dir string) string {
	access, _ := supervisor.Docker(10*time.Second, "exec", "htm-gate-"+id, "cat", "/log/access.log")
	cache, _ := os.ReadFile(filepath.Join(dir, "cache.log"))
	return "ACCESS\n" + access + "\nCACHE\n" + string(cache)
}

func rootfsHas(t *testing.T, id, token string) bool {
	t.Helper()
	src, err := supervisor.Docker(10*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Destination "/opt/rootfs.ext4"}}{{.Source}}{{end}}{{end}}`,
		"htm-fc-"+id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(strings.TrimSpace(src))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Contains(b, []byte(token))
}
