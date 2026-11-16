package guestdocker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const guestDockerImage = "hermetarium-guest-docker:local"

func TestGuestDocker(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := buildGuestDocker(root); err != nil {
		t.Fatal(err)
	}
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("strong wall docker: create %s", id)
	inst, err := supervisor.CreateStrong(root, id, supervisor.WithTestLabel(t, supervisor.CreateOpts{
		Image:  guestDockerImage,
		ACL:    filepath.Join(root, "examples", "echo", "squid.conf"),
		MemMiB: 2048,
		DiskMB: 2048,
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyStrong(id)

	deadline := time.Now().Add(3 * time.Minute)
	var serial string
	for time.Now().Before(deadline) {
		serial, _ = supervisor.Docker(10*time.Second, "logs", inst.Helper)
		if strings.Contains(serial, "DOCKER_INNER_OK") && strings.Contains(serial, "hermetarium-docker-ok") {
			t.Log("strong wall: docker ran a container")
			return
		}
		for _, marker := range []string{"DOCKERD_FAIL", "DOCKER_LOAD_FAIL", "DOCKER_RUN_FAIL"} {
			if strings.Contains(serial, marker) {
				t.Fatalf("%s\n%s", marker, tail(serial, 6000))
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timeout waiting for inner container\n%s", tail(serial, 6000))
}

func buildGuestDocker(root string) error {
	if _, err := supervisor.Docker(3*time.Minute, "pull", "busybox:1.37"); err != nil {
		return err
	}
	if _, err := supervisor.Docker(5*time.Minute, "pull", "docker:29-dind"); err != nil {
		return err
	}
	tar := filepath.Join(supervisor.CacheDir(root), "busybox-1.37.tar")
	if st, err := os.Stat(tar); err != nil || st.Size() < 1000 {
		if err := os.MkdirAll(filepath.Dir(tar), 0o755); err != nil {
			return err
		}
		if _, err := supervisor.Docker(2*time.Minute, "save", "-o", tar, "busybox:1.37"); err != nil {
			return err
		}
	}
	ctx, err := os.MkdirTemp("", "htm-guest-docker-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(ctx)
	src := filepath.Join(root, "tests", "guest-docker")
	for _, name := range []string{"Dockerfile", "start.sh"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(ctx, name), b, 0o755); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(tar)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ctx, "busybox.tar"), b, 0o644); err != nil {
		return err
	}
	_, err = supervisor.Docker(5*time.Minute, "build", "-t", guestDockerImage, ctx)
	return err
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
