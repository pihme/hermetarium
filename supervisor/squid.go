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
	tmplPath, err := SquidTemplate(root)
	if err != nil {
		return err
	}
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
		InboundPort:  InboundPort,
		InhabitantIP: inhabitantIP,
		EchoPort:     EchoPort,
	}
	if ex.Probe {
		cfg.ProbeHost = ProbeHost
		cfg.ProbePort = ProbePort
	}
	if ex.VendorHost != "" {
		key, live, err := ex.Secret()
		if err != nil {
			return err
		}
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

func EnsureSquidPulled() error {
	return EnsureImageExists(SquidImage)
}

func EnsureNetTools(root string) error {
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", NetToolsImage); err == nil {
		return nil
	}
	dir := filepath.Join(CacheDir(root), "nettools-build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	df := "FROM " + AlpineImage + "\nRUN apk add --no-cache iptables iproute2\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(df), 0o644); err != nil {
		return err
	}
	_, err := Docker(3*time.Minute, "build", "-t", NetToolsImage, dir)
	return err
}

func runSquid(name, logDir string, extra []string) error {
	if err := EnsureSquidPulled(); err != nil {
		return err
	}
	args := []string{"run", "-d", "--name", name}
	args = append(args, extra...)
	args = append(args,
		"-v", logDir+":/log",
		"--entrypoint", "/usr/sbin/squid",
		SquidImage,
		"-N", "-f", "/log/squid.conf",
	)
	_, err := Docker(30*time.Second, args...)
	return err
}

func applyIntercept(netns string) error {
	_, err := Docker(30*time.Second, "run", "--rm",
		"--network", "container:"+netns,
		"--cap-add", "NET_ADMIN",
		NetToolsImage,
		"iptables", "-t", "nat", "-A", "PREROUTING", "-p", "tcp", "--dport", "80",
		"-j", "REDIRECT", "--to-ports", "3128",
	)
	return err
}

func StartProbe(root, id, netns string) error {
	if err := EnsureImageExists(BusyboxImage); err != nil {
		return err
	}
	probeDir := filepath.Join(root, "tests", "probe")
	_, err := Docker(30*time.Second,
		"run", "-d", "--name", "htm-probe-"+id,
		"--network", "container:"+netns,
		"-v", probeDir+":/probe:ro",
		BusyboxImage,
		"httpd", "-f", "-p", "0.0.0.0:18080", "-h", "/probe",
	)
	return err
}

func StartVendorMock(root, id, netns string) error {
	bin, err := EnsureVendorMock(root)
	if err != nil {
		return err
	}
	if err := EnsureImageExists(AlpineImage); err != nil {
		return err
	}
	_, err = Docker(30*time.Second,
		"run", "-d", "--name", "htm-mock-"+id,
		"--network", "container:"+netns,
		"-e", "MOCK_EXPECT_KEY="+TestVendorKey,
		"-v", bin+":/usr/local/bin/vendor-mock:ro",
		AlpineImage,
		"/usr/local/bin/vendor-mock",
	)
	return err
}

func startTestGateExtras(root, id, netns string, ex Example) error {
	if ex.Probe {
		if err := StartProbe(root, id, netns); err != nil {
			return err
		}
	}
	if ex.VendorHost == "" {
		return nil
	}
	_, live, err := ex.Secret()
	if err != nil {
		return err
	}
	if live {
		return nil
	}
	return StartVendorMock(root, id, netns)
}

func EnsureVendorMock(root string) (string, error) {
	srcRoot, err := sourceTree(root)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(CacheDir(root), "vendor-mock")
	src := filepath.Join(srcRoot, "examples", "vendor-mock", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", dest, "./examples/vendor-mock")
	cmd.Dir = srcRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build vendor-mock: %s%s", out, err)
	}
	return dest, nil
}

func WaitSquid(logDir, container string, timeout time.Duration) error {
	pid := filepath.Join(logDir, "squid.pid")
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}} {{.State.ExitCode}}", container)
		if err == nil && !strings.Contains(out, "true") {
			logs, _ := Docker(5*time.Second, "logs", container)
			return errWait("squid container exited: " + out + "\n" + logs + "\n" + tailHostFile(filepath.Join(logDir, "cache.log")))
		}
		if _, err := os.Stat(pid); err == nil {
			chmodLogs(logDir)
			return nil
		}
		last = "waiting for " + pid
		time.Sleep(200 * time.Millisecond)
	}
	logs, _ := Docker(5*time.Second, "logs", container)
	return errWait("squid not ready: " + last + "\n" + logs + "\n" + tailHostFile(filepath.Join(logDir, "cache.log")))
}

func chmodLogs(logDir string) {
	DockerIgnore("run", "--rm", "-v", logDir+":/log", NetToolsImage,
		"chmod", "a+r", "/log/access.log", "/log/cache.log")
}

func tailHostFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 4000 {
		return string(b[len(b)-4000:])
	}
	return string(b)
}

type waitError string

func (e waitError) Error() string { return string(e) }

func errWait(s string) error { return waitError(s) }
