package backup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
