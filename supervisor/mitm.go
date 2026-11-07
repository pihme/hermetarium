package supervisor

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MITMCertPath is where the container sees the gate certificate.
// The private key stays in the instance dir and is not mounted here.
const MITMCertPath = "/etc/ssl/hermetarium-mitm.crt"

// ACLWantsHTTPS reports whether the gate file turns on Squid TLS interception.
// A commented https_port line does not count.
func ACLWantsHTTPS(conf []byte) bool {
	for _, line := range strings.Split(string(conf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "https_port") && strings.Contains(line, "ssl-bump") {
			return true
		}
	}
	return false
}

// PrepareMITM writes a fresh CA into dir/mitm and the public certificate alone
// into dir/mitm-pub. The strong wall serves mitm-pub to the guest. The key
// never goes in that directory.
func PrepareMITM(dir string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Hermetarium"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	mitm := filepath.Join(dir, "mitm")
	pub := filepath.Join(dir, "mitm-pub")
	if err := os.MkdirAll(mitm, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(pub, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mitm, "ca.pem"), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mitm, "ca.key"), keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pub, "ca.pem"), certPEM, 0o644)
}

// InitMITMDB builds the certificate store Squid uses to sign per-site certificates.
func InitMITMDB(root, dir string) error {
	if err := EnsureSquid(root); err != nil {
		return err
	}
	script := `set -eu
p=/usr/lib/squid/security_file_certgen
if [ ! -x "$p" ]; then
  echo "squid image has no $p" >&2
  exit 1
fi
"$p" -c -s /log/mitm/ssl_db -M 4MB
chown -R proxy:proxy /log/mitm
chmod 644 /log/mitm/ca.pem /log/mitm/ca.key
`
	_, err := Docker(60*time.Second, "run", "--rm",
		"-v", dir+":/log",
		"--entrypoint", "sh",
		SquidImage, "-c", script,
	)
	if err != nil {
		return fmt.Errorf("prepare https interception: %w", err)
	}
	return nil
}

// MITMTrustEnv is the container environment that makes TLS libraries trust the gate.
func MITMTrustEnv() []string {
	c := MITMCertPath
	return []string{
		"SSL_CERT_FILE=" + c,
		"NODE_EXTRA_CA_CERTS=" + c,
		"REQUESTS_CA_BUNDLE=" + c,
		"CURL_CA_BUNDLE=" + c,
		"GIT_SSL_CAINFO=" + c,
		"PIP_CERT=" + c,
	}
}

func mitmBoxDockerArgs(dir string) []string {
	cert := filepath.Join(dir, "mitm", "ca.pem")
	args := []string{"-v", cert + ":" + MITMCertPath + ":ro"}
	for _, e := range MITMTrustEnv() {
		args = append(args, "-e", e)
	}
	return args
}

// InstallBoxTrust copies the mounted certificate into the system trust store
// so tools that ignore the environment variables still accept it.
func InstallBoxTrust(container string) error {
	script := `set -eu
src=/etc/ssl/hermetarium-mitm.crt
test -f "$src"
mkdir -p /usr/local/share/ca-certificates
cp "$src" /usr/local/share/ca-certificates/hermetarium-mitm.crt
if command -v update-ca-certificates >/dev/null 2>&1; then
  update-ca-certificates
elif [ -f /etc/ssl/certs/ca-certificates.crt ]; then
  cat "$src" >> /etc/ssl/certs/ca-certificates.crt
fi
`
	_, err := Docker(30*time.Second, "exec", container, "sh", "-c", script)
	if err != nil {
		return fmt.Errorf("install gate certificate: %w", err)
	}
	return nil
}
