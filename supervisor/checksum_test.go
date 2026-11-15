package supervisor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestChecksumMismatchIsRefused(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "blob")
	body := []byte("not-the-kernel")
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err := requireSHA256(dest, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	err := requireSHA256(dest, kernelSHA256)
	if err == nil {
		t.Fatal("wrong checksum was accepted")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("rejected download was kept")
	}
}

func TestFirecrackerStamp(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "firecracker")
	stamp := filepath.Join(dir, "firecracker.stamp")
	body := []byte("fc-binary")
	if err := os.WriteFile(bin, body, 0o755); err != nil {
		t.Fatal(err)
	}

	ok, err := fcBinaryCurrent(bin, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing stamp was a cache hit")
	}
	if err := os.WriteFile(stamp, []byte(fcSHA256+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err = fcBinaryCurrent(bin, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stamp without the binary hash was a cache hit")
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if err := writeFCStamp(bin, stamp); err == nil {
		t.Fatal("stamp written for a missing binary")
	}
	if err := os.WriteFile(bin, body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFCStamp(bin, stamp); err != nil {
		t.Fatal(err)
	}
	ok, err = fcBinaryCurrent(bin, stamp)
	if err != nil || !ok {
		t.Fatalf("fresh stamp: ok=%v err=%v", ok, err)
	}

	if err := os.WriteFile(bin, []byte("swapped"), 0o755); err != nil {
		t.Fatal(err)
	}
	ok, err = fcBinaryCurrent(bin, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("swapped binary was a cache hit")
	}

	tgz := filepath.Join(dir, "firecracker.tgz")
	tarball := []byte("tarball-bytes")
	if err := os.WriteFile(tgz, tarball, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(tarball)
	discardFCStampIfTarballRejected(tgz, stamp, hex.EncodeToString(sum[:]))
	if _, err := os.Stat(stamp); err != nil {
		t.Fatal("matching tarball dropped the stamp")
	}
	if err := os.WriteFile(tgz, []byte("not-the-tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	discardFCStampIfTarballRejected(tgz, stamp, hex.EncodeToString(sum[:]))
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Fatal("rejected tarball kept the stamp")
	}
	if _, err := os.Stat(tgz); !os.IsNotExist(err) {
		t.Fatal("rejected tarball was kept")
	}
}

func TestEnsureStrongAssetsRejectsTarballWithoutBinary(t *testing.T) {
	pin := fcPin
	t.Cleanup(func() { fcPin = pin })

	root := t.TempDir()
	cache := CacheDir(root)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	tgzPath := filepath.Join(cache, "firecracker-"+fcVersion+"-x86_64.tgz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{
		{"release-v1.16.1-x86_64/LICENSE", "license"},
		{"release-v1.16.1-x86_64/firecracker-v1.16.1-x86_64.debug", "debug-only"},
	} {
		b := []byte(f.body)
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(b))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if err := os.WriteFile(tgzPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	fcPin = hex.EncodeToString(sum[:])

	bin := filepath.Join(cache, "firecracker")
	if err := os.WriteFile(bin, []byte("stale-firecracker"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(cache, fcStampName)

	_, _, err := EnsureStrongAssets(root)
	if err == nil {
		t.Fatal("tarball without firecracker-*x86_64 was accepted")
	}
	if _, statErr := os.Stat(stamp); !os.IsNotExist(statErr) {
		t.Fatalf("stamp written after failed extract: %v", statErr)
	}
	if _, statErr := os.Stat(bin); !os.IsNotExist(statErr) {
		t.Fatalf("stale binary still present: %v", statErr)
	}
	got, err := sha256File(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != fcPin {
		t.Fatal("fixture tarball was replaced")
	}
}
