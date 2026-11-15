package supervisor

import (
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
