package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitGuestUname(t *testing.T) {
	t.Run("serial", func(t *testing.T) {
		dir := t.TempDir()
		serial := filepath.Join(dir, "serial.log")
		if err := os.WriteFile(serial, []byte("GUEST_UNAME=6.1.188\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := waitGuestUname(dir, time.Second, func() (string, bool) {
			b, err := os.ReadFile(serial)
			if err != nil {
				t.Fatal(err)
			}
			return string(b), true
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dead", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "serial.log"), []byte("still booting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		const reason = "rejected machine config vcpu_count"
		if err := os.WriteFile(filepath.Join(dir, "fc.log"), []byte(reason+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err := waitGuestUname(dir, time.Minute, func() (string, bool) {
			b, readErr := os.ReadFile(filepath.Join(dir, "serial.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			return string(b), false
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("dead container waited %s", time.Since(start))
		}
		if !strings.Contains(err.Error(), reason) {
			t.Fatal(err)
		}
	})

	t.Run("late uname", func(t *testing.T) {
		n := 0
		err := waitGuestUname(t.TempDir(), time.Minute, func() (string, bool) {
			n++
			if n == 1 {
				return "", false
			}
			return "GUEST_UNAME=6.1.188", false
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestFcScriptsDirExtractsWithoutTree(t *testing.T) {
	root := t.TempDir()
	dir, err := fcScriptsDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"entrypoint.sh", "pack-oci.sh", "gate-cert.sh"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() == 0 || st.Mode()&0o111 == 0 {
			t.Fatalf("%s: size=%d mode=%v", p, st.Size(), st.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "firecracker-scripts")); !os.IsNotExist(err) {
		t.Fatalf("extract wrote into tree path: %v", err)
	}
}
