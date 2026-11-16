package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// gatePort is one listener the habitat may open toward the gate.
// SPEC §7: TCP 3128, TCP 3130, TCP 3131, and TCP/UDP 53. Nothing else.
type gatePort struct {
	Proto string
	Port  int
}

// gateAllow is the only list of those listeners. Both walls install it.
func gateAllow() []gatePort {
	return []gatePort{
		{Proto: "tcp", Port: SquidPort},
		{Proto: "tcp", Port: HTTPSInterceptPort},
		{Proto: "tcp", Port: GateCAPort},
		{Proto: "udp", Port: 53},
		{Proto: "tcp", Port: 53},
	}
}

// gateRulesScript is the iptables applied in each wall's netns.
// Addresses and the HTTPS bit come from the environment, so the weak wall
// and the strong wall cannot grow a second copy of the port list.
func gateRulesScript() string {
	var b strings.Builder
	b.WriteString(`#!/bin/sh
set -eu
hab=$(ip -o -4 addr show | awk -v ip="$GATE_IP" 'index($4, ip "/") == 1 {print $2; exit}')
test -n "$hab"
test -n "$GATE_CIDR"
gw=$(ip route | awk '/^default/ {print $3; exit}')
test -n "$gw"
iptables -P FORWARD DROP
iptables -A INPUT -i lo -j ACCEPT
iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
`)
	for _, p := range gateAllow() {
		fmt.Fprintf(&b, "iptables -A INPUT -i \"$hab\" -p %s --dport %d -j ACCEPT\n", p.Proto, p.Port)
	}
	fmt.Fprintf(&b, `if [ "${NO_INBOUND:-}" != 1 ]; then
  iptables -A INPUT -s "$gw" -p tcp --dport %d -j ACCEPT
fi
iptables -P INPUT DROP
iptables -t nat -A PREROUTING -s "$GATE_CIDR" -p tcp --dport 80 -j REDIRECT --to-ports %d
if [ "${GATE_HTTPS:-}" = 1 ]; then
  iptables -t nat -A PREROUTING -s "$GATE_CIDR" -p tcp --dport 443 -j REDIRECT --to-ports %d
fi
`, InboundPort, SquidPort, HTTPSInterceptPort)
	return b.String()
}

func writeGateRules(dir string) error {
	return os.WriteFile(filepath.Join(dir, "gate-rules.sh"), []byte(gateRulesScript()), 0o755)
}

func gateRuleEnv(gateIP, cidr string, https, noInbound bool) []string {
	env := []string{"GATE_IP=" + gateIP, "GATE_CIDR=" + cidr}
	if https {
		env = append(env, "GATE_HTTPS=1")
	}
	if noInbound {
		env = append(env, "NO_INBOUND=1")
	}
	return env
}

// applyGateRules installs gateRulesScript in netns. A failure is the caller's
// signal not to start the inhabitant.
func applyGateRules(netns, dir, gateIP, cidr string, https, noInbound bool) error {
	if err := writeGateRules(dir); err != nil {
		return err
	}
	args := []string{
		"run", "--rm",
		"--network", "container:" + netns,
		"--cap-add", "NET_ADMIN",
	}
	for _, e := range gateRuleEnv(gateIP, cidr, https, noInbound) {
		args = append(args, "-e", e)
	}
	args = append(args,
		"-v", filepath.Join(dir, "gate-rules.sh")+":/gate-rules.sh:ro",
		"--entrypoint", "sh",
		NetToolsImage,
		"/gate-rules.sh",
	)
	if _, err := Docker(30*time.Second, args...); err != nil {
		return fmt.Errorf("gate rules: %w", err)
	}
	return nil
}

// waitGateRules blocks until the Firecracker container has installed the
// shared script. The container exits when it cannot, and create must not continue.
func waitGateRules(dir, name string) error {
	ready := filepath.Join(dir, "rules.ready")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return nil
		}
		if !containerRunning(name) {
			return fmt.Errorf("gate rules: %s", strings.TrimSpace(containerLogs(name)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !containerRunning(name) {
		return fmt.Errorf("gate rules: %s", strings.TrimSpace(containerLogs(name)))
	}
	return fmt.Errorf("gate rules were not applied")
}

// waitGateCA blocks until the Firecracker container's httpd has accepted.
// ca.ready is written only after a fetch succeeds, so a dead httpd cannot
// look like a published certificate. Create must not release the guest before this.
func waitGateCA(dir, name string) error {
	ready := filepath.Join(dir, "ca.ready")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return nil
		}
		if !containerRunning(name) {
			return fmt.Errorf("gate certificate: %s", strings.TrimSpace(containerLogs(name)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !containerRunning(name) {
		return fmt.Errorf("gate certificate: %s", strings.TrimSpace(containerLogs(name)))
	}
	return fmt.Errorf("gate certificate was not served")
}

// installGateCert runs the shared install script. The weak wall mounts the
// certificate first because the image entrypoint is already PID 1, so the
// script finds the file and only updates the trust store. The strong guest
// sources the same script and fetches the certificate httpd is holding.
func installGateCert(container string) error {
	if _, err := Docker(30*time.Second, "exec", container, "sh", "/gate-cert.sh"); err != nil {
		return fmt.Errorf("install gate certificate: %w", err)
	}
	return nil
}
