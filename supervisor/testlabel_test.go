package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewTestLabelFormat(t *testing.T) {
	a := NewTestLabel()
	b := NewTestLabel()
	if a == b {
		t.Fatalf("labels should differ: %s", a)
	}
	pid, ok := parseTestLabelPID(a)
	if !ok {
		t.Fatalf("parse %q", a)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}
	if !strings.HasPrefix(a, testLabelPrefix+"-") {
		t.Fatalf("prefix: %s", a)
	}
}

func TestDockerArgsWithTestLabel(t *testing.T) {
	args := DockerArgsWithTestLabel([]string{"run", "-d", "--name", "x", "img"}, "htm-1-abc")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--label "+TestLabelKey+"=htm-1-abc") {
		t.Fatalf("run args: %v", args)
	}
	if args[0] != "run" || args[1] != "--label" {
		t.Fatalf("label should follow run: %v", args)
	}
	create := DockerArgsWithTestLabel([]string{"create", "img"}, "htm-1-abc")
	if create[0] != "create" || create[1] != "--label" {
		t.Fatalf("create args: %v", create)
	}
	inspect := DockerArgsWithTestLabel([]string{"inspect", "x"}, "htm-1-abc")
	if strings.Join(inspect, " ") != "inspect x" {
		t.Fatalf("inspect should stay unlabeled: %v", inspect)
	}
	blank := DockerArgsWithTestLabel([]string{"run", "-d", "img"}, "")
	if strings.Join(blank, " ") != "run -d img" {
		t.Fatalf("blank label: %v", blank)
	}
	already := []string{"run", "--label", TestLabelKey + "=htm-1-abc", "img"}
	if got := DockerArgsWithTestLabel(already, "htm-1-abc"); strings.Join(got, " ") != strings.Join(already, " ") {
		t.Fatalf("duplicate: %v", got)
	}
}

func TestParseTestLabelPID(t *testing.T) {
	cases := []struct {
		in  string
		pid int
		ok  bool
	}{
		{"htm-123-abcdef", 123, true},
		{"htm-1-x", 1, true},
		{"other-123-abcdef", 0, false},
		{"htm-abc-def", 0, false},
		{"htm-0-abc", 0, false},
		{"htm--abc", 0, false},
		{"htm-123", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		pid, ok := parseTestLabelPID(tc.in)
		if pid != tc.pid || ok != tc.ok {
			t.Errorf("%q: pid=%d ok=%v, want pid=%d ok=%v", tc.in, pid, ok, tc.pid, tc.ok)
		}
	}
}

func TestPidRunning(t *testing.T) {
	if !pidRunning(os.Getpid()) {
		t.Fatal("this process should be running")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if pidRunning(cmd.Process.Pid) {
		t.Fatalf("exited pid %d still reported running", cmd.Process.Pid)
	}
}

func TestCleanupRemovesOnlyOwnLabel(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImageExists(AlpineImage); err != nil {
		t.Fatal(err)
	}
	a := NewTestLabel()
	b := NewTestLabel()
	nameA := "htm-label-a-" + strconv.Itoa(os.Getpid())
	nameB := "htm-label-b-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() {
		DockerIgnore("rm", "-fv", nameA, nameB)
	})
	if _, err := Docker(30*time.Second, "run", "-d", "--name", nameA, "--label", TestLabelKey+"="+a, "--entrypoint", "sleep", AlpineImage, "120"); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(30*time.Second, "run", "-d", "--name", nameB, "--label", TestLabelKey+"="+b, "--entrypoint", "sleep", AlpineImage, "120"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveContainersByTestLabel(a); err != nil {
		t.Fatal(err)
	}
	leftA, err := ContainersByTestLabel(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftA) != 0 {
		t.Fatalf("label A still present: %v", leftA)
	}
	leftB, err := ContainersByTestLabel(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftB) == 0 {
		t.Fatal("cleanup of A removed B's container")
	}
	if err := RemoveContainersByTestLabel(b); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDetectsLeftoverLabel(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImageExists(AlpineImage); err != nil {
		t.Fatal(err)
	}
	label := NewTestLabel()
	name := "htm-leftover-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { DockerIgnore("rm", "-fv", name) })
	if _, err := Docker(30*time.Second, "run", "-d", "--name", name, "--label", TestLabelKey+"="+label, "--entrypoint", "sleep", AlpineImage, "120"); err != nil {
		t.Fatal(err)
	}
	left, err := ContainersByTestLabel(label)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) == 0 {
		t.Fatal("expected the labeled container to be visible before cleanup")
	}
	if err := RemoveContainersByTestLabel(label); err != nil {
		t.Fatal(err)
	}
	left, err = ContainersByTestLabel(label)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("container still present after remove: %v", left)
	}
}

func TestSweepLeavesRunningPidContainers(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImageExists(AlpineImage); err != nil {
		t.Fatal(err)
	}
	live := fmt.Sprintf("%s-%d-%s", testLabelPrefix, os.Getpid(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := fmt.Sprintf("%s-%d-%s", testLabelPrefix, cmd.Process.Pid, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	liveName := "htm-sweep-live-" + strconv.Itoa(os.Getpid())
	deadName := "htm-sweep-dead-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { DockerIgnore("rm", "-fv", liveName, deadName) })
	if _, err := Docker(30*time.Second, "run", "-d", "--name", liveName, "--label", TestLabelKey+"="+live, "--entrypoint", "sleep", AlpineImage, "120"); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(30*time.Second, "run", "-d", "--name", deadName, "--label", TestLabelKey+"="+dead, "--entrypoint", "sleep", AlpineImage, "120"); err != nil {
		t.Fatal(err)
	}
	sweepOrphanTestContainers()
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", liveName); err != nil {
		t.Fatalf("sweep removed a container whose label pid is still running: %v", err)
	}
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", deadName); err == nil {
		t.Fatal("sweep left a container whose label pid is not running")
	}
}

func TestCreateDoesNotSetTestLabel(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
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
	image := "hermetarium-label-create:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\nCMD [\"sleep\", \"3600\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DestroyWeak(id) })
	if _, err := CreateWeak(root, id, CreateOpts{Image: image, ACL: acl, NoInbound: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"htm-gate-" + id, "htm-box-" + id, "htm-dns-" + id} {
		got, err := Docker(15*time.Second, "inspect", "-f", `{{index .Config.Labels "`+TestLabelKey+`"}}`, name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(got) != "" {
			t.Errorf("%s has %s=%q, want empty", name, TestLabelKey, strings.TrimSpace(got))
		}
	}
}

func TestRootfsExportRemovesImageVolumes(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-rootfs-vol:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\nVOLUME /data\nCMD [\"sleep\", \"1\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DockerIgnore("rmi", "-f", image) })

	cid, err := Docker(30*time.Second, "create", image)
	if err != nil {
		t.Fatal(err)
	}
	cid = strings.TrimSpace(cid)
	vols := volumeNames(t, cid)
	if len(vols) == 0 {
		t.Fatal("image VOLUME did not create a volume on docker create")
	}
	DockerIgnore("rm", "-fv", cid)
	for _, vol := range vols {
		if _, err := Docker(15*time.Second, "volume", "inspect", vol); err == nil {
			t.Errorf("docker rm -fv left volume %s", vol)
		}
	}

	dest, err := EnsureImageRootfs(root, image, 280)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dest) })
	left, err := Docker(15*time.Second, "ps", "-aq", "--filter", "ancestor="+image)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(left) != "" {
		t.Fatalf("rootfs export left containers: %s", left)
	}
}
