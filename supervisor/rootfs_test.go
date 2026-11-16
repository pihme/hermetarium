package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHabitatRootfsIsPrivate(t *testing.T) {
	root := t.TempDir()
	cache, err := cacheFile(root, "rootfs-busybox-1024.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSparseExt4(cache); err != nil {
		t.Fatal(err)
	}
	cacheSum, err := sha256File(cache)
	if err != nil {
		t.Fatal(err)
	}
	srcAlloc, err := allocatedBytes(cache)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if srcAlloc*2 > st.Size() {
		t.Fatalf("fixture is not sparse: allocated %d size %d", srcAlloc, st.Size())
	}

	// habitatRootfs writes var/<id>/rootfs.ext4. CreateStrong bind-mounts
	// that path, not the cache. Two creates, then a third, must not share
	// the cache path or its bytes.
	first, err := makeHabitatRootfs(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	second, err := makeHabitatRootfs(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == cache || second == cache {
		t.Fatalf("shared rootfs first=%s second=%s cache=%s", first, second, cache)
	}
	if sameInode(t, first, cache) || sameInode(t, second, cache) || sameInode(t, first, second) {
		t.Fatalf("rootfs files share an inode")
	}
	assertHelperBind(t, root, cache, first)
	assertHelperBind(t, root, cache, second)
	for _, path := range []string{first, second} {
		sum, err := sha256File(path)
		if err != nil {
			t.Fatal(err)
		}
		if sum != cacheSum {
			t.Fatalf("copy %s is not the cache", path)
		}
		alloc, err := allocatedBytes(path)
		if err != nil {
			t.Fatal(err)
		}
		if alloc > srcAlloc+16*1024 {
			t.Fatalf("copy allocated %d, cache allocated %d", alloc, srcAlloc)
		}
	}

	f, err := os.OpenFile(first, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("DIRTY"), 0); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := sha256File(cache); err != nil || got != cacheSum {
		t.Fatalf("cache changed: %v %s", err, got)
	}
	if got, err := sha256File(second); err != nil || got != cacheSum {
		t.Fatalf("other habitat changed: %v %s", err, got)
	}

	third, err := makeHabitatRootfs(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	if third == first || third == second || third == cache {
		t.Fatalf("later habitat shares a rootfs %s", third)
	}
	if got, err := sha256File(third); err != nil || got != cacheSum {
		t.Fatalf("later habitat saw the write: %v %s", err, got)
	}
	if got, err := sha256File(cache); err != nil || got != cacheSum {
		t.Fatalf("cache changed after later create: %v %s", err, got)
	}
	assertHelperBind(t, root, cache, third)
}

func assertHelperBind(t *testing.T, root, cache, copied string) {
	t.Helper()
	dir := filepath.Dir(copied)
	if copied != habitatRootfsPath(dir) || filepath.Dir(dir) != VarDir(root) {
		t.Fatalf("helper bind source %s is not var/<id>/rootfs.ext4", copied)
	}
	vol := strongRootfsVolume(dir)
	if vol != copied+":/opt/rootfs.ext4" {
		t.Fatalf("helper bind %s, copy %s", vol, copied)
	}
	if copied == cache || vol == cache+":/opt/rootfs.ext4" {
		t.Fatalf("helper bind is the cache %s", vol)
	}
}

func TestHabitatRootfsReplacesSymlink(t *testing.T) {
	root := t.TempDir()
	cache, err := cacheFile(root, "rootfs-busybox-1024.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSparseExt4(cache); err != nil {
		t.Fatal(err)
	}
	cacheSum, err := sha256File(cache)
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	dest := habitatRootfsPath(dir)
	if err := os.Symlink(cache, dest); err != nil {
		t.Fatal(err)
	}
	got, err := habitatRootfs(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("path %s", got)
	}
	st, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		t.Fatalf("rootfs mode %v", st.Mode())
	}
	if sum, err := sha256File(cache); err != nil || sum != cacheSum {
		t.Fatalf("cache changed: %v %s", err, sum)
	}
	if sum, err := sha256File(dest); err != nil || sum != cacheSum {
		t.Fatalf("copy: %v %s", err, sum)
	}
	if _, err := os.Lstat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial left: %v", err)
	}
	assertHelperBind(t, root, cache, got)
}

func TestHabitatRootfsFailureKeepsDest(t *testing.T) {
	root := t.TempDir()
	cache, err := cacheFile(root, "rootfs-busybox-1024.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSparseExt4(cache); err != nil {
		t.Fatal(err)
	}
	cacheSum, err := sha256File(cache)
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	dest := habitatRootfsPath(dir)
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dest, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := habitatRootfs(dir, cache); err == nil {
		t.Fatal("rename onto a directory succeeded")
	}
	if _, err := os.Lstat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial left: %v", err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
		t.Fatalf("destination changed: %v %q", err, b)
	}
	if sum, err := sha256File(cache); err != nil || sum != cacheSum {
		t.Fatalf("cache changed: %v %s", err, sum)
	}

	// A partial symlink must not be opened. O_EXCL fails and the cache stays.
	partial := dest + ".partial"
	if err := os.Symlink(cache, partial); err != nil {
		t.Fatal(err)
	}
	if _, err := habitatRootfs(dir, cache); err == nil {
		t.Fatal("copy followed the partial symlink")
	}
	st, err := os.Lstat(partial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("partial mode %v", st.Mode())
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
		t.Fatalf("destination changed: %v %q", err, b)
	}
	if sum, err := sha256File(cache); err != nil || sum != cacheSum {
		t.Fatalf("cache changed: %v %s", err, sum)
	}
}

func TestHabitatRootfsStalePartial(t *testing.T) {
	root := t.TempDir()
	cache, err := cacheFile(root, "rootfs-busybox-1024.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSparseExt4(cache); err != nil {
		t.Fatal(err)
	}
	cacheSum, err := sha256File(cache)
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	partial := habitatRootfsPath(dir) + ".partial"
	if err := os.WriteFile(partial, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = habitatRootfs(dir, cache)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "run hermetarium destroy") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(err.Error(), partial) != 1 {
		t.Fatalf("path twice: %v", err)
	}
	if sum, err := sha256File(cache); err != nil || sum != cacheSum {
		t.Fatalf("cache changed: %v %s", err, sum)
	}
	if b, err := os.ReadFile(partial); err != nil || string(b) != "stale" {
		t.Fatalf("partial changed: %v %q", err, b)
	}
}

func TestRemovePrivateRootfs(t *testing.T) {
	root := t.TempDir()
	id := "abc"
	dir := filepath.Join(VarDir(root), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	priv := habitatRootfsPath(dir)
	if err := os.WriteFile(priv, []byte("priv"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "disk.ext4")
	if err := os.WriteFile(data, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	partial := priv + ".partial"
	if err := os.WriteFile(partial, []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sized := filepath.Join(cacheDir, "rootfs-img-256.ext4")
	orphan := filepath.Join(cacheDir, "rootfs-"+id+".ext4")
	if err := os.WriteFile(sized, []byte("sized"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(VarDir(root), "link")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sized, habitatRootfsPath(linkDir)); err != nil {
		t.Fatal(err)
	}
	partialLink := habitatRootfsPath(linkDir) + ".partial"
	if err := os.Symlink(sized, partialLink); err != nil {
		t.Fatal(err)
	}

	removePrivateRootfs(dir)
	removePrivateRootfs(linkDir)

	if _, err := os.Lstat(priv); !os.IsNotExist(err) {
		t.Fatalf("private rootfs: %v", err)
	}
	if _, err := os.Lstat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial rootfs: %v", err)
	}
	if _, err := os.Lstat(habitatRootfsPath(linkDir)); !os.IsNotExist(err) {
		t.Fatalf("symlink rootfs: %v", err)
	}
	if _, err := os.Lstat(partialLink); !os.IsNotExist(err) {
		t.Fatalf("symlink partial: %v", err)
	}
	for _, pair := range []struct{ path, body string }{
		{data, "data"},
		{sized, "sized"},
		{orphan, "orphan"},
	} {
		b, err := os.ReadFile(pair.path)
		if err != nil || string(b) != pair.body {
			t.Fatalf("%s: %v %q", pair.path, err, b)
		}
	}
}

func TestDropUnmountedRootfs(t *testing.T) {
	if _, err := Docker(5*time.Second, "info"); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	absent, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	kept := plantRootfsFixture(t, root, absent)
	dropUnmountedRootfs(absent, filepath.Dir(kept.priv))
	if _, err := os.Lstat(kept.priv); !os.IsNotExist(err) {
		t.Fatalf("private rootfs after failed create: %v", err)
	}
	if _, err := os.Lstat(kept.partial); !os.IsNotExist(err) {
		t.Fatalf("partial rootfs after failed create: %v", err)
	}
	kept.unchanged(t)

	present, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	live := plantRootfsFixture(t, root, present)
	name := "htm-fc-" + present
	if _, err := DockerLabeled(t, 30*time.Second, "create", "--name", name, AlpineImage, "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DockerIgnore("rm", "-f", name) })
	dropUnmountedRootfs(present, filepath.Dir(live.priv))
	if b, err := os.ReadFile(live.priv); err != nil || string(b) != "priv" {
		t.Fatalf("private rootfs while helper exists: %v %q", err, b)
	}
	if b, err := os.ReadFile(live.partial); err != nil || string(b) != "part" {
		t.Fatalf("partial rootfs while helper exists: %v %q", err, b)
	}
	live.unchanged(t)
}

type rootfsFixture struct {
	priv    string
	partial string
	data    string
	sized   string
	orphan  string
}

func plantRootfsFixture(t *testing.T, root, id string) rootfsFixture {
	t.Helper()
	dir := filepath.Join(VarDir(root), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fx := rootfsFixture{
		priv:    habitatRootfsPath(dir),
		partial: habitatRootfsPath(dir) + ".partial",
		data:    filepath.Join(dir, "disk.ext4"),
		sized:   filepath.Join(cacheDir, "rootfs-img-256.ext4"),
		orphan:  filepath.Join(cacheDir, "rootfs-"+id+".ext4"),
	}
	for _, pair := range []struct{ path, body string }{
		{fx.priv, "priv"},
		{fx.partial, "part"},
		{fx.data, "data"},
		{fx.sized, "sized"},
		{fx.orphan, "orphan"},
	} {
		if err := os.WriteFile(pair.path, []byte(pair.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fx
}

func (fx rootfsFixture) unchanged(t *testing.T) {
	t.Helper()
	for _, pair := range []struct{ path, body string }{
		{fx.data, "data"},
		{fx.sized, "sized"},
		{fx.orphan, "orphan"},
	} {
		b, err := os.ReadFile(pair.path)
		if err != nil || string(b) != pair.body {
			t.Fatalf("%s: %v %q", pair.path, err, b)
		}
	}
}

func makeHabitatRootfs(root, cache string) (string, error) {
	id, err := NewID()
	if err != nil {
		return "", err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return "", err
	}
	return habitatRootfs(dir, cache)
}

func writeSparseExt4(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	const size = 8 << 20
	if err := f.Truncate(size); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte("CACHE-HEAD"), 0); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte("CACHE-MID"), 4<<20); err != nil {
		return err
	}
	_, err = f.WriteAt([]byte("CACHE-TAIL"), size-16)
	return err
}

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	var sa, sb syscall.Stat_t
	if err := syscall.Stat(a, &sa); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Stat(b, &sb); err != nil {
		t.Fatal(err)
	}
	return sa.Dev == sb.Dev && sa.Ino == sb.Ino
}
