package backup

import (
	"bytes"
	"context"
	"errors"
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
	// A directory that is not an ancestor of var/ and does not contain the
	// canonical path, only another name for the same inode.
	hardDir := filepath.Join(root, "hard-journal")
	if err := os.MkdirAll(hardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(squid, filepath.Join(hardDir, "alias.conf")); err != nil {
		t.Logf("hard link not supported: %v", err)
	} else {
		cases = append(cases, struct {
			name    string
			disk    string
			journal string
		}{name: "hardlink-dir", disk: safeDisk, journal: hardDir})
	}
	// Not an ancestor of var/. The key inode is reachable only through this
	// bind mount. find must see it; restic would archive it.
	bindJournal := filepath.Join(root, "bind-journal")
	mountPoint := filepath.Join(bindJournal, "mitm")
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount(filepath.Join(inst, "mitm"), mountPoint, "", syscall.MS_BIND, ""); err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Logf("bind mount not permitted: %v", err)
		} else {
			t.Fatalf("bind mount: %v", err)
		}
	} else {
		t.Cleanup(func() {
			if err := syscall.Unmount(mountPoint, 0); err != nil {
				t.Errorf("unmount bind mount: %v", err)
			}
		})
		cases = append(cases, struct {
			name    string
			disk    string
			journal string
		}{name: "bind-mount", disk: safeDisk, journal: bindJournal})
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

// find exits non-zero when one subdirectory cannot be read, after listing
// the rest. That used to be reported as a gate file. The backup still
// must not start.
func TestSecretRefusalWalk(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	disk, _ := writeHabitat(t, root, id)
	journal := filepath.Join(root, "journal-dir")
	if err := os.MkdirAll(journal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journal, "notes"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sealed := filepath.Join(journal, "unreadable-walk")
	if err := os.Mkdir(sealed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o755) })
	if f, openErr := os.Open(sealed); openErr == nil {
		f.Close()
		t.Fatal("unreadable directory is readable")
	}

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(t.TempDir(), "backup-started")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\nfi\nexit 0\n")
	writeExe(t, filepath.Join(bin, "docker"), "#!/bin/sh\nexit 0\n")
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = scriptEnv(t, root, bin, herm, id, disk, journal, []string{
		"BACKUP_STAMP=" + stamp,
	})
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	text := string(out)
	if strings.Contains(text, "gate file") || strings.Contains(text, "gate private key") {
		t.Fatalf("walk error reported as a gate file\n%s", text)
	}
	if !strings.Contains(text, sealed) {
		t.Fatalf("walk error missing\n%s", text)
	}
	if _, statErr := os.Stat(stamp); statErr == nil {
		t.Fatalf("restic backup started\n%s", text)
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
		// The freeze script's sleeper text also contains "fsfreeze -u",
		// on the setsid line. A thaw is its own attach.
		if strings.Contains(line, "fsfreeze -u /data") && !strings.Contains(line, "setsid") {
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
	// Do not match hermetarium-thaw.current: a live host attach has that
	// path in its argv. The stamp is the token this run writes.
	killMatching("hermetarium-restic-hang")
	var stamp string
	t.Cleanup(func() {
		if validThawStamp(stamp) {
			killMatching(stamp)
		}
		killMatching("hermetarium-restic-hang")
	})

	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	freezeLog := filepath.Join(t.TempDir(), "fsfreeze")
	backupStamp := filepath.Join(t.TempDir(), "backup-started")
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
		"BACKUP_STAMP=" + backupStamp,
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
		if _, err := os.Stat(backupStamp); err == nil && strings.Contains(string(body), "-f /data") {
			seenFreeze = time.Now()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seenFreeze.IsZero() {
		t.Fatalf("disk was not frozen\n%s\n%s", out.String(), mustRead(freezeLog))
	}
	stamp = strings.TrimSpace(mustRead("/tmp/hermetarium-thaw.current"))
	if !validThawStamp(stamp) {
		t.Fatalf("thaw stamp missing\n%s", mustRead("/tmp/hermetarium-thaw.current"))
	}
	// pgid is the process group Setpgid started for this backup.sh.
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

	// Sleep is HERMETARIUM_FREEZE_SECONDS+30, started after fsfreeze -f.
	// An immediate thaw is the trap, not this timer.
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

// A value bash would wrap in $(( )) must be rejected before that arithmetic.
func TestFreezeSecondsTooLarge(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(checkout, "omakase", "backup.sh")
	journal := filepath.Join(t.TempDir(), "journal")
	if err := os.WriteFile(journal, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shells := []string{"sh"}
	if _, err := exec.LookPath("bash"); err == nil {
		shells = append(shells, "bash")
	}
	tooBig := []string{"18446744073709551616", "21601", "000000000021601"}
	for _, sh := range shells {
		for _, value := range tooBig {
			t.Run(sh+"/"+value, func(t *testing.T) {
				cmd := exec.Command(sh, script)
				cmd.Env = append(backupEnv(t, nil),
					"RESTIC_REPOSITORY="+t.TempDir(),
					"RESTIC_PASSWORD=test-password",
					"HERMETARIUM_ID=hab1",
					"HERMETARIUM_DATA_DISK="+journal,
					"GIT_WARDEN_JOURNAL="+journal,
					"HERMETARIUM_FREEZE_SECONDS="+value,
				)
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("backup.sh exited 0\n%s", out)
				}
				if !strings.Contains(string(out), "HERMETARIUM_FREEZE_SECONDS is too large") {
					t.Fatalf("bound accepted\n%s", out)
				}
			})
		}
		t.Run(sh+"/21600", func(t *testing.T) {
			cmd := exec.Command(sh, script)
			cmd.Env = append(backupEnv(t, nil),
				"RESTIC_REPOSITORY="+t.TempDir(),
				"RESTIC_PASSWORD=test-password",
				"HERMETARIUM_ID=hab1",
				"HERMETARIUM_DATA_DISK="+journal,
				"GIT_WARDEN_JOURNAL="+journal,
				"HERMETARIUM_FREEZE_SECONDS=21600",
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("backup.sh exited 0\n%s", out)
			}
			if strings.Contains(string(out), "too large") {
				t.Fatalf("6 hour bound rejected\n%s", out)
			}
		})
	}
}

// The backstop already consumed the token. A later fsfreeze -u failure is not
// "stayed frozen": this run no longer owns the freeze.
func TestConsumedTokenIsNotStayedFrozen(t *testing.T) {
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
	freezeLog := filepath.Join(t.TempDir(), "fsfreeze")
	tokenCopy := filepath.Join(t.TempDir(), "token")
	var stamp string
	t.Cleanup(func() {
		if !validThawStamp(stamp) {
			stamp = strings.TrimSpace(mustRead(tokenCopy))
		}
		if !validThawStamp(stamp) {
			stamp = strings.TrimSpace(mustRead("/tmp/hermetarium-thaw.current"))
		}
		if validThawStamp(stamp) {
			killMatching(stamp)
		}
	})
	writeExe(t, filepath.Join(bin, "fsfreeze"), "#!/bin/sh\nprintf '%s %s\\n' \"$(date +%s)\" \"$*\" >> \"$FSFREEZE_LOG\"\nif [ \"$1\" = -u ]; then exit 1; fi\nexit 0\n")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  cp /tmp/hermetarium-thaw.current \"$TOKEN_STAMP\" || exit 1\n  printf 'done\\n' > /tmp/hermetarium-thaw.current\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, `#!/bin/sh
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
		"FSFREEZE_LOG=" + freezeLog,
		"TOKEN_STAMP=" + tokenCopy,
		"HERMETARIUM_FREEZE_SECONDS=30",
	})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	stamp = strings.TrimSpace(mustRead(tokenCopy))
	if err != nil {
		t.Fatalf("backup.sh: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "stayed frozen") {
		t.Fatalf("consumed token reported a frozen disk\n%s", out)
	}
	log := mustRead(freezeLog)
	if !strings.Contains(log, "-f /data") {
		t.Fatalf("disk was not frozen\n%s", log)
	}
	if strings.Contains(log, "-u /data") {
		t.Fatalf("thaw ran after the token was consumed\n%s", log)
	}
}

// A failed freeze must put the previous thaw token back. The old order wrote
// this run's token, dropped the lock, and left it there when fsfreeze -f
// failed, so cleanup thawed a freeze it did not take.
func TestGuestFreezeToken(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	const thawFile = "/tmp/hermetarium-thaw.current"
	const previous = "previous-owner\n"
	var leaked string
	t.Cleanup(func() {
		if validThawStamp(leaked) {
			killMatching(leaked)
		}
	})

	root := t.TempDir()
	id := "hab1"
	disk, journal := writeHabitat(t, root, id)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Ahead of the real fsfreeze, so this test cannot freeze the host.
	writeExe(t, filepath.Join(bin, "fsfreeze"), `#!/bin/sh
printf '%s\n' "$*" >> "$FSFREEZE_LOG"
if [ "$1" = -f ]; then
  cp /tmp/hermetarium-thaw.current "$FREEZE_TOKEN" 2>/dev/null || true
  if [ "$FAIL_FREEZE" = newer ]; then
    printf '%s\n' "newer-owner" > /tmp/hermetarium-thaw.current
    exit 1
  fi
  if [ "$FAIL_FREEZE" = 1 ]; then
    exit 1
  fi
fi
exit 0
`)
	writeExe(t, filepath.Join(bin, "restic"), `#!/bin/sh
if [ "$1" = backup ]; then
  cp /tmp/hermetarium-thaw.current "$TOKEN_STAMP" || exit 1
  : > "$BACKUP_STAMP"
fi
exit 0
`)
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, `#!/bin/sh
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

	run := func(t *testing.T, mode string) (out string, freezeLog string, tokenAtFreeze string, tokenAtBackup string) {
		t.Helper()
		if err := os.WriteFile(thawFile, []byte(previous), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		freezeLog = filepath.Join(dir, "fsfreeze")
		freezeToken := filepath.Join(dir, "at-freeze")
		tokenStamp := filepath.Join(dir, "at-backup")
		backupStamp := filepath.Join(dir, "backup-started")
		extra := []string{
			"FSFREEZE_LOG=" + freezeLog,
			"FREEZE_TOKEN=" + freezeToken,
			"TOKEN_STAMP=" + tokenStamp,
			"BACKUP_STAMP=" + backupStamp,
			"HERMETARIUM_FREEZE_SECONDS=1",
		}
		if mode != "" {
			extra = append(extra, "FAIL_FREEZE="+mode)
		}
		cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
		cmd.Env = scriptEnv(t, root, bin, herm, id, disk, journal, extra)
		raw, err := cmd.CombinedOutput()
		out = string(raw)
		tokenAtFreeze = mustRead(freezeToken)
		tokenAtBackup = mustRead(tokenStamp)
		leaked = strings.TrimSpace(tokenAtFreeze)
		if validThawStamp(leaked) {
			killMatching(leaked)
		}
		if mode == "" {
			if err != nil {
				t.Fatalf("backup.sh: %v\n%s", err, out)
			}
			if _, statErr := os.Stat(backupStamp); statErr != nil {
				t.Fatalf("restic backup did not run: %v\n%s", statErr, out)
			}
			return out, mustRead(freezeLog), tokenAtFreeze, tokenAtBackup
		}
		if err == nil {
			t.Fatalf("backup.sh exited 0\n%s", out)
		}
		if _, statErr := os.Stat(backupStamp); statErr == nil {
			t.Fatalf("restic backup started after a failed freeze\n%s", out)
		}
		return out, mustRead(freezeLog), tokenAtFreeze, tokenAtBackup
	}

	t.Run("failed", func(t *testing.T) {
		_, log, atFreeze, _ := run(t, "1")
		if !strings.Contains(log, "-f /data") {
			t.Fatalf("freeze was not attempted\n%s", log)
		}
		if strings.Contains(log, "-u /data") {
			t.Fatalf("failed freeze thawed\n%s", log)
		}
		if strings.TrimSpace(atFreeze) == "previous-owner" {
			t.Fatalf("freeze ran without publishing this run's token\n%s", atFreeze)
		}
		if got := mustRead(thawFile); got != previous {
			t.Fatalf("thaw token %q, want previous owner", got)
		}
	})

	t.Run("newer", func(t *testing.T) {
		_, log, _, _ := run(t, "newer")
		if !strings.Contains(log, "-f /data") {
			t.Fatalf("freeze was not attempted\n%s", log)
		}
		if strings.Contains(log, "-u /data") {
			t.Fatalf("failed freeze thawed a newer owner\n%s", log)
		}
		if got := mustRead(thawFile); got != "newer-owner\n" {
			t.Fatalf("thaw token %q, want the newer owner", got)
		}
	})

	t.Run("succeeded", func(t *testing.T) {
		_, log, atFreeze, atBackup := run(t, "")
		if !strings.Contains(log, "-f /data") {
			t.Fatalf("disk was not frozen\n%s", log)
		}
		got := strings.TrimSpace(atBackup)
		if !validThawStamp(got) {
			t.Fatalf("successful freeze left %q", atBackup)
		}
		if got == strings.TrimSpace(previous) || got != strings.TrimSpace(atFreeze) {
			t.Fatalf("token changed around the freeze\nfreeze %q\nbackup %q", atFreeze, atBackup)
		}
	})
}

func mustRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// validThawStamp is the token backup.sh wrote (epoch-pid). It is not a
// path, so pgrep -f cannot land on a live hermetarium attach.
func validThawStamp(s string) bool {
	left, right, ok := strings.Cut(s, "-")
	if !ok || left == "" || right == "" {
		return false
	}
	for _, part := range []string{left, right} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// killMatching signals processes whose command line contains pattern.
// It does not signal a process group: the matched pid may lead a group
// this test did not start.
func killMatching(pattern string) {
	if !validThawStamp(pattern) && pattern != "hermetarium-restic-hang" {
		return
	}
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
		killPID(pid)
	}
}

func killPID(pid int) {
	out, _ := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	_ = syscall.Kill(pid, syscall.SIGKILL)
	for _, field := range strings.Fields(string(out)) {
		child, err := strconv.Atoi(field)
		if err != nil || child <= 1 || child == pid {
			continue
		}
		_ = syscall.Kill(child, syscall.SIGKILL)
	}
}
