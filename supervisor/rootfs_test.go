package supervisor

import (
	"os"
	"syscall"
	"testing"
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

	// habitatRootfs is what CreateStrong mounts. Two creates, then a third,
	// must not share the cache path or its bytes.
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
