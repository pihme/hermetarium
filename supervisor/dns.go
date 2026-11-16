package supervisor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"
)

//go:embed gatedns.bin
var gateDNSBin []byte

// ACLOpensWeb reports whether the gate file allows traffic past the
// stand-in and inbound names. That is when outside names resolve.
func ACLOpensWeb(conf []byte) bool {
	closed := map[string]bool{
		"inbound": true, "vendorhost": true, "probe": true, "probe_dst": true,
	}
	for _, line := range strings.Split(string(conf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "http_access" && f[1] == "allow" && !closed[f[2]] {
			return true
		}
	}
	return false
}

func startGateDNS(root, id, netns, gateContainer, gateIP, label string, conf []byte) error {
	bin, err := ensureGateDNSBin(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(VarDir(root), id)
	up, err := gateResolvers(gateContainer)
	if err != nil {
		return err
	}
	// Squid resolves from the gate container's resolv.conf. Intercept mode
	// checks the name against the address the box connected to, so the
	// forwarder uses those same servers.
	var b strings.Builder
	if ACLOpensWeb(conf) {
		b.WriteString("open 1\n")
	} else {
		b.WriteString("open 0\n")
	}
	fmt.Fprintf(&b, "gate %s\n", gateIP)
	for _, h := range StandInNames() {
		fmt.Fprintf(&b, "name %s\n", h)
	}
	for _, s := range up {
		fmt.Fprintf(&b, "up %s\n", s)
	}
	if err := os.WriteFile(filepath.Join(dir, "dns.conf"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	name := "htm-dns-" + id
	args := []string{"run", "-d", "--name", name,
		"--network", "container:" + netns,
		"-v", dir + ":/log",
		"-v", bin + ":/gatedns:ro",
		"--entrypoint", "/gatedns",
	}
	args = append(args, TestLabelArgs(label)...)
	args = append(args, AlpineImage)
	if _, err := Docker(30*time.Second, args...); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}}", name)
		if err == nil && strings.TrimSpace(out) == "true" {
			return nil
		}
		last, _ = Docker(5*time.Second, "logs", name)
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("dns gate exited: %s", last)
}

func ensureGateDNSBin(root string) (string, error) {
	dest, err := cacheFile(root, "gatedns.bin")
	if err != nil {
		return "", err
	}
	old, _ := os.ReadFile(dest)
	if string(old) == string(gateDNSBin) {
		return dest, nil
	}
	if err := os.WriteFile(dest, gateDNSBin, 0o755); err != nil {
		return "", err
	}
	return dest, nil
}

func gateResolvers(container string) ([]string, error) {
	out, err := Docker(10*time.Second, "exec", container, "cat", "/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	var up []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			if ip := net.ParseIP(f[1]); ip != nil && ip.To4() != nil {
				up = append(up, f[1])
			}
		}
	}
	return up, nil
}

func stopGateDNS(id string) {
	DockerIgnore("rm", "-f", "htm-dns-"+id)
}
