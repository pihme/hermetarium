package backup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

// Stand-ins answer realpath and docker inspect so a path that is not secret
// reaches restic backup. A refusal must stop first.
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
	for _, name := range []string{"io.jsonl", "access.log", "rootfs.ext4"} {
		if err := os.WriteFile(filepath.Join(inst, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
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
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")

	cases := []struct {
		name    string
		journal string
	}{
		{name: "squid.conf", journal: squid},
		{name: "symlink", journal: link},
		{name: "dotdot", journal: dotdot},
		{name: "ca.key", journal: key},
		{name: "dir-slash", journal: inst + "/"},
		{name: "dir", journal: inst},
		{name: "root", journal: "/"},
		{name: "other-squid", journal: otherSquid},
		{name: "other-dir", journal: otherDir},
		{name: "var-parent", journal: root},
	}
	hard := filepath.Join(root, "squid-hard")
	if err := os.Link(squid, hard); err != nil {
		t.Logf("hard link not supported: %v", err)
	} else {
		cases = append(cases, struct {
			name    string
			journal string
		}{name: "hardlink", journal: hard})
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
			journal string
		}{name: "hardlink-dir", journal: hardDir})
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
			journal string
		}{name: "bind-mount", journal: bindJournal})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stamp := filepath.Join(t.TempDir(), "backup-started")
			// Extra keys come first. A shell keeps the first value of a name.
			env := append([]string{
				"HERMETARIUM_ROOT=" + root,
				"HERMETARIUM=" + herm,
				"HERMETARIUM_ID=" + id,
				"GIT_WARDEN_JOURNAL=" + tc.journal,
				"RESTIC_REPOSITORY=" + filepath.Join(root, "repo"),
				"RESTIC_PASSWORD=test-password",
				"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"BACKUP_STAMP=" + stamp,
				"LOG_PATH=" + inst,
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
	writeHabitat(t, root, id)
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
	cmd.Env = scriptEnv(t, root, bin, herm, id, journal, []string{
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
	// LOG_PATH is the /log mount the script compares. DOCKER_LOG records pause.
	writeExe(t, path, `#!/bin/sh
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
  pause|unpause)
    if [ -n "${DOCKER_LOG:-}" ]; then
      printf '%s\n' "$cmd" >> "$DOCKER_LOG"
    fi
    exit 0
    ;;
esac
case "$fmt" in
  */log*) printf '%s\n' "$LOG_PATH" ;;
  *) exit 1 ;;
esac
`)
}

func writeHabitat(t *testing.T, root, id string) {
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
	for _, name := range []string{"io.jsonl", "access.log", "rootfs.ext4"} {
		if err := os.WriteFile(filepath.Join(inst, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func scriptEnv(t *testing.T, root, bin, herm, id, journal string, extra []string) []string {
	t.Helper()
	// Go keeps the later value of a repeated key.
	env := append(backupEnv(t, nil),
		"HERMETARIUM_ROOT="+root,
		"HERMETARIUM="+herm,
		"HERMETARIUM_ID="+id,
		"GIT_WARDEN_JOURNAL="+journal,
		"RESTIC_REPOSITORY="+filepath.Join(root, "repo"),
		"RESTIC_PASSWORD=test-password",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LOG_PATH="+filepath.Join(root, "var", id),
	)
	return append(env, extra...)
}

// The Firecracker container name is only the habitat id. A /log mount from
// another root must not be treated as this habitat's gate file.
func TestFirecrackerContainerNotThisRoot(t *testing.T) {
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
	stamp := filepath.Join(t.TempDir(), "backup-started")
	writeExe(t, filepath.Join(bin, "restic"), "#!/bin/sh\nif [ \"$1\" = backup ]; then\n  : > \"$BACKUP_STAMP\"\nfi\nexit 0\n")
	writeDockerStub(t, filepath.Join(bin, "docker"))
	herm := filepath.Join(root, "hermetarium")
	writeExe(t, herm, "#!/bin/sh\nexit 0\n")
	env := scriptEnv(t, root, bin, herm, id, journal, []string{
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
