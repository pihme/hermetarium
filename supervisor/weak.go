package supervisor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func CreateWeak(root, id string, opts CreateOpts) (*WeakInstance, error) {
	defer func() { takeGateReady(id) }()
	if opts.Rootfs != "" {
		return nil, fmt.Errorf("the Docker wall rejects --rootfs")
	}
	dir, conf, https, err := prepareHabitat(root, id, opts)
	if err != nil {
		return nil, err
	}
	plan := WeakPlan(id)
	logPath := filepath.Join(dir, "io.jsonl")
	network := "htm-" + id
	gate := ContainerName(id, "gate")
	box := ContainerName(id, "box")

	if _, err := Docker(30*time.Second,
		// --internal makes Docker block every route out of this network, so
		// the box cannot leave even with NET_ADMIN. The gate also joins the
		// default bridge below, which is the only path to the Internet.
		"network", "create", "--driver", "bridge", "--internal", "--subnet", plan.CIDR,
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
	br, err := acceptSameBridge(network)
	if err != nil {
		return nil, err
	}
	// Destroy reads this after the bridge is gone. The rule names that
	// interface, so a later retry still has to delete it.
	if err := os.WriteFile(filepath.Join(dir, "bridge.name"), []byte(br+"\n"), 0o644); err != nil {
		_ = forgetSameBridge(br)
		return nil, err
	}

	if err := runSquid(root, gate, dir, opts.TestLabel, append([]string{
		"--network", "bridge",
		"--add-host", "inhabitant:" + plan.Guest,
		"--sysctl", "net.ipv4.ip_forward=1",
	}, inboundPublish(opts.NoInbound)...)); err != nil {
		return nil, err
	}
	if _, err := Docker(30*time.Second, "network", "connect", "--ip", plan.Gate, network, gate); err != nil {
		return nil, err
	}
	if err := WaitSquid(dir, gate, 20*time.Second); err != nil {
		return nil, err
	}
	// One script, same bytes the Firecracker container runs. Failure must not start the box.
	if err := applyGateRules(gate, dir, plan.Gate, plan.CIDR, https, opts.NoInbound); err != nil {
		return nil, err
	}
	// Sidecars share this netns only after Squid is listening.
	if err := runGateReady(root, id, gate); err != nil {
		return nil, err
	}
	if err := startGateDNS(root, id, gate, gate, plan.Gate, opts.TestLabel, conf); err != nil {
		return nil, err
	}

	// Docker keeps 127.0.0.11 on an --internal network and answers outside
	// names with SERVFAIL. Mount the gate resolver before the image command.
	resolv := filepath.Join(dir, "resolv.conf")
	if err := os.WriteFile(resolv, []byte("nameserver "+plan.Gate+"\n"), 0o644); err != nil {
		return nil, err
	}
	boxArgs := []string{
		"run", "-d", "--name", box,
		"--network", network, "--ip", plan.Guest,
		"--cap-add", "NET_ADMIN",
		"-v", resolv + ":/etc/resolv.conf:ro",
	}
	for _, h := range plan.Names {
		boxArgs = append(boxArgs, "--add-host", h+":"+plan.Gate)
	}
	if opts.CPUs > 0 {
		boxArgs = append(boxArgs, "--cpus", strconv.Itoa(opts.CPUs))
	}
	if https {
		mitmArgs, err := mitmBoxDockerArgs(root, dir)
		if err != nil {
			return nil, err
		}
		boxArgs = append(boxArgs, mitmArgs...)
	}
	boxArgs = append(boxArgs, TestLabelArgs(opts.TestLabel)...)
	boxArgs = append(boxArgs, opts.Image)
	if _, err := Docker(30*time.Second, boxArgs...); err != nil {
		return nil, err
	}
	// resolv.conf is mounted read-only. This only installs the default route.
	if _, err := Docker(15*time.Second,
		"exec", box, "sh", "-c",
		"ip route replace default via "+plan.Gate+" || ip route add default via "+plan.Gate,
	); err != nil {
		return nil, err
	}
	if https {
		if err := installGateCert(box); err != nil {
			return nil, err
		}
	}

	url, err := finishInbound(gate, opts.NoInbound, 20*time.Second)
	if err != nil {
		return nil, err
	}

	inst := &WeakInstance{
		ID: id, Wall: "weak", Network: network, Gate: gate, Box: box,
		GateIP: plan.Gate, BoxIP: plan.Guest, InboundURL: url, NoInbound: opts.NoInbound,
		LogPath: logPath, Dir: dir,
	}
	if err := writeInstance(inst); err != nil {
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
	// bridge.name still names the FORWARD rule after the network is gone, so
	// a failed forget must not skip the box and the network.
	err := forgetSavedBridge(id)
	DockerIgnore("rm", "-fv", ContainerName(id, "box"))
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
	stopGateDNS(id)
	removeGate(id)
	DockerIgnore("network", "rm", "htm-"+id)
	return err
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

func runOnHostNet(script string, env ...string) (string, error) {
	args := []string{"run", "--rm", "--network", "host", "--cap-add", "NET_ADMIN"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, "--entrypoint", "sh", NetToolsImage, "-c", script)
	return Docker(30*time.Second, args...)
}

const acceptSameBridgeScript = `set -eu
br=$(ip -o -4 addr show | awk -v ip="$GW/" 'index($4, ip) == 1 { print $2; exit }')
test -n "$br"
ok=0
msg=""
owned=0
for ipt in iptables-legacy iptables-nft; do
  command -v "$ipt" >/dev/null 2>&1 || continue
  # -C does not create the table. A missing table here means the insert will.
  if [ "$ipt" = iptables-nft ] && command -v nft >/dev/null 2>&1; then
    err=$(nft list table ip filter 2>&1 >/dev/null) || {
      case "$err" in
        *"No such file or directory"*) owned=1 ;;
      esac
    }
  fi
  if "$ipt" -w -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1; then
    ok=1
    continue
  fi
  if err=$("$ipt" -w -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT 2>&1); then
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
printf '%s\n' "$br"
if [ "$owned" = 1 ] && nft list table ip filter >/dev/null 2>&1; then
  printf 'owned\n'
fi
`

// Exit 0 only when every backend that exists no longer has the rule.
// A missing binary is skipped. -C exit 1 is "no match"; any other failure
// still has to be retried, because the bridge name is all destroy kept.
// OWNED=1 means hermetarium created table ip filter. Delete only when the
// listing is nothing but FORWARD, INPUT, and OUTPUT, each with no rules and
// policy accept. A drop policy, another chain, a rule, a named object, a
// table flag, or a chain that is not the filter hook at priority 0 stays.
// Real nft delete table removes a non-empty table and does not fail, so the
// delete is not a content check. A delete nft refuses is listed again; the
// test fake returns EBUSY for that path, and the marker then stays.
const dropOwnedFilterTableScript = `# Allow-list. A policy drop, a table flag, or a chain that is not the
# iptables-nft filter hook has no rule and would otherwise be deleted.
filter_table_disposable() {
  printf '%s' "$1" | awk '
function skip_ws() {
  while (i <= n) {
    c = substr(s, i, 1)
    if (c != " " && c != "\t" && c != "\n" && c != "\r") return
    i++
  }
}
function parse_string(    res, c) {
  if (badparse) return ""
  if (substr(s, i, 1) != "\"") { badparse = 1; return "" }
  i++
  res = ""
  while (i <= n) {
    c = substr(s, i, 1)
    i++
    if (c == "\\") {
      if (i > n) { badparse = 1; return "" }
      c = substr(s, i, 1)
      i++
      res = res c
      continue
    }
    if (c == "\"") return res
    res = res c
  }
  badparse = 1
  return ""
}
function parse_skip(    c, depth, instr, esc) {
  if (badparse) return
  skip_ws()
  if (i > n) { badparse = 1; return }
  c = substr(s, i, 1)
  if (c == "\"") { parse_string(); return }
  if (c != "{" && c != "[") {
    while (i <= n) {
      c = substr(s, i, 1)
      if (c == "," || c == "]" || c == "}" || c == " " || c == "\t" || c == "\n" || c == "\r") return
      i++
    }
    return
  }
  depth = 0
  instr = 0
  esc = 0
  while (i <= n) {
    c = substr(s, i, 1)
    i++
    if (instr) {
      if (esc) { esc = 0; continue }
      if (c == "\\") { esc = 1; continue }
      if (c == "\"") instr = 0
      continue
    }
    if (c == "\"") { instr = 1; continue }
    if (c == "{" || c == "[") depth++
    else if (c == "}" || c == "]") {
      depth--
      if (depth == 0) return
    }
  }
  badparse = 1
}
function parse_table_body(    key, c) {
  if (badparse) return
  skip_ws()
  if (substr(s, i, 1) != "{") { badparse = 1; return }
  i++
  skip_ws()
  if (substr(s, i, 1) == "}") { i++; keep = 1; return }
  while (i <= n && !badparse) {
    skip_ws()
    key = parse_string()
    if (badparse) return
    skip_ws()
    if (substr(s, i, 1) != ":") { badparse = 1; return }
    i++
    skip_ws()
    if (key == "family" || key == "name") parse_string()
    else if (key == "handle") parse_skip()
    else { keep = 1; parse_skip() }
    if (badparse) return
    skip_ws()
    c = substr(s, i, 1)
    if (c == ",") { i++; continue }
    if (c == "}") { i++; return }
    badparse = 1
    return
  }
}
function parse_chain_body(    key, name, policy, typ, hook, prio, c, seen_name, seen_policy, seen_type, seen_hook, seen_prio, at) {
  if (badparse) return
  skip_ws()
  if (substr(s, i, 1) != "{") { badparse = 1; return }
  i++
  name = ""
  policy = ""
  typ = ""
  hook = ""
  prio = ""
  seen_name = 0
  seen_policy = 0
  seen_type = 0
  seen_hook = 0
  seen_prio = 0
  skip_ws()
  if (substr(s, i, 1) == "}") { i++; keep = 1; return }
  while (i <= n && !badparse) {
    skip_ws()
    key = parse_string()
    if (badparse) return
    skip_ws()
    if (substr(s, i, 1) != ":") { badparse = 1; return }
    i++
    skip_ws()
    if (key == "name") {
      name = parse_string()
      seen_name = 1
    } else if (key == "policy") {
      policy = parse_string()
      seen_policy = 1
    } else if (key == "type") {
      typ = parse_string()
      seen_type = 1
    } else if (key == "hook") {
      hook = parse_string()
      seen_hook = 1
    } else if (key == "prio") {
      at = i
      parse_skip()
      prio = substr(s, at, i - at)
      seen_prio = 1
    } else if (key == "handle") {
      parse_skip()
    } else if (key == "family" || key == "table") {
      parse_string()
    } else {
      keep = 1
      parse_skip()
    }
    if (badparse) return
    skip_ws()
    c = substr(s, i, 1)
    if (c == ",") { i++; continue }
    if (c == "}") { i++; break }
    badparse = 1
    return
  }
  if (!seen_name || !seen_policy) keep = 1
  if (name != "FORWARD" && name != "INPUT" && name != "OUTPUT") keep = 1
  if (policy != "accept") keep = 1
  if (seen_type && typ != "filter") keep = 1
  if (seen_hook && !((name == "INPUT" && hook == "input") || (name == "FORWARD" && hook == "forward") || (name == "OUTPUT" && hook == "output"))) keep = 1
  if (seen_prio && prio != "0") keep = 1
}
function parse_element(    key, c) {
  if (badparse) return
  skip_ws()
  if (substr(s, i, 1) != "{") { badparse = 1; return }
  i++
  skip_ws()
  if (substr(s, i, 1) == "}") { i++; keep = 1; return }
  while (i <= n && !badparse) {
    skip_ws()
    key = parse_string()
    if (badparse) return
    skip_ws()
    if (substr(s, i, 1) != ":") { badparse = 1; return }
    i++
    skip_ws()
    if (key == "chain") parse_chain_body()
    else if (key == "table") parse_table_body()
    else if (key == "metainfo") parse_skip()
    else { keep = 1; parse_skip() }
    if (badparse) return
    skip_ws()
    c = substr(s, i, 1)
    if (c == ",") { i++; continue }
    if (c == "}") { i++; return }
    badparse = 1
    return
  }
}
function parse_elements(    c) {
  if (badparse) return
  skip_ws()
  if (substr(s, i, 1) != "[") { badparse = 1; return }
  i++
  skip_ws()
  if (substr(s, i, 1) == "]") { i++; return }
  while (!badparse) {
    parse_element()
    if (badparse) return
    skip_ws()
    c = substr(s, i, 1)
    if (c == ",") { i++; continue }
    if (c == "]") { i++; return }
    badparse = 1
    return
  }
}
function parse_root(    key) {
  skip_ws()
  if (substr(s, i, 1) != "{") { badparse = 1; return }
  i++
  skip_ws()
  key = parse_string()
  if (badparse || key != "nftables") { badparse = 1; return }
  skip_ws()
  if (substr(s, i, 1) != ":") { badparse = 1; return }
  i++
  parse_elements()
  if (badparse) return
  skip_ws()
  if (substr(s, i, 1) == ",") { keep = 1; i++; parse_skip() }
  skip_ws()
  if (substr(s, i, 1) != "}") { badparse = 1; return }
  i++
  skip_ws()
  if (i <= n) badparse = 1
}
{
  s = s $0
}
END {
  n = length(s)
  i = 1
  keep = 0
  badparse = 0
  parse_root()
  if (badparse || keep) exit 1
  exit 0
}
'
}
if [ "${OWNED:-}" = 1 ]; then
  command -v nft >/dev/null 2>&1 || {
    printf 'nft: missing\n' >&2
    exit 1
  }
  err=$(nft list table ip filter 2>&1 >/dev/null) || {
    case "$err" in
      *"No such file or directory"*)
        printf 'dropped\n'
        exit 0
        ;;
    esac
    printf 'nft: %s\n' "$err" >&2
    exit 1
  }
  rules=$(nft -j list table ip filter 2>&1) || {
    printf 'nft: %s\n' "$rules" >&2
    exit 1
  }
  if ! filter_table_disposable "$rules"; then
    exit 0
  fi
  if ! err=$(nft delete table ip filter 2>&1); then
    again=$(nft -j list table ip filter 2>&1) || {
      case "$again" in
        *"No such file or directory"*)
          printf 'dropped\n'
          exit 0
          ;;
      esac
      printf 'nft: %s\n' "$again" >&2
      exit 1
    }
    if ! filter_table_disposable "$again"; then
      exit 0
    fi
    printf 'nft: %s\n' "$err" >&2
    exit 1
  fi
  printf 'dropped\n'
fi
exit 0
`

const forgetSameBridgeScript = `set -u
test -n "$BR" || exit 1
for ipt in iptables-legacy iptables-nft; do
  command -v "$ipt" >/dev/null 2>&1 || continue
  while true; do
    "$ipt" -w -C FORWARD -i "$BR" -o "$BR" -j ACCEPT >/dev/null 2>&1
    st=$?
    if [ "$st" -eq 1 ]; then
      break
    fi
    if [ "$st" -ne 0 ]; then
      printf '%s: check failed\n' "$ipt" >&2
      exit 1
    fi
    if ! "$ipt" -w -D FORWARD -i "$BR" -o "$BR" -j ACCEPT >/dev/null 2>&1; then
      printf '%s: delete failed\n' "$ipt" >&2
      exit 1
    fi
  done
done
` + dropOwnedFilterTableScript

func acceptSameBridge(network string) (string, error) {
	unlock, err := lockIPFilter()
	if err != nil {
		return "", err
	}
	defer unlock()
	return acceptSameBridgeLocked(network)
}

func acceptSameBridgeLocked(network string) (string, error) {
	gw, err := networkIPv4Gateway(network)
	if err != nil {
		return "", err
	}
	out, err := runOnHostNet(acceptSameBridgeScript, "GW="+gw)
	if err != nil {
		return "", fmt.Errorf("accept forwarded traffic on %s: %w", network, err)
	}
	br, owned, err := parseBridgeOwner(out)
	if err != nil {
		return "", fmt.Errorf("accept forwarded traffic on %s: %w", network, err)
	}
	if owned {
		if err := rememberFilterTableOwned(); err != nil {
			// This insert created the table. The marker is what a later
			// destroy would have trusted, so clean up with that same fact.
			_ = forgetSameBridgeLocked(br, true)
			return "", err
		}
	}
	return br, nil
}

func parseBridgeOwner(out string) (string, bool, error) {
	text := strings.TrimSpace(out)
	lines := strings.Split(text, "\n")
	if text == "" || len(lines) > 2 {
		return "", false, fmt.Errorf("bridge name %q", out)
	}
	br := strings.TrimSpace(lines[0])
	if br == "" || strings.ContainsAny(br, " \t") {
		return "", false, fmt.Errorf("bridge name %q", out)
	}
	if len(lines) == 1 {
		return br, false, nil
	}
	if strings.TrimSpace(lines[1]) != "owned" {
		return "", false, fmt.Errorf("bridge name %q", out)
	}
	return br, true, nil
}

// The creator may be destroyed while another habitat still has a rule, so the
// flag lives beside the instance dirs until a later destroy finds only the
// empty iptables-nft accept chains.
func filterOwnedPath(root string) string {
	return filepath.Join(VarDir(root), ".ip-filter-owned")
}

func rememberFilterTableOwned() error {
	root, err := Root()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(VarDir(root), 0o755); err != nil {
		return err
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return err
	}
	// The token is the ownership. An in-place rewrite keeps the inode, so a
	// later destroy must not unlink a marker it did not write.
	return os.WriteFile(filterOwnedPath(root), []byte(hex.EncodeToString(buf[:])+"\n"), 0o644)
}

func clearFilterTableOwned(root string) error {
	err := os.Remove(filterOwnedPath(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readIPFilterMarker(root string) (string, error) {
	fi, err := os.Stat(filterOwnedPath(root))
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", fmt.Errorf("filter table marker is a directory")
	}
	b, err := os.ReadFile(filterOwnedPath(root))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("empty filter table marker")
	}
	return token, nil
}

// lockIPFilter is the critical section for the FORWARD rules and the marker.
// Two destroys must not both see an empty table, and a destroy must not
// unlink a marker written for a table created after it listed.
func lockIPFilter() (func(), error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(VarDir(root), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(VarDir(root), ".ip-filter.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func scriptDroppedFilterTable(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "dropped" {
			return true
		}
	}
	return false
}

func forgetSavedBridge(id string) error {
	root, err := Root()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(VarDir(root), id, "bridge.name"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	br := strings.TrimSpace(string(b))
	if br == "" {
		return fmt.Errorf("habitat %s: empty bridge name", id)
	}
	// Create built this image. Destroy still deletes the saved rule when the
	// image has since been removed.
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", NetToolsImage); err != nil {
		if buildErr := EnsureNetTools(root); buildErr != nil {
			return fmt.Errorf("rebuild %s: %w", NetToolsImage, buildErr)
		}
	}
	return forgetSameBridge(br)
}

func forgetSameBridge(bridge string) error {
	unlock, err := lockIPFilter()
	if err != nil {
		return err
	}
	defer unlock()
	return forgetSameBridgeLocked(bridge, false)
}

func forgetSameBridgeLocked(bridge string, forceOwned bool) error {
	env := []string{"BR=" + bridge}
	root, rootErr := Root()
	var marker string
	ownedFile := false
	if rootErr == nil {
		var idErr error
		marker, idErr = readIPFilterMarker(root)
		ownedFile = idErr == nil
	}
	if ownedFile || forceOwned {
		env = append(env, "OWNED=1")
	}
	out, err := runOnHostNet(forgetSameBridgeScript, env...)
	if err != nil {
		return fmt.Errorf("remove forward accept on %s: %w", bridge, err)
	}
	// forceOwned cleans up an insert whose marker write failed. It must not
	// unlink a marker this call did not see.
	if !scriptDroppedFilterTable(out) || !ownedFile {
		return nil
	}
	now, err := readIPFilterMarker(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove forward accept on %s: %w", bridge, err)
	}
	if now != marker {
		return nil
	}
	if err := clearFilterTableOwned(root); err != nil {
		return fmt.Errorf("remove forward accept on %s: %w", bridge, err)
	}
	return nil
}

func SyncInstanceLog(dir string) error {
	return SyncAccessLog(filepath.Join(dir, "access.log"), filepath.Join(dir, "io.jsonl"))
}
