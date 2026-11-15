package supervisor

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateFilesHardened(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(root, "examples", "echo", "squid.conf"),
		filepath.Join(root, "examples", "agentd", "squid.conf"),
		filepath.Join(root, "inhabitants", "claude-code", "squid.conf"),
		filepath.Join(root, "inhabitants", "grok-build", "squid.conf"),
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if strings.Contains(text, "DONT_VERIFY_PEER") {
			t.Fatalf("%s still skips certificate checks", path)
		}
		if strings.Contains(text, "dns_v4_first") {
			t.Fatalf("%s still sets dns_v4_first", path)
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, " myport ") {
				t.Fatalf("%s uses myport: %s", path, line)
			}
		}
	}
}

func TestSquidStartupQuiet(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(filepath.Join(root, "examples", "echo", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), conf, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DestroyWeak(id) })
	name := "htm-gate-" + id
	if err := runSquid(root, name, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := WaitSquid(dir, name, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	logb, err := os.ReadFile(filepath.Join(dir, "cache.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(logb)
	if strings.Contains(log, "not reliable for interception") {
		t.Fatalf("myport warning:\n%s", log)
	}
	if strings.Contains(log, "dns_v4_first") {
		t.Fatalf("dns_v4_first warning:\n%s", log)
	}
}

func TestUntrustedVendorGetsNoKey(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	caPEM, certPEM, keyPEM, err := vendorCert(net.ParseIP("127.0.0.1"), "vendor.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), caPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.key"), keyPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "badpeer")
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin, "github.com/pihme/hermetarium/tests/badpeer")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build badpeer: %v\n%s", err, out)
	}

	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	holder := "htm-hold-" + id
	t.Cleanup(func() {
		DockerIgnore("rm", "-f", "htm-peer-"+id)
		DockerIgnore("rm", "-f", "htm-gate-"+id)
		DockerIgnore("rm", "-f", holder)
	})
	if _, err := Docker(30*time.Second, "run", "-d", "--name", holder, "--entrypoint", "sleep", AlpineImage, "600"); err != nil {
		t.Fatal(err)
	}
	const key = "htm-secret-key"
	writePeerConf := func(cafile string) {
		t.Helper()
		conf := "" +
			"http_port 127.0.0.1:3128 accel defaultsite=vendor.example\n" +
			"visible_hostname hermetarium\n" +
			"pid_filename /log/squid.pid\n" +
			"coredump_dir /tmp\n" +
			"cache_log /log/cache.log\n" +
			"access_log stdio:/log/access.log\n" +
			"cache deny all\n" +
			"cache_peer 127.0.0.1 parent 18443 0 no-query no-digest originserver ssl ssldomain=vendor.example tls-cafile=" + cafile + " name=vendor\n" +
			"cache_peer_access vendor allow all\n" +
			"never_direct allow all\n" +
			"request_header_add Authorization \"Bearer " + key + "\" all\n" +
			"http_access allow all\n"
		if err := os.WriteFile(filepath.Join(dir, "squid.conf"), []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(filepath.Join(dir, "squid.pid"))
		_ = os.Remove(filepath.Join(dir, "peer.log"))
	}

	name := "htm-gate-" + id
	writePeerConf("/etc/ssl/certs/ca-certificates.crt")
	if err := runSquid(root, name, dir, []string{"--network", "container:" + holder}); err != nil {
		t.Fatal(err)
	}
	if err := WaitSquid(dir, name, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(30*time.Second, "run", "-d", "--name", "htm-peer-"+id,
		"--network", "container:"+holder,
		"-e", "CERT=/log/server.pem", "-e", "KEY=/log/server.key",
		"-e", "PORT=18443", "-e", "LOG=/log/peer.log",
		"-v", dir+":/log", "-v", bin+":/badpeer:ro",
		"--entrypoint", "/badpeer",
		AlpineImage,
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	ask := func() {
		t.Helper()
		_, _ = Docker(20*time.Second, "run", "--rm", "--network", "container:"+holder,
			BusyboxImage, "wget", "-qO-", "http://127.0.0.1:3128/")
	}
	ask()
	if got := peerLog(t, dir); strings.TrimSpace(got) != "" {
		t.Fatalf("untrusted vendor saw a request: %q", got)
	}

	// The same certificate, signed by a CA the gate is told to trust, does get the key.
	DockerIgnore("rm", "-f", name)
	writePeerConf("/log/ca.pem")
	if err := runSquid(root, name, dir, []string{"--network", "container:" + holder}); err != nil {
		t.Fatal(err)
	}
	if err := WaitSquid(dir, name, 20*time.Second); err != nil {
		b, _ := os.ReadFile(filepath.Join(dir, "cache.log"))
		t.Fatalf("%v\n%s", err, b)
	}
	ask()
	got := peerLog(t, dir)
	if !strings.Contains(got, "Bearer "+key) {
		b, _ := os.ReadFile(filepath.Join(dir, "cache.log"))
		t.Fatalf("trusted vendor did not see the key: %q\n%s", got, b)
	}
}

func peerLog(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "peer.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

func vendorCert(ip net.IP, name string) (caPEM, certPEM, keyPEM []byte, err error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, err
	}
	caTpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Hermetarium Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	leafSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, err
	}
	leaf := &x509.Certificate{
		SerialNumber: leafSerial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{ip},
		DNSNames:     []string{name},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caTpl, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
	return caPEM, certPEM, keyPEM, nil
}
