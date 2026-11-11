package supervisor

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed squid/Dockerfile
var squidDockerfile string

// SquidImage is the gate. EnsureSquid builds it from squid/Dockerfile.
const SquidImage = "hermetarium-squid:local"

func InstallACL(dir, src string) error {
	if src == "" {
		return fmt.Errorf("create requires an ACL file (--acl)")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read ACL %s: %w", src, err)
	}
	return os.WriteFile(filepath.Join(dir, "squid.conf"), b, 0o644)
}

// EnsureSquid builds the OpenSSL gate image once. A later create reuses it
// while squid/Dockerfile is unchanged.
func EnsureSquid(root string) error {
	dir := filepath.Join(CacheDir(root), "squid-build")
	stamp := filepath.Join(dir, "Dockerfile")
	have := false
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", SquidImage); err == nil {
		have = true
	}
	old, _ := os.ReadFile(stamp)
	if have && string(old) == squidDockerfile {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(stamp, []byte(squidDockerfile), 0o644); err != nil {
		return err
	}
	_, err := Docker(5*time.Minute, "build", "-t", SquidImage, dir)
	if err != nil {
		return fmt.Errorf("build squid image: %w", err)
	}
	return nil
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

func runSquid(root, name, logDir string, extra []string) error {
	if err := EnsureSquid(root); err != nil {
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

// applyIntercept redirects the box's web traffic (source cidr) to Squid and
// stops the gate from routing anything else. Other containers on the default
// bridge can reach the gate but are not redirected.
func applyIntercept(netns, cidr string, https bool) error {
	if _, err := Docker(30*time.Second, "run", "--rm",
		"--network", "container:"+netns,
		"--cap-add", "NET_ADMIN",
		NetToolsImage,
		"iptables", "-P", "FORWARD", "DROP",
	); err != nil {
		return err
	}
	if err := redirectPort(netns, cidr, "80", "3128"); err != nil {
		return err
	}
	if !https {
		return nil
	}
	return redirectPort(netns, cidr, "443", fmt.Sprintf("%d", HTTPSInterceptPort))
}

func redirectPort(netns, cidr, dport, to string) error {
	_, err := Docker(30*time.Second, "run", "--rm",
		"--network", "container:"+netns,
		"--cap-add", "NET_ADMIN",
		NetToolsImage,
		"iptables", "-t", "nat", "-A", "PREROUTING", "-s", cidr, "-p", "tcp", "--dport", dport,
		"-j", "REDIRECT", "--to-ports", to,
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
