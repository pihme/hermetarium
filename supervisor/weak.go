package supervisor

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type WeakInstance struct {
	ID         string `json:"id"`
	Wall       string `json:"wall"`
	Network    string `json:"network"`
	Gate       string `json:"gate"`
	Box        string `json:"box"`
	GateIP     string `json:"gateIp"`
	BoxIP      string `json:"boxIp"`
	InboundURL string `json:"inboundUrl"`
	NoInbound  bool   `json:"noInbound"`
	LogPath    string `json:"logPath"`
	Dir        string `json:"dir"`
}

func CreateWeak(root, id string, opts CreateOpts) (*WeakInstance, error) {
	if opts.DataPath != "" {
		return nil, fmt.Errorf("--data applies only to --wall strong")
	}
	if err := EnsureImageExists(opts.Image); err != nil {
		return nil, err
	}
	if err := EnsureNetTools(root); err != nil {
		return nil, err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return nil, err
	}
	sub := SubnetForID(id)
	if err := InstallACL(dir, opts.ACL); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "io.jsonl")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return nil, err
	}
	conf, err := os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		return nil, err
	}
	https := ACLWantsHTTPS(conf)
	if https {
		if err := PrepareMITM(dir); err != nil {
			return nil, err
		}
		if err := InitMITMDB(root, dir); err != nil {
			return nil, err
		}
	}
	network := "htm-" + id
	gate := "htm-gate-" + id
	box := "htm-box-" + id

	if _, err := Docker(30*time.Second,
		// --internal makes Docker block every route out of this network, so
		// the box cannot leave even with NET_ADMIN. The gate also joins the
		// default bridge below, which is the only path to the Internet.
		"network", "create", "--driver", "bridge", "--internal", "--subnet", sub.CIDR,
		network,
	); err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = DestroyWeak(id)
		}
	}()

	// nftables accepts ICC and stops only its own chain. An iptables FORWARD
	// drop still discards the bridged packet, so the gate cannot reach the box.
	// ARP does not traverse that chain. The rule is this bridge only.
	if err := acceptSameBridge(network); err != nil {
		return nil, err
	}

	if err := runSquid(root, gate, dir, append([]string{
		"--network", "bridge",
		"--add-host", "inhabitant:" + sub.Box,
		"--sysctl", "net.ipv4.ip_forward=1",
	}, inboundPublish(opts.NoInbound)...)); err != nil {
		return nil, err
	}
	if _, err := Docker(30*time.Second, "network", "connect", "--ip", sub.Gate, network, gate); err != nil {
		return nil, err
	}
	if err := WaitSquid(dir, gate, 20*time.Second); err != nil {
		return nil, err
	}
	if err := lockGate(gate, sub.Gate, opts.NoInbound); err != nil {
		return nil, err
	}
	if err := applyIntercept(gate, sub.CIDR, https); err != nil {
		return nil, err
	}
	if err := runAfterGate(opts, id, gate); err != nil {
		return nil, err
	}

	boxArgs := []string{
		"run", "-d", "--name", box,
		"--network", network, "--ip", sub.Box,
		"--cap-add", "NET_ADMIN",
		"--add-host", ProbeHost + ":" + sub.Gate,
	}
	for _, h := range VendorHosts() {
		boxArgs = append(boxArgs, "--add-host", h+":"+sub.Gate)
	}
	if opts.CPUs > 0 {
		boxArgs = append(boxArgs, "--cpus", strconv.Itoa(opts.CPUs))
	}
	if https {
		boxArgs = append(boxArgs, mitmBoxDockerArgs(dir)...)
	}
	boxArgs = append(boxArgs, opts.Image)
	if _, err := Docker(30*time.Second, boxArgs...); err != nil {
		return nil, err
	}
	if _, err := Docker(15*time.Second,
		"exec", box, "sh", "-c",
		"ip route replace default via "+sub.Gate+" || ip route add default via "+sub.Gate,
	); err != nil {
		return nil, err
	}
	if https {
		if err := InstallBoxTrust(box); err != nil {
			return nil, err
		}
	}

	url, err := finishInbound(gate, opts.NoInbound, 20*time.Second)
	if err != nil {
		return nil, err
	}

	inst := &WeakInstance{
		ID: id, Wall: "weak", Network: network, Gate: gate, Box: box,
		GateIP: sub.Gate, BoxIP: sub.Box, InboundURL: url, NoInbound: opts.NoInbound,
		LogPath: logPath, Dir: dir,
	}
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), b, 0o644); err != nil {
		return nil, err
	}
	cleanup = false
	return inst, nil
}

func ExecWeak(inst *WeakInstance, argv []string) (stdout, stderr string, code int) {
	args := append([]string{"exec", inst.Box}, argv...)
	return DockerExec(60*time.Second, args...)
}

func DestroyWeak(id string) error {
	// Inhabitant images may declare VOLUME. -v removes those anonymous volumes.
	_ = forgetSameBridge("htm-" + id)
	DockerIgnore("rm", "-fv", "htm-box-"+id)
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
	removeGate(id)
	DockerIgnore("network", "rm", "htm-"+id)
	return nil
}

func networkIPv4Gateway(network string) (string, error) {
	out, err := Docker(15*time.Second, "network", "inspect", "-f", "{{range .IPAM.Config}}{{.Gateway}}|{{.Subnet}}\n{{end}}", network)
	if err != nil {
		return "", err
	}
	// Docker omits Gateway unless create passed --gateway. The bridge still
	// owns the first address of the subnet.
	addr, ok := bridgeIPv4(out)
	if !ok {
		return "", fmt.Errorf("network %s: no IPv4 gateway", network)
	}
	return addr, nil
}

func bridgeIPv4(inspect string) (string, bool) {
	var gateway string
	var subnet *net.IPNet
	for _, line := range strings.Split(inspect, "\n") {
		gw, cidr, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		if gateway == "" {
			if ip := net.ParseIP(strings.TrimSpace(gw)); ip != nil && ip.To4() != nil {
				gateway = ip.To4().String()
			}
		}
		if subnet == nil {
			if _, n, err := net.ParseCIDR(strings.TrimSpace(cidr)); err == nil && n.IP.To4() != nil {
				subnet = n
			}
		}
	}
	if gateway != "" {
		return gateway, true
	}
	if subnet == nil {
		return "", false
	}
	host := append(net.IP(nil), subnet.IP.To4()...)
	for i := len(host) - 1; i >= 0; i-- {
		host[i]++
		if host[i] != 0 {
			break
		}
	}
	if !subnet.Contains(host) {
		return "", false
	}
	return host.String(), true
}

func runOnHostNet(script, gw string) error {
	_, err := Docker(30*time.Second, "run", "--rm",
		"--network", "host",
		"--cap-add", "NET_ADMIN",
		"-e", "GW="+gw,
		"--entrypoint", "sh",
		NetToolsImage, "-c", script,
	)
	return err
}

const acceptSameBridgeScript = `set -eu
br=$(ip -o -4 addr show | awk -v ip="$GW/" 'index($4, ip) == 1 { print $2; exit }')
test -n "$br"
ok=0
msg=""
for ipt in iptables-legacy iptables-nft; do
  command -v "$ipt" >/dev/null 2>&1 || continue
  if "$ipt" -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1; then
    ok=1
    continue
  fi
  if err=$("$ipt" -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT 2>&1); then
    ok=1
  else
    msg="$msg$ipt: $err
"
  fi
done
if [ "$ok" != 1 ]; then
  printf '%s' "$msg" >&2
  exit 1
fi
`

const forgetSameBridgeScript = `br=$(ip -o -4 addr show | awk -v ip="$GW/" 'index($4, ip) == 1 { print $2; exit }')
if [ -z "$br" ]; then
  exit 0
fi
for ipt in iptables-legacy iptables-nft; do
  command -v "$ipt" >/dev/null 2>&1 || continue
  while "$ipt" -D FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1; do
    :
  done
done
exit 0
`

func acceptSameBridge(network string) error {
	gw, err := networkIPv4Gateway(network)
	if err != nil {
		return err
	}
	if err := runOnHostNet(acceptSameBridgeScript, gw); err != nil {
		return fmt.Errorf("accept forwarded traffic on %s: %w", network, err)
	}
	return nil
}

func forgetSameBridge(network string) error {
	gw, err := networkIPv4Gateway(network)
	if err != nil {
		return err
	}
	return runOnHostNet(forgetSameBridgeScript, gw)
}

func LoadWeak(root, id string) (*WeakInstance, error) {
	b, err := os.ReadFile(filepath.Join(VarDir(root), id, "instance.json"))
	if err != nil {
		return nil, err
	}
	var inst WeakInstance
	if err := json.Unmarshal(b, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

func SyncInstanceLog(dir string) error {
	return SyncAccessLog(filepath.Join(dir, "access.log"), filepath.Join(dir, "io.jsonl"))
}
