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
		filepath.Join(root, "examples", "model", "squid.conf"),
		filepath.Join(root, "examples", "openweb", "squid.conf"),
		filepath.Join(root, "inhabitants", "claude-code", "squid.conf"),
		filepath.Join(root, "inhabitants", "grok-build", "squid.conf"),
		filepath.Join(root, "inhabitants", "opencode", "squid.conf"),
		filepath.Join(root, "omakase", "opencode-tui", "squid.conf"),
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

// The directory the gate files read their denial page from, and the page they
// name there. The image copies it from squid/errors, so a config and the image
// must agree on both, or every denial shows "Internal Error: Missing Template".
const (
	denialPageDir = "/usr/local/share/hermetarium/errors"
	denialPageErr = "ERR_HTM_NOT_ALLOWLISTED"
)

// TestGateFilesDenyInfo finds gate files by glob rather than a fixed list, so a
// new example or inhabitant that ships a wall without the explanatory denial
// page fails here instead of silently giving the inhabitant Squid's generic
// error page.
func TestGateFilesDenyInfo(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, pattern := range []string{
		filepath.Join("examples", "*", "squid.conf"),
		filepath.Join("inhabitants", "*", "squid.conf"),
		filepath.Join("omakase", "*", "squid.conf"),
	} {
		m, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 8 {
		t.Fatalf("found %d gate files, expected at least 8\n%v", len(files), files)
	}
	wantDir := "error_directory " + denialPageDir
	wantPage := "deny_info " + denialPageErr + " all"
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		// Only active lines count: a commented rule reads like coverage while
		// Squid never sees it.
		var active []string
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			active = append(active, line)
		}
		lastDeny := -1
		for i, line := range active {
			if line == "http_access deny all" {
				lastDeny = i
			}
		}
		if lastDeny < 0 {
			t.Fatalf("%s has no http_access deny all", rel)
		}
		// error_directory is read once for the process, so its place in the
		// file does not matter; the deny_info line does, after the last deny.
		hasDir := false
		for _, line := range active {
			hasDir = hasDir || line == wantDir
		}
		if !hasDir {
			t.Fatalf("%s does not set %q", rel, wantDir)
		}
		if lastDeny+1 >= len(active) || active[lastDeny+1] != wantPage {
			t.Fatalf("%s does not name the denial page after its final deny\nwant %q\n%s", rel, wantPage, strings.Join(active, "\n"))
		}
	}
}

// TestSquidDenialPage proves the image carries the files the gate files name.
// Squid only loads a deny_info name that is not a built-in error from
// error_directory, and asserts at startup on a name it cannot index there, so
// the directory must hold every template, not just the new page.
func TestSquidDenialPage(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "supervisor", "squid", "errors")
	b, err := os.ReadFile(filepath.Join(dir, denialPageErr))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	// The page says why, coarsely, in the word the I/O log uses. It must not
	// hand the inhabitant the allowlist or the wall's configuration.
	if !strings.Contains(text, "not allowlisted") {
		t.Fatalf("%s does not name the reason", denialPageErr)
	}
	for _, leak := range []string{"dstdomain", "cache_peer", "http_access", "acl ", "__", "squid.conf", "allowlisted hosts"} {
		if strings.Contains(text, leak) {
			t.Fatalf("%s reveals wall internals with %q", denialPageErr, leak)
		}
	}
	// A template that prints %h would show the hostname Squid does not know in
	// this image, so the image's visible_hostname keeps that placeholder filled
	// in for Squid's own error pages as well.
	if !strings.Contains(squidDockerfile, "printf 'visible_hostname hermetarium") {
		t.Fatal("squid/Dockerfile does not set visible_hostname for the image's own error pages")
	}
	// The page sits in the directory the gate files point Squid at. A copy in
	// Squid's own errors/ tree would not be read for an unknown name.
	if !strings.Contains(squidDockerfile, "errors/ "+denialPageDir+"/") {
		t.Fatal("squid/Dockerfile does not copy the error pages into " + denialPageDir)
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
	name := ContainerName(id, "gate")
	if err := runSquid(root, name, dir, TrackTestContainers(t), nil); err != nil {
		t.Fatal(err)
	}
	if err := WaitSquid(dir, name, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	// The gate files name a page only an image built from the full context
	// carries. A missing one has no startup error: Squid starts and answers
	// every denial with "Internal Error: Missing Template".
	page, err := Docker(15*time.Second, "exec", "-u", "root", name, "cat", denialPageDir+"/"+denialPageErr)
	if err != nil || !strings.Contains(page, "not allowlisted") {
		t.Fatalf("gate image has no denial page at %s/%s: %v\n%s", denialPageDir, denialPageErr, err, page)
	}
	// The proxy user owns cache.log, and the unit job has no nettools image to chmod it.
	// Root in the running gate can read the path Squid was given.
	log, err := Docker(15*time.Second, "exec", "-u", "root", name, "cat", "/log/cache.log")
	if err != nil {
		t.Fatal(err)
	}
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
	label := TrackTestContainers(t)
	t.Cleanup(func() {
		DockerIgnore("rm", "-fv", "htm-peer-"+id)
		DockerIgnore("rm", "-fv", ContainerName(id, "gate"))
		DockerIgnore("rm", "-fv", holder)
	})
	if _, err := Docker(30*time.Second, "run", "-d", "--name", holder, "--label", TestLabelKey+"="+label, "--entrypoint", "sleep", AlpineImage, "600"); err != nil {
		t.Fatal(err)
	}
	const key = "htm-secret-key"
	writePeerConf := func(cafile string) {
		t.Helper()
		// Squid names a certificate error at section 83 level 3. Default
		// logging hides it, and an empty peer log does not prove a rejection.
		conf := "" +
			"http_port 127.0.0.1:3128 accel defaultsite=vendor.example\n" +
			"visible_hostname hermetarium\n" +
			"pid_filename /log/squid.pid\n" +
			"coredump_dir /tmp\n" +
			"cache_log /log/cache.log\n" +
			"access_log stdio:/log/access.log\n" +
			"debug_options 83,3\n" +
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

	name := ContainerName(id, "gate")
	writePeerConf("/etc/ssl/certs/ca-certificates.crt")
	// Peer first. Squid's startup probe marks a parent dead when nothing is
	// listening, and a later request then never attempts certificate checks.
	if _, err := Docker(30*time.Second, "run", "-d", "--name", "htm-peer-"+id,
		"--label", TestLabelKey+"="+label,
		"--network", "container:"+holder,
		"-e", "CERT=/log/server.pem", "-e", "KEY=/log/server.key",
		"-e", "PORT=18443", "-e", "LOG=/log/peer.log",
		"-v", dir+":/log", "-v", bin+":/badpeer:ro",
		"--entrypoint", "/badpeer",
		AlpineImage,
	); err != nil {
		t.Fatal(err)
	}
	// An empty peer log is also what a refused connect looks like. badpeer
	// shares this network namespace and binds 127.0.0.1:18443.
	peerUp := time.Now().Add(20 * time.Second)
	var dialErr error
	for {
		_, dialErr = Docker(5*time.Second, "run", "--rm", "--network", "container:"+holder,
			BusyboxImage, "nc", "-z", "-w", "1", "127.0.0.1", "18443")
		if dialErr == nil {
			break
		}
		if time.Now().After(peerUp) {
			logs, _ := Docker(5*time.Second, "logs", "htm-peer-"+id)
			t.Fatalf("127.0.0.1:18443 did not accept a connection: %v\n%s", dialErr, logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := runSquid(root, name, dir, label, []string{"--network", "container:" + holder}); err != nil {
		t.Fatal(err)
	}
	if err := WaitSquid(dir, name, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if vols := volumeNames(t, name); len(vols) != 0 {
		t.Fatalf("gate left anonymous volumes %v", vols)
	}
	ask := func() {
		t.Helper()
		_, _ = Docker(20*time.Second, "run", "--rm", "--network", "container:"+holder,
			BusyboxImage, "wget", "-qO-", "http://127.0.0.1:3128/")
	}
	ask()
	if got := peerLog(t, dir); strings.TrimSpace(got) != "" {
		t.Fatalf("untrusted vendor saw a request: %q", got)
	}
	clog, err := os.ReadFile(filepath.Join(dir, "cache.log"))
	var cacheLog string
	if err == nil {
		cacheLog = string(clog)
	} else if os.IsPermission(err) {
		// The proxy user owns cache.log, and the unit job has no nettools image to chmod it.
		cacheLog, err = Docker(15*time.Second, "exec", "-u", "root", name, "cat", "/log/cache.log")
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
	if !strings.Contains(cacheLog, "X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY") &&
		!strings.Contains(cacheLog, "unable to get local issuer certificate") &&
		!strings.Contains(cacheLog, "certificate verify failed") {
		t.Fatalf("untrusted certificate was not rejected:\n%s", cacheLog)
	}

	// The same certificate, signed by a CA the gate is told to trust, does get the key.
	DockerIgnore("rm", "-fv", name)
	writePeerConf("/log/ca.pem")
	if err := runSquid(root, name, dir, label, []string{"--network", "container:" + holder}); err != nil {
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
	if vols := volumeNames(t, name); len(vols) != 0 {
		t.Fatalf("restarted gate left anonymous volumes %v", vols)
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
