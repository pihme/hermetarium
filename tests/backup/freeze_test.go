package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

// TestFreezeReports runs backup.sh against a stub attach. ssh errors and a
// missing fsfreeze exit before fsfreeze -f, so the script must not say the
// disk stayed frozen. A freeze that then fails to thaw still reports that.
func TestFreezeReports(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(checkout, "omakase", "backup.sh")
	root := t.TempDir()
	id := "hab1"
	inst := filepath.Join(root, "var", id)
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"squid.conf", "io.jsonl", "access.log"} {
		if err := os.WriteFile(filepath.Join(inst, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	disk := filepath.Join(root, "disk.ext4")
	journal := filepath.Join(root, "journal")
	if err := os.WriteFile(disk, []byte("disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nexit 0\n")
	writeExe(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf '%s\\n' \"$DISK_PATH\"\n")
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, `#!/bin/sh
if [ "$1" = logs ]; then
  exit 0
fi
if [ "$1" != attach ]; then
  exit 0
fi
case " $* " in
  *" fsfreeze -u "*)
    echo "thaw failed" >&2
    exit 1
    ;;
esac
case "$ATTACH_MODE" in
  refused)
    echo "ssh: connect to host 172.16.0.2 port 22: Connection refused" >&2
    ;;
  timeout)
    echo "ssh: connect to host 172.16.0.2 port 22: Connection timed out" >&2
    ;;
  noroute)
    echo "ssh: connect to host 172.16.0.2 port 22: No route to host" >&2
    ;;
  denied)
    echo "root@172.16.0.2: Permission denied (publickey)." >&2
    ;;
  nofsfreeze)
    echo "fsfreeze is not installed in this habitat" >&2
    ;;
  froze)
    exit 0
    ;;
  *)
    echo "stub attach mode" >&2
    ;;
esac
exit 1
`)

	run := func(t *testing.T, mode string) string {
		t.Helper()
		cmd := exec.Command(script)
		cmd.Env = append([]string{
			"HERMETARIUM_ROOT=" + root,
			"HERMETARIUM=" + herm,
			"HERMETARIUM_ID=" + id,
			"HERMETARIUM_DATA_DISK=" + disk,
			"GIT_WARDEN_JOURNAL=" + journal,
			"RESTIC_REPOSITORY=" + filepath.Join(root, "repo"),
			"RESTIC_PASSWORD=test-password",
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"DISK_PATH=" + disk,
			"ATTACH_MODE=" + mode,
		}, backupEnv(t, nil)...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("backup.sh exited 0\n%s", out)
		}
		return string(out)
	}

	for _, mode := range []string{"refused", "timeout", "noroute", "denied", "nofsfreeze"} {
		t.Run(mode, func(t *testing.T) {
			out := run(t, mode)
			if strings.Contains(out, "stayed frozen") {
				t.Fatalf("unfrozen disk reported frozen\n%s", out)
			}
			if strings.Contains(out, "needs fsfreeze") {
				t.Fatalf("fsfreeze claimed\n%s", out)
			}
			if mode == "refused" && !strings.Contains(out, "/usr/sbin/sshd") {
				t.Fatalf("sshd missing\n%s", out)
			}
		})
	}
	t.Run("thaw", func(t *testing.T) {
		out := run(t, "froze")
		if !strings.Contains(out, "stayed frozen") {
			t.Fatalf("thaw failure missing\n%s", out)
		}
	})
}
