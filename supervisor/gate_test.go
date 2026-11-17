package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	fcscripts "github.com/pihme/hermetarium/firecracker-scripts"
)

func TestGateRulesScriptOneList(t *testing.T) {
	script := gateRulesScript()
	for _, p := range gateAllow() {
		line := fmt.Sprintf("-p %s --dport %d -j ACCEPT", p.Proto, p.Port)
		if strings.Count(script, line) != 1 {
			t.Fatalf("allow %s appears %d times\n%s", line, strings.Count(script, line), script)
		}
	}
	if strings.Contains(script, "172.16.") || strings.Contains(script, "https_port") || strings.Contains(script, "ssl-bump") {
		t.Fatal("gate rules hardcode an address or a second HTTPS check")
	}
	if !strings.Contains(script, `if [ "${GATE_HTTPS:-}" = 1 ]`) {
		t.Fatal("HTTPS redirect is not gated on the one check's result")
	}
	dir := t.TempDir()
	if err := fcscripts.Extract(dir); err != nil {
		t.Fatal(err)
	}
	ep, err := os.ReadFile(filepath.Join(dir, "entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(ep)
	if !strings.Contains(text, "sh /log/gate-rules.sh") {
		t.Fatal("strong wall does not run the shared gate rules")
	}
	if strings.Contains(text, "--dport") || strings.Contains(text, "https_port") || strings.Contains(text, "ssl-bump") {
		t.Fatal("entrypoint carries its own allow-list or HTTPS check")
	}
	if strings.Contains(script, "/usr/sbin/httpd") {
		t.Fatal("allow-list carries the certificate server")
	}
	https := strings.Index(text, `if [ "${GATE_HTTPS:-}" = 1 ]`)
	ready := strings.Index(text, ": > /log/ca.ready")
	dns := strings.Index(text, "test -f /log/dns.ready")
	if https < 0 || ready < https || dns < ready {
		t.Fatal("certificate server must accept before the guest is released")
	}
}

func TestGateRulesMatchAcrossWalls(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureNetTools(root); err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-sleep:local"
	ctx := t.TempDir()
	df := "FROM " + BusyboxImage + "\nCMD [\"sleep\", \"3600\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(3*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DockerIgnore("rmi", image) })
	acl := filepath.Join(root, "examples", "echo", "squid.conf")
	opts := CreateOpts{Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 256}
	var got []string
	for _, wall := range []string{"weak", "strong"} {
		id := newTestID(t)
		var netns string
		switch wall {
		case "weak":
			t.Cleanup(func() { _ = DestroyWeak(id) })
			inst, err := CreateWeak(root, id, WithTestLabel(t, opts))
			if err != nil {
				t.Fatal(err)
			}
			netns = inst.Gate
		default:
			t.Cleanup(func() { _ = DestroyStrong(id); endStrong(id, filepath.Join(VarDir(root), id)) })
			inst, err := CreateStrong(root, id, WithTestLabel(t, opts))
			if err != nil {
				t.Fatal(err)
			}
			netns = inst.FirecrackerContainer
		}
		got = append(got, appliedGateRules(t, netns))
	}
	if got[0] != got[1] {
		t.Fatalf("wall rules diverged\nweak:\n%s\nstrong:\n%s", got[0], got[1])
	}
	for _, p := range gateAllow() {
		line := fmt.Sprintf("-p %s -m %s --dport %d", p.Proto, p.Proto, p.Port)
		if !strings.Contains(got[0], line) && !strings.Contains(got[0], fmt.Sprintf("-p %s --dport %d", p.Proto, p.Port)) {
			t.Fatalf("applied rules missing %s\n%s", line, got[0])
		}
	}
}

func appliedGateRules(t *testing.T, netns string) string {
	t.Helper()
	out, err := Docker(20*time.Second, "run", "--rm",
		"--network", "container:"+netns,
		"--cap-add", "NET_ADMIN",
		"--entrypoint", "sh",
		NetToolsImage,
		"-c", "iptables -S; echo ---; iptables -t nat -S",
	)
	if err != nil {
		t.Fatal(err)
	}
	iface := regexp.MustCompile(`-i [^ ]+`)
	cidr := regexp.MustCompile(`-s [^ ]+`)
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			lines = append(lines, line)
			continue
		}
		// Docker injects embedded-DNS nat chains on a bridge netns. The
		// Firecracker container never joins that bridge, so those lines are
		// not the allow-list.
		if strings.Contains(line, "DOCKER_OUTPUT") || strings.Contains(line, "DOCKER_POSTROUTING") || strings.Contains(line, "127.0.0.11") {
			continue
		}
		line = iface.ReplaceAllStringFunc(line, func(s string) string {
			if strings.HasSuffix(s, " lo") {
				return s
			}
			return "-i HAB"
		})
		line = cidr.ReplaceAllString(line, "-s CIDR")
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestGateCAAcceptsBeforeDNS(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureNetTools(root); err != nil {
		t.Fatal(err)
	}
	scripts, err := fcScriptsDir(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pub := filepath.Join(dir, "mitm-pub")
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pub, "ca.pem"), []byte("test-ca\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGateRules(dir); err != nil {
		t.Fatal(err)
	}
	plan := StrongPlan()
	id := newTestID(t)
	name := ContainerName(id, "fc")
	t.Cleanup(func() { DockerIgnore("rm", "-f", name) })
	args := []string{
		"run", "-d", "--name", name,
		"--device", "/dev/kvm",
		"--device", "/dev/net/tun",
		"--cap-add", "NET_ADMIN",
		"--sysctl", "net.ipv4.ip_forward=1",
		"--sysctl", "net.ipv4.conf.all.rp_filter=0",
		"--sysctl", "net.ipv4.conf.default.rp_filter=0",
		"--entrypoint", "sh",
		"-e", "GATE_IP=" + plan.Gate,
		"-e", "GUEST_IP=" + plan.Guest,
		"-e", "GATE_CIDR=" + plan.CIDR,
		"-e", "GATE_PREFIX=" + strconv.Itoa(plan.Prefix),
		"-e", "GATE_HTTPS=1",
		"-e", "GATE_CA_PORT=" + strconv.Itoa(GateCAPort),
		"-e", "NO_INBOUND=1",
		"-v", filepath.Join(scripts, "entrypoint.sh") + ":/entrypoint.sh:ro",
		"-v", dir + ":/log",
	}
	args = append(args, TestLabelArgs(TrackTestContainers(t))...)
	args = append(args, NetToolsImage, "/entrypoint.sh")
	if _, err := Docker(30*time.Second, args...); err != nil {
		t.Fatal(err)
	}
	if err := waitGateCA(dir, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dns.ready")); err == nil {
		t.Fatal("dns.ready was written before the guest was released")
	}
	body, err := Docker(10*time.Second, "exec", name, "wget", "-q", "-O", "-", "http://"+plan.Gate+":"+strconv.Itoa(GateCAPort)+"/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	if body != "test-ca\n" {
		t.Fatalf("served certificate %q", body)
	}
	out, err := Docker(15*time.Second, "ps", "-aq", "--filter", "name=htm-ca-"+id, "--format", "{{.Names}}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("certificate container still exists: %s", out)
	}
}
