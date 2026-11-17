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
	// A habitat id can be reused after destroy. Squid refuses to start when the
	// old squid.pid is still in the instance dir.
	if err := os.Remove(filepath.Join(dir, "squid.pid")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), b, 0o644); err != nil {
		return err
	}
	// Squid has no directive that inserts a path prefix. The gate file passes
	// the prefix as the argument of this helper.
	return os.WriteFile(filepath.Join(dir, "model-rewrite"), []byte(modelRewrite), 0o755)
}

// modelRewrite speaks Squid's url_rewrite protocol for model.hermetarium.test.
const modelRewrite = `#!/bin/sh
prefix=$1
while IFS= read -r line; do
  url=${line%% *}
  case $url in
    http://model.hermetarium.test*|https://model.hermetarium.test*)
      scheme=${url%%://*}
      rest=${url#*://}
      case $rest in
        */*) rest=${rest#*/} ;;
        *) rest= ;;
      esac
      printf 'OK rewrite-url="%s://model.hermetarium.test%s/%s"\n' "$scheme" "$prefix" "$rest"
      ;;
    *)
      printf 'ERR\n'
      ;;
  esac
done
`

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

func netToolsDockerfile() string {
	// iptables is nf_tables. iptables-legacy is the other filter hook: a drop
	// there is invisible to nft list, and an accept in one does not cover it.
	// openssh-client is the strong-wall attach program, in a throwaway container
	// that shares the Firecracker container network namespace. nft deletes
	// table ip filter only when it is the empty iptables-nft accept chains;
	// iptables cannot.
	// busybox-extras supplies /usr/sbin/httpd. Alpine busybox has no httpd
	// applet, and the Firecracker container serves the gate CA with it.
	return "FROM " + AlpineImage + "\nRUN apk add --no-cache iptables iptables-legacy iproute2 openssh-client busybox-extras nftables\n"
}

func EnsureNetTools(root string) error {
	dir := filepath.Join(CacheDir(root), "nettools-build")
	stamp := filepath.Join(dir, "Dockerfile")
	df := netToolsDockerfile()
	have := false
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", NetToolsImage); err == nil {
		have = true
	}
	old, readErr := os.ReadFile(stamp)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if have && readErr == nil && string(old) == df {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Docker needs the Dockerfile in the context. It is also the stamp, and it
	// may match this Dockerfile only after the build succeeds: a failed rebuild
	// would otherwise leave the new stamp on the previous image.
	if err := os.WriteFile(stamp, []byte(df), 0o644); err != nil {
		return err
	}
	if _, err := Docker(3*time.Minute, "build", "-t", NetToolsImage, dir); err != nil {
		if os.IsNotExist(readErr) {
			if rmErr := os.Remove(stamp); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("%w\nremove Dockerfile: %v", err, rmErr)
			}
		} else if werr := os.WriteFile(stamp, old, 0o644); werr != nil {
			return fmt.Errorf("%w\nrestore Dockerfile: %v", err, werr)
		}
		return err
	}
	return nil
}

// The gate image declares VOLUME for /var/log/squid and /var/spool/squid.
// runSquid mounts a tmpfs on those paths so a create does not allocate
// anonymous volumes. -v still removes any that exist; bind mounts stay.
// Volumes left by earlier runs are not removed.
// Release-Notes: The gate mounts tmpfs on /var/log/squid and /var/spool/squid, so a habitat no longer leaves anonymous Docker volumes for those paths. Volumes left by earlier releases still appear in `docker volume ls -qf dangling=true`; inspect before `docker volume rm`.
func removeGate(id string) {
	names := []string{ContainerName(id, "gate"), legacyContainerName(id, "gate")}
	if root, err := Root(); err == nil {
		if inst, err := loadInstance(root, id); err == nil {
			names = append(names, inst.Gate)
		}
	}
	dockerRemove("-fv", names...)
}

func runSquid(root, name, logDir, label string, extra []string) error {
	if err := EnsureSquid(root); err != nil {
		return err
	}
	args := []string{"run", "-d", "--name", name}
	args = append(args, extra...)
	args = append(args, TestLabelArgs(label)...)
	args = append(args,
		"--tmpfs", "/var/log/squid",
		"--tmpfs", "/var/spool/squid",
		"-v", logDir+":/log",
		"--entrypoint", "/usr/sbin/squid",
		SquidImage,
		"-N", "-f", "/log/squid.conf",
	)
	_, err := Docker(30*time.Second, args...)
	return err
}

func WaitSquid(logDir, container string, timeout time.Duration) error {
	pid := filepath.Join(logDir, "squid.pid")
	deadline := time.Now().Add(timeout)
	var last string
	var chmodErr error
	for time.Now().Before(deadline) {
		out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}} {{.State.ExitCode}}", container)
		if err == nil && !strings.Contains(out, "true") {
			logs, _ := Docker(5*time.Second, "logs", container)
			return errWait("squid container exited: " + out + "\n" + logs + "\n" + tailHostFile(filepath.Join(logDir, "cache.log")))
		}
		if _, err := os.Stat(pid); err == nil {
			// The pid lands before access.log and cache.log. Keep trying for
			// the same deadline; an error that outlives it fails create.
			chmodErr = chmodLogs(container, logDir)
			if chmodErr == nil {
				return nil
			}
			last = chmodErr.Error()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		last = "waiting for " + pid
		time.Sleep(200 * time.Millisecond)
	}
	if chmodErr != nil {
		return fmt.Errorf("chmod gate logs: %w", chmodErr)
	}
	logs, _ := Docker(5*time.Second, "logs", container)
	return errWait("squid not ready: " + last + "\n" + logs + "\n" + tailHostFile(filepath.Join(logDir, "cache.log")))
}

func chmodLogs(container, logDir string) error {
	for _, name := range []string{"access.log", "cache.log"} {
		if _, err := os.Stat(filepath.Join(logDir, name)); err != nil {
			return err
		}
	}
	// Root in the running gate can chmod the proxy-owned logs. The unit job
	// has no nettools image; spawning one here used to fail and get discarded.
	_, err := Docker(5*time.Second, "exec", "-u", "root", container,
		"chmod", "a+r", "/log/access.log", "/log/cache.log")
	return err
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
