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
