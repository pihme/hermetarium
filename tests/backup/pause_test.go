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

// restic does not return. The pause bound must stop it and unpause.
func TestPauseBound(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	writeHabitat(t, root, id)
	journal := filepath.Join(root, "journal")
	if err := os.WriteFile(journal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(t.TempDir(), "docker")
	stamp := filepath.Join(t.TempDir(), "backup-started")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\n  exec sleep 120\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, journal, []string{
		"BACKUP_STAMP=" + stamp,
		"DOCKER_LOG=" + dockerLog,
		"HERMETARIUM_PAUSE_SECONDS=1",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	started := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	if time.Since(started) > 15*time.Second {
		t.Fatalf("backup.sh ran %s, want the pause bound\n%s", time.Since(started), out)
	}
	if _, statErr := os.Stat(stamp); statErr != nil {
		t.Fatalf("restic backup did not start: %v\n%s", statErr, out)
	}
	got, readErr := os.ReadFile(dockerLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "pause\nunpause\n" {
		t.Fatalf("docker actions:\n%s\n%s", got, out)
	}
	if strings.Contains(string(out), "stayed paused") {
		t.Fatalf("unpause failed\n%s", out)
	}
}

// timeout leaves the shell's process group. SIGTERM to the shell alone
// must unpause without waiting out the pause bound.
func TestBackupUnpauseOnSignal(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	writeHabitat(t, root, id)
	journal := filepath.Join(root, "journal")
	if err := os.WriteFile(journal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(t.TempDir(), "docker")
	stamp := filepath.Join(t.TempDir(), "backup-started")
	resticPid := filepath.Join(t.TempDir(), "restic.pid")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  echo $$ > \"$RESTIC_PID\"\n  : > \"$BACKUP_STAMP\"\n  exec sleep 60\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, journal, []string{
		"BACKUP_STAMP=" + stamp,
		"RESTIC_PID=" + resticPid,
		"DOCKER_LOG=" + dockerLog,
		"HERMETARIUM_PAUSE_SECONDS=30",
	})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// timeout's process group is not the shell's. A failed test must
		// not leave the stand-in restic sleeping.
		b, readErr := os.ReadFile(resticPid)
		if readErr != nil {
			return
		}
		pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if convErr != nil || pid <= 1 {
			return
		}
		out, psErr := exec.Command("ps", "-o", "pgid=", "-p", strconv.Itoa(pid)).Output()
		if psErr == nil {
			pgid, pgErr := strconv.Atoi(strings.TrimSpace(string(out)))
			if pgErr == nil && pgid > 1 {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("backup did not reach restic\n%s", buf.String())
		}
		if _, statErr := os.Stat(stamp); statErr == nil {
			logged, readErr := os.ReadFile(dockerLog)
			if readErr == nil && strings.Contains(string(logged), "pause") {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("backup.sh exited 0\n%s", buf.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("SIGTERM waited out the pause bound\n%s", buf.String())
	}
	got, readErr := os.ReadFile(dockerLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "pause\nunpause\n" {
		t.Fatalf("docker actions:\n%s\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "stayed paused") {
		t.Fatalf("unpause failed\n%s", buf.String())
	}
}

// A signal during docker pause used to run the trap before paused=1.
func TestBackupUnpauseOnSignalDuringPause(t *testing.T) {
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := "hab1"
	writeHabitat(t, root, id)
	journal := filepath.Join(root, "journal")
	if err := os.WriteFile(journal, []byte("journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(t.TempDir(), "docker")
	writeExe(t, filepath.Join(bin, "docker"), `#!/bin/sh
cmd=$1
prev=
fmt=
for a in "$@"; do
  if [ "$prev" = "--format" ]; then
    fmt=$a
  fi
  prev=$a
done
case "$cmd" in
  pause)
    if [ -n "${DOCKER_LOG:-}" ]; then
      printf '%s\n' pause >> "$DOCKER_LOG"
    fi
    exec sleep 30
    ;;
  unpause)
    if [ -n "${DOCKER_LOG:-}" ]; then
      printf '%s\n' unpause >> "$DOCKER_LOG"
    fi
    exit 0
    ;;
esac
case "$fmt" in
  */log*) printf '%s\n' "$LOG_PATH" ;;
  *) exit 1 ;;
esac
`)
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nexit 0\n")
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, journal, []string{
		"DOCKER_LOG=" + dockerLog,
		"HERMETARIUM_PAUSE_SECONDS=30",
	})
	cmd := exec.Command(filepath.Join(checkout, "omakase", "backup.sh"))
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("docker pause did not start\n%s", buf.String())
		}
		logged, readErr := os.ReadFile(dockerLog)
		if readErr == nil && strings.Contains(string(logged), "pause") {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	// The pause client is in this group. The signal has to reach it, or
	// the shell defers the trap until that sleep ends.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("backup.sh exited 0\n%s", buf.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("SIGTERM during pause did not unpause\n%s", buf.String())
	}
	got, readErr := os.ReadFile(dockerLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "pause\nunpause\n" {
		t.Fatalf("docker actions:\n%s\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "stayed paused") {
		t.Fatalf("unpause failed\n%s", buf.String())
	}
}

// A value bash would wrap in $(( )) must be rejected before that arithmetic.
func TestPauseSecondsTooLarge(t *testing.T) {
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
					"GIT_WARDEN_JOURNAL="+journal,
					"HERMETARIUM_PAUSE_SECONDS="+value,
				)
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("backup.sh exited 0\n%s", out)
				}
				if !strings.Contains(string(out), "HERMETARIUM_PAUSE_SECONDS is too large") {
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
				"GIT_WARDEN_JOURNAL="+journal,
				"HERMETARIUM_PAUSE_SECONDS=21600",
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
