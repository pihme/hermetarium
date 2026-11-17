package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDestroyRemovesLegacyContainers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERMETARIUM_ROOT", root)
	id := "oldhab"
	dir := filepath.Join(root, "var", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootfs := filepath.Join(dir, "rootfs.ext4")
	if err := os.WriteFile(rootfs, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{
		ID: id, Wall: "strong", Dir: dir,
		FirecrackerContainer: "htm-fc-" + id,
		Gate:                 "htm-gate-" + id,
	}
	if err := writeInstance(inst); err != nil {
		t.Fatal(err)
	}

	var rms []string
	alive := map[string]bool{"htm-fc-" + id: true}
	stubDocker(t, func(_ time.Duration, args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("empty docker args")
		}
		switch args[0] {
		case "rm":
			rms = append(rms, strings.Join(args, " "))
			for _, name := range args[2:] {
				delete(alive, name)
			}
			return "", nil
		case "inspect":
			name := args[len(args)-1]
			if alive[name] {
				return "id\n", nil
			}
			return "", fmt.Errorf("Error: No such object: %s", name)
		default:
			return "", nil
		}
	})

	if err := refuseLiveFirecracker(root, id); err == nil {
		t.Fatal("create would replace a rootfs that htm-fc-oldhab still has open")
	}
	dropUnmountedRootfs(id, dir)
	if _, err := os.Stat(rootfs); err != nil {
		t.Fatal("rootfs deleted while htm-fc-oldhab was running")
	}

	if err := Destroy(root, id); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rms, "\n")
	for _, name := range []string{
		"htm-" + id + "-fc", "htm-fc-" + id,
		"htm-" + id + "-gate", "htm-gate-" + id,
		"htm-" + id + "-dns", "htm-dns-" + id,
	} {
		if !strings.Contains(joined, name) {
			t.Fatalf("destroy did not remove %s\n%s", name, joined)
		}
	}
	if _, err := os.Stat(rootfs); !os.IsNotExist(err) {
		t.Fatal("rootfs still present after the old Firecracker container was removed")
	}
}

func TestDestroyRemovesLegacyWeakContainers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERMETARIUM_ROOT", root)
	id := "oldbox"
	dir := filepath.Join(root, "var", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{
		ID: id, Wall: "weak", Dir: dir,
		Box:  "htm-box-" + id,
		Gate: "htm-gate-" + id,
	}
	if err := writeInstance(inst); err != nil {
		t.Fatal(err)
	}
	var rms []string
	stubDocker(t, func(_ time.Duration, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "rm" {
			rms = append(rms, strings.Join(args, " "))
		}
		if len(args) > 0 && args[0] == "inspect" {
			return "", fmt.Errorf("Error: No such object: %s", args[len(args)-1])
		}
		return "", nil
	})
	if err := Destroy(root, id); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rms, "\n")
	for _, name := range []string{
		"htm-" + id + "-box", "htm-box-" + id,
		"htm-" + id + "-gate", "htm-gate-" + id,
		"htm-" + id + "-dns", "htm-dns-" + id,
	} {
		if !strings.Contains(joined, name) {
			t.Fatalf("destroy did not remove %s\n%s", name, joined)
		}
	}
}
