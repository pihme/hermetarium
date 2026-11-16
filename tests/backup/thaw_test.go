package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

// restic backup fails after the pause. The script fails, and a later WRITE_n
// means the EXIT trap unpaused the Firecracker container.
func TestBackupUnpauseOnResticFailure(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("/dev/kvm not available")
	}
	checkout, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := buildBackupImage(checkout); err != nil {
		t.Fatal(err)
	}
	resticBin := ensureRestic(t, checkout)
	real := resticBin
	if real == "" {
		real, err = exec.LookPath("restic")
		if err != nil {
			t.Fatal(err)
		}
	}
	cli := buildCLI(t, checkout)
	root := checkout

	acl := filepath.Join(t.TempDir(), "squid.conf")
	echo, err := os.ReadFile(filepath.Join(checkout, "examples", "echo", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acl, echo, 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	hab, err := supervisor.CreateStrong(root, id, supervisor.WithTestLabel(t, supervisor.CreateOpts{
		Image: backupImage, ACL: acl, NoInbound: true,
		MemMiB: 128, DiskMB: 256,
	}))
	if err != nil {
		_ = supervisor.DestroyStrong(id)
		t.Fatal(err)
	}
	defer supervisor.DestroyStrong(id)

	serial := waitSerial(t, hab.FirecrackerContainer, "WRITE_1")
	if !strings.Contains(serial, "KNOWN_READY") {
		t.Fatalf("writer did not start\n%s", serial)
	}

	journal := filepath.Join(t.TempDir(), "warden.journal")
	if err := os.WriteFile(journal, []byte("warden-journal-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cache := filepath.Join(t.TempDir(), "cache")
	password := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(password, []byte("backup-test-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := backupEnv(t, []string{
		"HERMETARIUM_ROOT=" + root,
		"HERMETARIUM=" + cli,
		"HERMETARIUM_ID=" + id,
		"GIT_WARDEN_JOURNAL=" + journal,
		"RESTIC_REPOSITORY=" + repo,
		"RESTIC_PASSWORD_FILE=" + password,
		"RESTIC_CACHE_DIR=" + cache,
		"PATH=" + pathWith(resticBin),
	})
	initCmd := exec.Command(resticArg(resticBin), "init")
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("restic init: %v\n%s", err, out)
	}

	stamp := filepath.Join(t.TempDir(), "backup-started")
	wrap := t.TempDir()
	writeExe(t, filepath.Join(wrap, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > "+shSingle(stamp)+"\n  exit 1\nfi\nexec "+shSingle(real)+" \"$@\"\n")
	failEnv := backupEnv(t, []string{
		"HERMETARIUM_ROOT=" + root,
		"HERMETARIUM=" + cli,
		"HERMETARIUM_ID=" + id,
		"GIT_WARDEN_JOURNAL=" + journal,
		"RESTIC_REPOSITORY=" + repo,
		"RESTIC_PASSWORD_FILE=" + password,
		"RESTIC_CACHE_DIR=" + cache,
		"PATH=" + wrap + string(os.PathListSeparator) + pathWith(resticBin),
	})
	script := filepath.Join(checkout, "omakase", "backup.sh")
	cmd := exec.Command(script)
	cmd.Env = failEnv
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("backup.sh exited 0\n%s", out)
	}
	if _, statErr := os.Stat(stamp); statErr != nil {
		t.Fatalf("restic backup did not run after the pause\n%s", out)
	}
	state, _ := supervisor.Docker(10*time.Second, "inspect", "-f", "{{.State.Status}} {{.State.Paused}}", hab.FirecrackerContainer)
	if strings.TrimSpace(state) != "running false" {
		t.Fatalf("Firecracker container not running after a failed backup: %q\n%s", state, out)
	}

	returned, _ := supervisor.Docker(10*time.Second, "logs", hab.FirecrackerContainer)
	atReturn := lastWrite(returned)
	if atReturn < 1 {
		t.Fatalf("no WRITE_n when backup.sh returned\n%s", returned)
	}
	deadline := time.Now().Add(30 * time.Second)
	after := atReturn
	for time.Now().Before(deadline) {
		serial, _ = supervisor.Docker(10*time.Second, "logs", hab.FirecrackerContainer)
		after = lastWrite(serial)
		if after > atReturn {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if after <= atReturn {
		t.Fatalf("habitat stopped writing after a failed backup (%d then %d)\n%s", atReturn, after, out)
	}
}

func shSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
