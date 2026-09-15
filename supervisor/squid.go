package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type squidConf struct {
	ListenPort   int
	LogDir       string
	ProbeHost    string
	ProbePort    int
	InboundPort  int
	InhabitantIP string
	EchoPort     int
	VendorHost   string
	VendorPeer   string
	VendorPort   int
	VendorSSL    bool
	VendorKey    string
}

func WriteSquidACL(root, hostLogDir, inhabitantIP string, ex Example) error {
	tmplPath := filepath.Join(root, "squid", "squid.conf.tmpl")
	b, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}
	tmpl, err := template.New("squid").Parse(string(b))
	if err != nil {
		return err
	}
	out, err := os.Create(filepath.Join(hostLogDir, "squid.conf"))
	if err != nil {
		return err
	}
	defer out.Close()
	cfg := squidConf{
		ListenPort:   SquidPort,
		LogDir:       "/log",
		ProbeHost:    ProbeHost,
		ProbePort:    ProbePort,
		InboundPort:  InboundPort,
		InhabitantIP: inhabitantIP,
		EchoPort:     EchoPort,
	}
	if ex.VendorHost != "" {
		key, live := ex.Secret()
		cfg.VendorHost = ex.VendorHost
		cfg.VendorKey = key
		if live {
			cfg.VendorPeer = ex.LivePeer
			cfg.VendorPort = ex.LivePort
			cfg.VendorSSL = true
		} else {
			cfg.VendorPeer = "127.0.0.1"
			cfg.VendorPort = VendorMockPort
		}
	}
	return tmpl.Execute(out, cfg)
}

func EnsureVendorMock(root string) (string, error) {
	dest := filepath.Join(CacheDir(root), "vendor-mock")
	src := filepath.Join(root, "examples", "vendor-mock", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, "./examples/vendor-mock")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build vendor-mock: %s%s", out, err)
	}
	return dest, nil
}

func EnsureSquidImage(root string) error {
	bin, err := EnsureVendorMock(root)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "squid", "vendor-mock"), b, 0o755); err != nil {
		return err
	}
	ctx := filepath.Join(root, "squid")
	_, err = Docker(3*time.Minute, "build", "-t", SquidImage, ctx)
	return err
}

func WaitSquid(container string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}} {{.State.ExitCode}}", container)
		if err == nil && !strings.Contains(out, "true") {
			logs, _ := Docker(5*time.Second, "logs", container)
			return errWait("squid container exited: " + out + "\n" + logs + "\n" + dockerCopy(container, "/log/cache.log"))
		}
		_, stderr, code := DockerExec(5*time.Second, "exec", container, "test", "-f", "/log/squid.pid")
		if code == 0 {
			_, _, _ = DockerExec(5*time.Second, "exec", container, "chmod", "a+r", "/log/access.log", "/log/cache.log")
			return nil
		}
		last = stderr
		time.Sleep(200 * time.Millisecond)
	}
	logs, _ := Docker(5*time.Second, "logs", container)
	return errWait("squid not ready: " + last + "\n" + logs)
}

type waitError string

func (e waitError) Error() string { return string(e) }

func errWait(s string) error { return waitError(s) }

func dockerCopy(container, src string) string {
	tmp, err := os.CreateTemp("", "htm-copy-")
	if err != nil {
		return ""
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if _, err := Docker(5*time.Second, "cp", container+":"+src, tmp.Name()); err != nil {
		return ""
	}
	b, err := os.ReadFile(tmp.Name())
	if err != nil {
		return ""
	}
	if len(b) > 4000 {
		return string(b[len(b)-4000:])
	}
	return string(b)
}
