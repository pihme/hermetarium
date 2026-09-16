package supervisor

import (
	"os"
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

func WriteSquidACL(root, hostLogDir, inhabitantIP string, opts CreateOpts) error {
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
	if opts.ProbeHost != "" {
		cfg.ProbeHost = opts.ProbeHost
		cfg.ProbePort = opts.ProbePort
		if cfg.ProbePort == 0 {
			cfg.ProbePort = ProbePort
		}
	}
	if opts.VendorHost != "" {
		peer, port, ssl, key, err := opts.vendorPeer()
		if err != nil {
			return err
		}
		cfg.VendorHost = opts.VendorHost
		cfg.VendorPeer = peer
		cfg.VendorPort = port
		cfg.VendorSSL = ssl
		cfg.VendorKey = key
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
