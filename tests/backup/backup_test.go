package backup

import (
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const backupImage = "hermetarium-backup:local"

// Only in the gate file passed to create. It must not come back out of the repository.
const modelKey = "htm-fake-model-key-64-DO-NOT-BACKUP"

// Planted at var/<id>/mitm/ca.key. The backup must leave it behind.
const gateKey = "htm-fake-gate-private-key-64-DO-NOT-BACKUP"

const (
	resticVer = "0.19.1"
	resticURL = "https://github.com/restic/restic/releases/download/v0.19.1/restic_0.19.1_linux_amd64.bz2"
	// SHA256SUMS from the v0.19.1 release, file restic_0.19.1_linux_amd64.bz2.
	resticBZ2SHA = "f415415624dcc452f2a02b8c33641791a8c6d6d3b65bbb3543fcf9a25151585c"
)

const knownText = "known-v1"

func TestBackupWhileWriting(t *testing.T) {
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
	cli := buildCLI(t, checkout)
	root := checkout

	acl := filepath.Join(t.TempDir(), "squid.conf")
	echo, err := os.ReadFile(filepath.Join(checkout, "examples", "echo", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(echo, []byte(modelKey)) {
		t.Fatal("echo gate file already contains the test key")
	}
	if err := os.WriteFile(acl, append(echo, []byte("\n# "+modelKey+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst, err := supervisor.CreateStrong(root, id, supervisor.WithTestLabel(t, supervisor.CreateOpts{
		Image: backupImage, ACL: acl, NoInbound: true,
		MemMiB: 128, DiskMB: 256,
	}))
	if err != nil {
		_ = supervisor.DestroyStrong(id)
		t.Fatal(err)
	}
	defer supervisor.DestroyStrong(id)

	serial := waitSerial(t, inst.FirecrackerContainer, "WRITE_1")
	if !strings.Contains(serial, "KNOWN_READY") || !strings.Contains(serial, "FIRST_BOOT") {
		t.Fatalf("writer did not start\n%s", serial)
	}
	assertOneRootDrive(t, filepath.Join(root, "var", id, "vm.json"))

	instDir := filepath.Join(root, "var", id)
	gateFile, err := os.ReadFile(filepath.Join(instDir, "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gateFile, []byte(modelKey)) {
		t.Fatal("running gate file does not contain the model key")
	}
	if err := os.MkdirAll(filepath.Join(instDir, "mitm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "mitm", "ca.key"), []byte(gateKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
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

	rootfs := filepath.Join(instDir, "rootfs.ext4")
	script := filepath.Join(checkout, "omakase", "backup.sh")
	first := runBackup(t, script, env)
	if first.TotalBytesProcessed < fileSize(t, rootfs) {
		t.Fatalf("first backup processed %d bytes, root file is %d", first.TotalBytesProcessed, fileSize(t, rootfs))
	}
	t.Logf("first backup added %d bytes, processed %d", first.DataAdded, first.TotalBytesProcessed)

	// Immediately, while the guest has changed only a little. A later check
	// would let the writer run for as long as e2fsck takes.
	second := runBackup(t, script, env)
	returned, _ := supervisor.Docker(10*time.Second, "logs", inst.FirecrackerContainer)
	atReturn := lastWrite(returned)
	if atReturn < 1 {
		t.Fatalf("no WRITE_n when backup.sh returned\n%s", returned)
	}
	t.Logf("second backup added %d bytes (files new %d, changed %d, unmodified %d)",
		second.DataAdded, second.FilesNew, second.FilesChanged, second.FilesUnmodified)
	if second.FilesNew != 0 {
		t.Fatalf("second backup treated files as new (%d)", second.FilesNew)
	}
	// FilesNew == 0 still allows another copy of every unique block.
	// An ext4 journal of a few MiB was 27% of the first snapshot on CI
	// (about 6 MiB) and failed a 25% ceiling. Half still rejects a full recopy.
	if first.DataAdded < 1 || second.DataAdded*2 >= first.DataAdded {
		t.Fatalf("second backup added %d bytes, first added %d", second.DataAdded, first.DataAdded)
	}

	restore := filepath.Join(t.TempDir(), "restore")
	restoreCmd := exec.Command(resticArg(resticBin), "restore", "latest", "--target", restore)
	restoreCmd.Env = env
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		t.Fatalf("restic restore: %v\n%s", err, out)
	}
	restoredDisk := filepath.Join(restore, strings.TrimPrefix(rootfs, string(os.PathSeparator)))
	read := checkRestored(t, restoredDisk)
	if !strings.Contains(read, "KNOWN="+knownText) {
		t.Fatalf("restored known file:\n%s", read)
	}
	// /churn is written after sync, and docker pause does not flush it.
	assertTreeHas(t, restore, "warden-journal-marker")
	assertTreeHas(t, restore, "io.jsonl")
	assertTreeMissing(t, restore, modelKey)
	assertTreeMissing(t, restore, gateKey)
	ls := resticLS(t, resticBin, env)
	for _, bad := range []string{"squid.conf", "ca.key"} {
		if strings.Contains(ls, bad) {
			t.Fatalf("snapshot lists %s\n%s", bad, ls)
		}
	}
	for _, need := range []string{"io.jsonl", "access.log", filepath.Base(journal), "rootfs.ext4"} {
		if !strings.Contains(ls, need) {
			t.Fatalf("snapshot missing %s\n%s", need, ls)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	after := atReturn
	for time.Now().Before(deadline) {
		serial, _ = supervisor.Docker(10*time.Second, "logs", inst.FirecrackerContainer)
		after = lastWrite(serial)
		if after > atReturn {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if after <= atReturn {
		t.Fatalf("habitat stopped writing after backup.sh returned (%d then %d)", atReturn, after)
	}

	// destroy deletes the habitat copy. The restored file stays, and a new
	// strong wall boots that file instead of packing the image again.
	before, err := os.Stat(restoredDisk)
	if err != nil {
		t.Fatal(err)
	}
	destroyCLI(t, cli, root, id)
	if _, err := os.Stat(rootfs); !os.IsNotExist(err) {
		t.Fatalf("destroy left the root file: %v", err)
	}
	id2, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.DestroyStrong(id2)
		_ = os.Remove(filepath.Join(root, "var", id2, "rootfs.ext4"))
	}()
	create := exec.Command(cli, "create", "--wall", "strong", "--rootfs", restoredDisk,
		"--image", backupImage, "--acl", acl, "--no-inbound", "--mem", "128", "--id", id2)
	create.Env = append(os.Environ(), "HERMETARIUM_ROOT="+root)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create --rootfs: %v\n%s", err, out)
	}
	restoredSerial := waitSerial(t, "htm-fc-"+id2, "RESTORED_BOOT")
	if !strings.Contains(restoredSerial, "KNOWN="+knownText) {
		t.Fatalf("restored boot did not see the known file\n%s", restoredSerial)
	}
	if strings.Contains(restoredSerial, "FIRST_BOOT") {
		t.Fatal("create --rootfs packed a fresh image")
	}
	assertOneRootDrive(t, filepath.Join(root, "var", id2, "vm.json"))
	copyPath := filepath.Join(root, "var", id2, "rootfs.ext4")
	if same, err := sameFile(restoredDisk, copyPath); err != nil || same {
		t.Fatalf("habitat copy is not a distinct file: same=%v err=%v", same, err)
	}
	destroyCLI(t, cli, root, id2)
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("destroy left the copy: %v", err)
	}
	afterStat, err := os.Stat(restoredDisk)
	if err != nil {
		t.Fatalf("destroy removed the restored file: %v", err)
	}
	if !os.SameFile(before, afterStat) {
		t.Fatal("destroy replaced the restored file")
	}
}

func assertOneRootDrive(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vm struct {
		BootSource struct {
			BootArgs string `json:"boot_args"`
		} `json:"boot-source"`
		Drives []struct {
			DriveID string `json:"drive_id"`
			Path    string `json:"path_on_host"`
			Root    bool   `json:"is_root_device"`
		} `json:"drives"`
	}
	if err := json.Unmarshal(b, &vm); err != nil {
		t.Fatal(err)
	}
	if len(vm.Drives) != 1 || vm.Drives[0].DriveID != "rootfs" || !vm.Drives[0].Root || vm.Drives[0].Path != "/opt/rootfs.ext4" {
		t.Fatalf("drives %+v", vm.Drives)
	}
	if strings.Contains(vm.BootSource.BootArgs, "hermetarium.data=1") {
		t.Fatalf("boot args %s", vm.BootSource.BootArgs)
	}
}

func destroyCLI(t *testing.T, cli, root, id string) {
	t.Helper()
	cmd := exec.Command(cli, "destroy", id)
	cmd.Env = append(os.Environ(), "HERMETARIUM_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("destroy %s: %v\n%s", id, err, out)
	}
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

type backupSummary struct {
	MessageType         string `json:"message_type"`
	FilesNew            int    `json:"files_new"`
	FilesChanged        int    `json:"files_changed"`
	FilesUnmodified     int    `json:"files_unmodified"`
	DataAdded           int64  `json:"data_added"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
}

func runBackup(t *testing.T, script string, env []string) backupSummary {
	t.Helper()
	cmd := exec.Command(script)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	t.Logf("backup stderr:\n%s", stderr.String())
	if err != nil {
		t.Fatalf("backup: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	var sum backupSummary
	found := false
	for _, line := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var one backupSummary
		if json.Unmarshal(line, &one) != nil || one.MessageType != "summary" {
			continue
		}
		sum = one
		found = true
	}
	if !found {
		t.Fatalf("no restic summary\n%s", stdout.String())
	}
	return sum
}

func checkRestored(t *testing.T, disk string) string {
	t.Helper()
	out, err := supervisor.Docker(2*time.Minute, "run", "--rm", "--privileged", "--entrypoint", "sh",
		"-v", disk+":/disk.ext4",
		supervisor.AlpineImage, "-c", `
set -eu
apk add --no-cache e2fsprogs >/dev/null
e2fsck -p /disk.ext4 || test $? -le 1
mkdir -p /mnt
mount -t ext4 -o loop,ro,noexec,nosuid,nodev /disk.ext4 /mnt
echo "KNOWN=$(cat /mnt/known)"
if [ -f /mnt/churn ]; then echo "CHURN=$(cat /mnt/churn)"; else echo CHURN=missing; fi
`)
	if err != nil {
		t.Fatalf("read restored root file: %v\n%s", err, out)
	}
	return out
}

func assertTreeHas(t *testing.T, root, needle string) {
	t.Helper()
	if !treeScan(t, root, needle) {
		t.Fatalf("%s does not contain %s", root, needle)
	}
}

func assertTreeMissing(t *testing.T, root, needle string) {
	t.Helper()
	if treeScan(t, root, needle) {
		t.Fatalf("%s contains %s", root, needle)
	}
}

func treeScan(t *testing.T, root, needle string) bool {
	t.Helper()
	want := []byte(needle)
	found := false
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if info.Mode()&os.ModeType != 0 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, want) || strings.Contains(path, needle) {
			found = true
			return io.EOF
		}
		return nil
	})
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return found
}

func resticLS(t *testing.T, bin string, env []string) string {
	t.Helper()
	cmd := exec.Command(resticArg(bin), "ls", "latest")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restic ls: %v\n%s", err, out)
	}
	return string(out)
}

func backupEnv(t *testing.T, extra []string) []string {
	t.Helper()
	drop := map[string]bool{
		"RESTIC_REPOSITORY": true, "RESTIC_PASSWORD": true, "RESTIC_PASSWORD_FILE": true,
		"RESTIC_PASSWORD_COMMAND": true, "RESTIC_CACHE_DIR": true,
		"HERMETARIUM_ROOT": true, "HERMETARIUM": true, "HERMETARIUM_ID": true,
		"GIT_WARDEN_JOURNAL": true, "HERMETARIUM_BACKUP_SCRIPT": true,
		"HERMETARIUM_PAUSE_SECONDS": true,
		"PATH":                      true,
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || drop[k] {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// resticArg is the path passed to exec.Command. Go resolves a bare name
// with the test process PATH, before cmd.Env, so a downloaded binary
// has to be named by its absolute path. An empty bin means PATH already
// has restic.
func resticArg(bin string) string {
	if bin == "" {
		return "restic"
	}
	return bin
}

func pathWith(resticBin string) string {
	path := os.Getenv("PATH")
	if resticBin == "" {
		return path
	}
	return filepath.Dir(resticBin) + string(os.PathListSeparator) + path
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func buildCLI(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "hermetarium")
	cmd := exec.Command("go", "build", "-o", bin, "./supervisor/cmd/hermetarium")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build hermetarium: %v\n%s", err, out)
	}
	return bin
}

func buildBackupImage(root string) error {
	_, err := supervisor.Docker(3*time.Minute, "build", "-t", backupImage,
		filepath.Join(root, "tests", "backup"))
	return err
}

func waitSerial(t *testing.T, fcContainer, needle string) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var serial string
	for time.Now().Before(deadline) {
		serial, _ = supervisor.Docker(10*time.Second, "logs", fcContainer)
		if strings.Contains(serial, needle) {
			return serial
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("missing %s\n%s", needle, serial)
	return ""
}

func lastWrite(s string) int {
	n := 0
	const mark = "WRITE_"
	for _, field := range strings.Fields(s) {
		if !strings.HasPrefix(field, mark) {
			continue
		}
		v, err := strconv.Atoi(field[len(mark):])
		if err != nil || v <= n {
			continue
		}
		n = v
	}
	return n
}

// ensureRestic returns a restic binary path. An empty string means PATH already has one.
// CI's Ubuntu image does not ship restic. The binary is not committed.
func ensureRestic(t *testing.T, root string) string {
	t.Helper()
	if _, err := exec.LookPath("restic"); err == nil {
		return ""
	}
	dir := filepath.Join(supervisor.CacheDir(root), "restic-"+resticVer)
	bin := filepath.Join(dir, "restic")
	stamp := filepath.Join(dir, "sha256")
	if b, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(b)) == resticBZ2SHA {
		if st, err := os.Stat(bin); err == nil && st.Mode()&0o111 != 0 {
			return bin
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fetchRestic(bin); err != nil {
		t.Fatalf("restic is not on PATH and the download failed: %v", err)
	}
	if err := os.WriteFile(stamp, []byte(resticBZ2SHA+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return bin
}

func fetchRestic(dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resticURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", resticURL, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "restic-*.bz2")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != resticBZ2SHA {
		return fmt.Errorf("restic checksum %s, want %s", sum, resticBZ2SHA)
	}
	f, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer f.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, bzip2.NewReader(f)); err != nil {
		return err
	}
	return nil
}
