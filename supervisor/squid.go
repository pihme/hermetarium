package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type squidConf struct {
	ListenPort int
	LogDir     string
	ProbeHost  string
	ProbePort  int
}

func WriteSquidACL(root, hostLogDir string) error {
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
	return tmpl.Execute(out, squidConf{
		ListenPort: SquidPort,
		LogDir:     "/log",
		ProbeHost:  ProbeHost,
		ProbePort:  ProbePort,
	})
}

func EnsureSquidImage(root string) error {
	ctx := filepath.Join(root, "squid")
	_, err := Docker(3*time.Minute, "build", "-t", SquidImage, ctx)
	return err
}

func EnsureHelloImage(root string) error {
	ctx := filepath.Join(root, "habitat-hello")
	_, err := Docker(3*time.Minute, "build", "-t", HelloImage, ctx)
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
