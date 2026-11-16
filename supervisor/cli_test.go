package supervisor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestResolveCreate(t *testing.T) {
	if _, err := ResolveCreate(nil); err == nil {
		t.Fatal("expected --image required")
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1"}); err == nil {
		t.Fatal("expected --acl required")
	}
	ex, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf"})
	if err != nil || ex.Image != "myorg/box:1" || ex.ACL != "/tmp/acl.conf" {
		t.Fatalf("got %+v %v", ex, err)
	}
	if ex.MemMiB != 512 || ex.DiskMB != 1024 || ex.NoInbound {
		t.Fatalf("defaults mem=%d disk=%d no-inbound=%v", ex.MemMiB, ex.DiskMB, ex.NoInbound)
	}
	quiet, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--no-inbound"})
	if err != nil || !quiet.NoInbound {
		t.Fatalf("no-inbound %+v %v", quiet, err)
	}
	sized, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--mem", "2048", "--disk", "4096"})
	if err != nil || sized.MemMiB != 2048 || sized.DiskMB != 4096 {
		t.Fatalf("got %+v %v", sized, err)
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--mem", "0"}); err == nil {
		t.Fatal("expected --mem to reject 0")
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--disk", "nope"}); err == nil {
		t.Fatal("expected --disk to reject a non-integer")
	}
	withData, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--data", "/tmp/disk.ext4"})
	if err != nil || withData.DataPath != "/tmp/disk.ext4" || withData.DataMiB != 1024 {
		t.Fatalf("data defaults %+v %v", withData, err)
	}
	sizedData, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--data", "/tmp/disk.ext4", "--data-size", "64"})
	if err != nil || sizedData.DataMiB != 64 {
		t.Fatalf("data size %+v %v", sizedData, err)
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--data-size", "64"}); err == nil {
		t.Fatal("expected --data-size to require --data")
	}
	withRoot, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--rootfs", "/tmp/root.ext4"})
	if err != nil || withRoot.Rootfs != "/tmp/root.ext4" {
		t.Fatalf("rootfs %+v %v", withRoot, err)
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--rootfs"}); err == nil {
		t.Fatal("expected --rootfs to require a file")
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--rootfs", "-root.ext4"}); err == nil {
		t.Fatal("expected --rootfs to reject a flag")
	}
	if ex.CPUs != 0 {
		t.Fatalf("default cpus %d", ex.CPUs)
	}
	withCPU, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--cpus", "4"})
	if err != nil || withCPU.CPUs != 4 {
		t.Fatalf("cpus %+v %v", withCPU, err)
	}
	if _, err := ResolveCreate([]string{"--image", "myorg/box:1", "--acl", "/tmp/acl.conf", "--cpus", "0"}); err == nil {
		t.Fatal("expected --cpus to reject 0")
	}
}

func TestChooseID(t *testing.T) {
	id, err := ChooseID(nil)
	if err != nil || len(id) != 12 {
		t.Fatalf("generated id %q %v", id, err)
	}
	named, err := ChooseID([]string{"--id", "claude"})
	if err != nil || named != "claude" {
		t.Fatalf("name %q %v", named, err)
	}
	for _, bad := range []string{"", "my agent", "../x", "a/b", "-claude", ".hidden"} {
		if _, err := ChooseID([]string{"--id", bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	long := "a" + string(bytes.Repeat([]byte("b"), 63))
	if _, err := ChooseID([]string{"--id", long}); err == nil {
		t.Fatal("accepted a long name")
	}
	if SubnetForID("claude").CIDR == SubnetForID("grok").CIDR {
		t.Fatal("claude and grok share a subnet")
	}
}

func TestCreateRejectsSizeFlagsOnWeakWall(t *testing.T) {
	code := Run([]string{"create", "--wall", "weak", "--image", "hermetarium/echo:dev", "--acl", "/tmp/acl.conf", "--mem", "2048"})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestCreateRejectsCPUsAboveWallLimit(t *testing.T) {
	host := runtime.NumCPU()
	cases := []struct {
		wall string
		cpus string
		want string
	}{
		{"strong", "33", "strong-wall limit of 32"},
		{"weak", strconv.Itoa(host + 1), fmt.Sprintf("this host's CPU count of %d", host)},
	}
	for _, tc := range cases {
		t.Run(tc.wall, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stderr
			os.Stderr = w
			code := Run([]string{
				"create", "--wall", tc.wall,
				"--image", "hermetarium/echo:dev",
				"--acl", "/tmp/acl.conf",
				"--cpus", tc.cpus,
			})
			w.Close()
			os.Stderr = old
			b, _ := io.ReadAll(r)
			if code != 2 {
				t.Fatalf("code=%d stderr=%q", code, b)
			}
			if !bytes.Contains(b, []byte(tc.want)) {
				t.Fatalf("stderr %q", b)
			}
		})
	}
	// Inclusive bounds. Checked on the function so a legal value does not start a wall.
	for _, ok := range []struct {
		wall string
		n    int
	}{
		{"strong", 0},
		{"strong", 1},
		{"strong", maxStrongCPUs},
		{"weak", 0},
		{"weak", 1},
		{"weak", host},
	} {
		if err := limitCPUs(ok.wall, ok.n); err != nil {
			t.Fatalf("%s %d: %v", ok.wall, ok.n, err)
		}
	}
}

func TestCreateRejectsRootfsOnWeakWall(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := Run([]string{"create", "--wall", "weak", "--image", "img", "--acl", "acl.conf", "--rootfs", "root.ext4"})
	w.Close()
	os.Stderr = old
	b, _ := io.ReadAll(r)
	if code != 2 {
		t.Fatalf("code=%d stderr=%q", code, b)
	}
	if !bytes.Contains(b, []byte("the Docker wall rejects --rootfs")) {
		t.Fatalf("stderr %q", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("created %v", entries)
	}
}

func TestExt4Regular(t *testing.T) {
	dir := t.TempDir()
	if err := ext4Regular(dir); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatalf("directory: %v", err)
	}
	missing := filepath.Join(dir, "missing")
	if err := ext4Regular(missing); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing: %v", err)
	}
	bad := filepath.Join(dir, "not-ext4")
	if err := os.WriteFile(bad, []byte("not a filesystem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ext4Regular(bad); err == nil || !strings.Contains(err.Error(), "not ext4") {
		t.Fatalf("non-ext4: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(bad, link); err != nil {
		t.Fatal(err)
	}
	if err := ext4Regular(link); err == nil || !strings.Contains(err.Error(), "not a file") {
		t.Fatalf("symlink: %v", err)
	}
	good := filepath.Join(dir, "ok.ext4")
	f, err := os.OpenFile(good, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0x53, 0xEF}, 0x438); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ext4Regular(good); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := ext4Regular(good); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("ext4 check changed the file")
	}
}

func TestCreateRejectsDataOnWeakWall(t *testing.T) {
	code := Run([]string{"create", "--wall", "weak", "--image", "hermetarium/echo:dev", "--acl", "/tmp/acl.conf", "--data", "/tmp/disk.ext4"})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestCreateRejectsBadDataFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cases := [][]string{
		{"create", "--wall", "strong", "--image", "img", "--acl", "acl.conf", "--data"},
		{"create", "--wall", "strong", "--image", "img", "--acl", "acl.conf", "--data", "--mem"},
		{"create", "--wall", "strong", "--image", "img", "--acl", "acl.conf", "--data", "-disk.ext4"},
		{"create", "--wall", "strong", "--image", "img", "--acl", "acl.conf", "--data", "disk:ext4"},
	}
	for _, args := range cases {
		if code := Run(args); code != 2 {
			t.Fatalf("%v code=%d", args, code)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("created %v", entries)
	}
}

func TestVersionCommand(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := Run([]string{"version"})
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	if code != 0 || !bytes.Contains(b, []byte(Version)) {
		t.Fatalf("code=%d out=%q", code, b)
	}
}
