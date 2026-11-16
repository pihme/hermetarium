package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateHasNoAnonymousVolumes(t *testing.T) {
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
		if err := runSquid(root, name, dir, TrackTestContainers(t), nil); err != nil {
			t.Fatal(err)
		}
		if vols := volumeNames(t, name); len(vols) != 0 {
			t.Fatalf("gate has anonymous volumes %v, want none (tmpfs should cover the image VOLUME paths)", vols)
		}
		tmpfs, err := Docker(15*time.Second, "inspect", "-f", "{{json .HostConfig.Tmpfs}}", name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(tmpfs, "/var/log/squid") || !strings.Contains(tmpfs, "/var/spool/squid") {
			t.Fatalf("gate HostConfig.Tmpfs %s, want /var/log/squid and /var/spool/squid", tmpfs)
		}
		if err := destroy(id); err != nil {
			t.Fatal(err)
		}
		if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", name); err == nil {
			t.Errorf("gate container %s still present after destroy", name)
		}
	}
}

func TestDestroyRemovesBoxVolumes(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-volume:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\nVOLUME /data\nCMD [\"sleep\", \"3600\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	acl := filepath.Join(t.TempDir(), "squid.conf")
	conf := "" +
		"http_port 127.0.0.1:3129\n" +
		"http_port 0.0.0.0:3128 intercept\n" +
		"visible_hostname hermetarium\n" +
		"pid_filename /log/squid.pid\n" +
		"coredump_dir /tmp\n" +
		"cache_log /log/cache.log\n" +
		"logformat htm %ts.%03tu %6tr %>a %Ss/%03>Hs %<st %rm %ru %[un %Sh/%<a %mt %>lp\n" +
		"access_log stdio:/log/access.log htm\n" +
		"logfile_rotate 0\n" +
		"shutdown_lifetime 1 seconds\n" +
		"cache_effective_user proxy\n" +
		"dns_v4_first on\n" +
		"cache deny all\n" +
		"http_access deny all\n"
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DestroyWeak(id) })
	if _, err := CreateWeak(root, id, WithTestLabel(t, CreateOpts{Image: image, ACL: acl, NoInbound: true})); err != nil {
		t.Fatal(err)
	}
	box := "htm-box-" + id
	vols := volumeNames(t, box)
	if len(vols) == 0 {
		t.Fatal("box container has no anonymous volumes")
	}
	mounts, err := Docker(15*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Type "volume"}}{{.Destination}}{{"\n"}}{{end}}{{end}}`, box)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mounts, "/data") {
		t.Fatalf("box volume mounts %q, want /data", mounts)
	}
	if err := DestroyWeak(id); err != nil {
		t.Fatal(err)
	}
	for _, vol := range vols {
		if _, err := Docker(15*time.Second, "volume", "inspect", vol); err == nil {
			t.Errorf("box left volume %s", vol)
		}
	}
}

func volumeNames(t *testing.T, container string) []string {
	t.Helper()
	out, err := Docker(15*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}`, container)
	if err != nil {
		t.Fatal(err)
	}
	return splitNonEmpty(out)
}
