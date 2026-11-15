package datadisk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const dataImage = "hermetarium-data-disk:local"

func TestDataDiskSurvivesDestroy(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := buildDataImage(root); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(t.TempDir(), "disk.ext4")
	acl := filepath.Join(root, "examples", "echo", "squid.conf")

	first, err := start(t, root, acl, disk)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyStrong(first.ID)
	serial := waitSerial(t, first.Helper, "DATA_READY")
	if !strings.Contains(serial, "DATA_MARKER=missing") {
		t.Fatalf("first boot did not create the marker\n%s", serial)
	}
	st, err := os.Stat(disk)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("data file mode %v", st.Mode().Perm())
	}

	if _, err := start(t, root, acl, disk); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("second attach while running: %v", err)
	}

	if err := supervisor.DestroyStrong(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("destroy removed the data file: %v", err)
	}
	read := hostRead(t, disk)
	if !strings.Contains(read, "MARKER=persisted") || !strings.Contains(read, "EXEC_FAIL") {
		t.Fatalf("host read:\n%s", read)
	}

	second, err := start(t, root, acl, disk)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyStrong(second.ID)
	serial = waitSerial(t, second.Helper, "DATA_MARKER=persisted")
	if !strings.Contains(serial, "DATA_READY") {
		t.Fatalf("second boot\n%s", serial)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, root, acl, disk string) (*supervisor.StrongInstance, error) {
	t.Helper()
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst, err := supervisor.CreateStrong(root, id, supervisor.CreateOpts{
		Image: dataImage, ACL: acl, NoInbound: true,
		MemMiB: 128, DiskMB: 256,
		DataPath: disk, DataMiB: 32,
	})
	if err != nil {
		_ = supervisor.DestroyStrong(id)
		return nil, err
	}
	return inst, nil
}

func waitSerial(t *testing.T, helper, needle string) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var serial string
	for time.Now().Before(deadline) {
		serial, _ = supervisor.Docker(10*time.Second, "logs", helper)
		if strings.Contains(serial, needle) {
			return serial
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("missing %s\n%s", needle, serial)
	return ""
}

func hostRead(t *testing.T, disk string) string {
	t.Helper()
	out, err := supervisor.Docker(2*time.Minute, "run", "--rm", "--privileged", "--entrypoint", "sh",
		"-v", disk+":/disk.ext4",
		supervisor.AlpineImage, "-c", `
set -eu
apk add --no-cache e2fsprogs >/dev/null
e2fsck -p /disk.ext4 || test $? -le 1
mkdir -p /mnt
mount -t ext4 -o loop,ro,noexec,nosuid,nodev /disk.ext4 /mnt
echo MARKER=$(cat /mnt/marker)
if /mnt/runme; then echo EXEC_OK; else echo EXEC_FAIL; fi
`)
	if err != nil {
		t.Fatalf("host mount: %v\n%s", err, out)
	}
	return out
}

func buildDataImage(root string) error {
	_, err := supervisor.Docker(3*time.Minute, "build", "-t", dataImage,
		filepath.Join(root, "tests", "data-disk"))
	return err
}
