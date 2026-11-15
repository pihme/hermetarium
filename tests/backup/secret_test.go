package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
