package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDestroyRemovesGateVolumes(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), []byte("http_access deny all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, destroy := range []func(string) error{DestroyWeak, DestroyStrong} {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		name := "htm-gate-" + id
		t.Cleanup(func() { _ = destroy(id) })
		if err := runSquid(root, name, dir, nil); err != nil {
			t.Fatal(err)
		}
		vols := gateVolumeNames(t, name)
		if len(vols) == 0 {
			t.Fatal("gate container has no anonymous volumes")
		}
		if err := destroy(id); err != nil {
			t.Fatal(err)
		}
		for _, vol := range vols {
			if _, err := Docker(15*time.Second, "volume", "inspect", vol); err == nil {
				t.Errorf("%s left volume %s", name, vol)
			}
		}
	}
}

func gateVolumeNames(t *testing.T, container string) []string {
	t.Helper()
	out, err := Docker(15*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}`, container)
	if err != nil {
		t.Fatal(err)
	}
	var vols []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			vols = append(vols, line)
		}
	}
	return vols
}
