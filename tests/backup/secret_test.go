package backup

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

// Stand-ins answer realpath, docker inspect, and hermetarium attach so a
// path that is not secret reaches restic backup. A refusal must stop first.
func TestSecretRefusal(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(checkout, "omakase", "backup.sh")

	root := t.TempDir()
	id := "hab1"
	inst := filepath.Join(root, "var", id)
	if err := os.MkdirAll(filepath.Join(inst, "mitm"), 0o755); err != nil {
		t.Fatal(err)
	}
	squid := filepath.Join(inst, "squid.conf")
	key := filepath.Join(inst, "mitm", "ca.key")
	if err := os.WriteFile(squid, []byte("gate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"io.jsonl", "access.log"} {
		if err := os.WriteFile(filepath.Join(inst, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	safeDisk := filepath.Join(root, "disk.ext4")
	if err := os.WriteFile(safeDisk, []byte("disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	safeJournal := filepath.Join(root, "journal")
	if err := os.WriteFile(safeJournal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "squid-link")
	if err := os.Symlink(squid, link); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(root, "var", "other")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	otherSquid := filepath.Join(otherDir, "squid.conf")
	if err := os.WriteFile(otherSquid, []byte("other-gate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// filepath.Join would clean the ".." away.
	dotdot := filepath.Join(inst, "mitm") + "/../squid.conf"

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\nfi\nexit 0\n")
	writeExe(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf '%s\\n' \"$DISK_PATH\"\n")
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")

	cases := []struct {
		name    string
		disk    string
		journal string
	}{
		{name: "squid.conf", disk: squid, journal: safeJournal},
		{name: "symlink", disk: safeDisk, journal: link},
		{name: "dotdot", disk: safeDisk, journal: dotdot},
		{name: "ca.key", disk: safeDisk, journal: key},
		{name: "dir-slash", disk: safeDisk, journal: inst + "/"},
		{name: "dir", disk: safeDisk, journal: inst},
		{name: "root", disk: safeDisk, journal: "/"},
		{name: "other-squid", disk: safeDisk, journal: otherSquid},
		{name: "other-dir", disk: safeDisk, journal: otherDir},
		{name: "var-parent", disk: safeDisk, journal: root},
	}
	hard := filepath.Join(root, "squid-hard")
	if err := os.Link(squid, hard); err != nil {
		t.Logf("hard link not supported: %v", err)
	} else {
		cases = append(cases, struct {
			name    string
			disk    string
			journal string
		}{name: "hardlink", disk: safeDisk, journal: hard})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stamp := filepath.Join(t.TempDir(), "backup-started")
			// Extra keys come first. A shell keeps the first value of a name.
			env := append([]string{
				"HERMETARIUM_ROOT=" + root,
				"HERMETARIUM=" + herm,
				"HERMETARIUM_ID=" + id,
				"HERMETARIUM_DATA_DISK=" + tc.disk,
				"GIT_WARDEN_JOURNAL=" + tc.journal,
				"RESTIC_REPOSITORY=" + filepath.Join(root, "repo"),
				"RESTIC_PASSWORD=test-password",
				"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"BACKUP_STAMP=" + stamp,
				"DISK_PATH=" + tc.disk,
			}, backupEnv(t, nil)...)
			cmd := exec.Command(script)
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("backup.sh exited 0\n%s", out)
			}
			if !strings.Contains(string(out), "refusing to back up the gate file or the gate private key") {
				t.Fatalf("refusal missing\n%s", out)
			}
			if _, statErr := os.Stat(stamp); statErr == nil {
				t.Fatalf("restic backup started\n%s", out)
			}
		})
	}
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeDockerStub(t *testing.T, path string) {
	t.Helper()
	// DISK_PATH and LOG_PATH are the inspect sources the script compares.
	writeExe(t, path, `#!/bin/sh
prev=
fmt=
for a in "$@"; do
  if [ "$prev" = "--format" ]; then
    fmt=$a
  fi
  prev=$a
done
case "$fmt" in
  */opt/data.ext4*) printf '%s\n' "$DISK_PATH" ;;
  */log*) printf '%s\n' "$LOG_PATH" ;;
  *) exit 1 ;;
esac
`)
}

func writeHabitat(t *testing.T, root, id string) (disk, journal string) {
	t.Helper()
	inst := filepath.Join(root, "var", id)
	if err := os.MkdirAll(filepath.Join(inst, "mitm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "squid.conf"), []byte("gate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "mitm", "ca.key"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"io.jsonl", "access.log"} {
		if err := os.WriteFile(filepath.Join(inst, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	disk = filepath.Join(root, "disk.ext4")
	if err := os.WriteFile(disk, []byte("disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal = filepath.Join(root, "journal")
	if err := os.WriteFile(journal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return disk, journal
}

func scriptEnv(t *testing.T, root, bin, herm, id, disk, journal string, extra []string) []string {
	t.Helper()
	// Go keeps the later value of a repeated key.
	env := append(backupEnv(t, nil),
		"HERMETARIUM_ROOT="+root,
		"HERMETARIUM="+herm,
		"HERMETARIUM_ID="+id,
		"HERMETARIUM_DATA_DISK="+disk,
		"GIT_WARDEN_JOURNAL="+journal,
		"RESTIC_REPOSITORY="+filepath.Join(root, "repo"),
		"RESTIC_PASSWORD=test-password",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DISK_PATH="+disk,
		"LOG_PATH="+filepath.Join(root, "var", id),
	)
	return append(env, extra...)
}

func thawLines(log string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		// The freeze script's text also contains "fsfreeze -u". A real thaw
		// is its own attach, with no shell.
		if strings.Contains(line, "-- fsfreeze -u /data") && !strings.Contains(line, "sh -c") {
			n++
		}
	}
	return n
}

// The helper name is only the habitat id. A /log mount from another root
// must not be treated as this habitat's gate file.
func TestHelperNotThisRoot(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(t.TempDir(), "backup-started")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, disk, journal, []string{
		"BACKUP_STAMP=" + stamp,
		"LOG_PATH=" + t.TempDir(),
	})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	if !strings.Contains(string(out), "is not habitat "+id) {
		t.Fatalf("scope refusal missing\n%s", out)
	}
	if _, statErr := os.Stat(stamp); statErr == nil {
		t.Fatalf("restic backup started\n%s", out)
	}
}

// A failed freeze used to retry fsfreeze -u and say the disk stayed frozen.
func TestFreezeFailureIsNotStayedFrozen(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	callLog := filepath.Join(t.TempDir(), "calls")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALL_LOG\"\nif [ \"$1\" = logs ]; then\n  exit 0\nfi\nexit 1\n")
	env := scriptEnv(t, root, bin, herm, id, disk, journal, []string{"CALL_LOG=" + callLog})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "could not freeze /data") {
		t.Fatalf("freeze error missing\n%s", text)
	}
	if strings.Contains(text, "stayed frozen") {
		t.Fatalf("failed freeze reported a frozen disk\n%s", text)
	}
	calls, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	if n := thawLines(string(calls)); n != 1 {
		t.Fatalf("thaw attempts %d, want 1\n%s", n, calls)
	}
}

// restic does not return. The bound must stop it and thaw.
func TestFreezeBound(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	callLog := filepath.Join(t.TempDir(), "calls")
	stamp := filepath.Join(t.TempDir(), "backup-started")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\n  exec sh -c 'i=0; while [ \"$i\" -lt 120 ]; do i=$((i+1)); sleep 1; done' hermetarium-restic-hang\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALL_LOG\"\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, disk, journal, []string{
		"CALL_LOG=" + callLog,
		"BACKUP_STAMP=" + stamp,
		"HERMETARIUM_FREEZE_SECONDS=1",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	t.Cleanup(func() { killMatching("hermetarium-restic-hang") })
	started := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("backup.sh ran %s, want the freeze bound\n%s", time.Since(started), out)
	}
	if _, statErr := os.Stat(stamp); statErr != nil {
		t.Fatalf("restic backup did not start: %v\n%s", statErr, out)
	}
	calls, readErr := os.ReadFile(callLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if thawLines(string(calls)) < 1 {
		t.Fatalf("restic hang did not thaw\n%s\n%s", calls, out)
	}
	if strings.Contains(string(out), "stayed frozen") {
		t.Fatalf("thaw failed\n%s", out)
	}
}

// SIGKILL skips the EXIT trap. The guest timer must still thaw.
func TestGuestThawAfterKill(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	killMatching("hermetarium-thaw.current")
	killMatching("hermetarium-restic-hang")
	_ = os.Remove("/tmp/hermetarium-thaw.current")
	t.Cleanup(func() {
		killMatching("hermetarium-restic-hang")
		killMatching("hermetarium-thaw.current")
		_ = os.Remove("/tmp/hermetarium-thaw.current")
	})

	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	freezeLog := filepath.Join(t.TempDir(), "fsfreeze")
	stamp := filepath.Join(t.TempDir(), "backup-started")
	// Ahead of the real fsfreeze, so this test cannot freeze the host.
	writeExe(t, filepath.Join(bin, "fsfreeze"), "#!/bin/sh\nprintf '%s %s\\n' \"$(date +%s)\" \"$*\" >> \"$FSFREEZE_LOG\"\nexit 0\n")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\n  exec sh -c 'i=0; while [ \"$i\" -lt 120 ]; do i=$((i+1)); sleep 1; done' hermetarium-restic-hang\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
if [ "$1" = logs ]; then
  exit 0
fi
if [ "$1" != attach ]; then
  exit 1
fi
shift
shift
if [ "$1" != "--" ]; then
  exit 1
fi
shift
exec "$@"
`)
	env := scriptEnv(t, root, bin, herm, id, disk, journal, []string{
		"BACKUP_STAMP=" + stamp,
		"FSFREEZE_LOG=" + freezeLog,
		"HERMETARIUM_FREEZE_SECONDS=5",
	})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	seenFreeze := time.Time{}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-waited:
			t.Fatalf("backup.sh exited before the kill\n%s", out.String())
		default:
		}
		body, _ := os.ReadFile(freezeLog)
		if _, err := os.Stat(stamp); err == nil && strings.Contains(string(body), "-f /data") {
			seenFreeze = time.Now()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seenFreeze.IsZero() {
		t.Fatalf("disk was not frozen\n%s\n%s", out.String(), mustRead(freezeLog))
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("backup.sh survived SIGKILL")
	}
	if strings.Contains(mustRead(freezeLog), "-u /data") {
		t.Fatalf("thaw ran before the guest timer\n%s", mustRead(freezeLog))
	}
	killMatching("hermetarium-restic-hang")

	// Guest sleep is HERMETARIUM_FREEZE_SECONDS+30. An immediate thaw is the trap.
	until := time.Now().Add(50 * time.Second)
	for time.Now().Before(until) {
		if strings.Contains(mustRead(freezeLog), "-u /data") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(mustRead(freezeLog), "-u /data") {
		t.Fatalf("guest timer did not thaw\n%s\n%s", mustRead(freezeLog), out.String())
	}
	if time.Since(seenFreeze) < 25*time.Second {
		t.Fatalf("thaw was too soon (%s), want the guest timer\n%s", time.Since(seenFreeze), mustRead(freezeLog))
	}
}

func mustRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func killMatching(pattern string) {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
