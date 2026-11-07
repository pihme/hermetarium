package supervisor

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestACLWantsHTTPS(t *testing.T) {
	if ACLWantsHTTPS([]byte("#https_port 0.0.0.0:3130 intercept ssl-bump cert=/log/mitm/ca.pem\n")) {
		t.Fatal("commented https_port must stay off")
	}
	on := []byte("https_port 0.0.0.0:3130 intercept ssl-bump cert=/log/mitm/ca.pem key=/log/mitm/ca.key\n")
	if !ACLWantsHTTPS(on) {
		t.Fatal("active https_port ssl-bump must turn interception on")
	}
	if ACLWantsHTTPS([]byte("http_port 0.0.0.0:3128 intercept\n")) {
		t.Fatal("plain http intercept is not https interception")
	}
}

func TestPrepareMITM(t *testing.T) {
	dir := t.TempDir()
	if err := PrepareMITM(dir); err != nil {
		t.Fatal(err)
	}
	cert := parseCert(t, filepath.Join(dir, "mitm", "ca.pem"))
	if !cert.IsCA {
		t.Fatal("certificate is not a CA")
	}
	pub := parseCert(t, filepath.Join(dir, "mitm-pub", "ca.pem"))
	if !pub.Equal(cert) {
		t.Fatal("public certificate differs from the CA")
	}
	if _, err := os.Stat(filepath.Join(dir, "mitm-pub", "ca.key")); err == nil {
		t.Fatal("public directory contains the private key")
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "mitm", "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatalf("key block %v", block)
	}
}

func TestInhabitantHTTPSStaysCommented(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Join(filepath.Dir(file), "..", "inhabitants")
	for _, name := range []string{"claude-code", "grok-build", "deepseek-harness"} {
		b, err := os.ReadFile(filepath.Join(root, name, "squid.conf"))
		if err != nil {
			t.Fatal(err)
		}
		if ACLWantsHTTPS(b) {
			t.Fatalf("%s enables https interception by default", name)
		}
		if !containsLine(b, "#https_port 0.0.0.0:3130 intercept name=tlsin ssl-bump cert=/log/mitm/ca.pem key=/log/mitm/ca.key generate-host-certificates=on dynamic_cert_mem_cache_size=4MB") {
			t.Fatalf("%s is missing the OPEN-HTTPS port line", name)
		}
	}
}

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatal("no pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func containsLine(b []byte, want string) bool {
	for _, line := range splitLines(string(b)) {
		if line == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		line := s[:i]
		if i < len(s) {
			i++
		}
		out = append(out, line)
		s = s[i:]
	}
	return out
}
