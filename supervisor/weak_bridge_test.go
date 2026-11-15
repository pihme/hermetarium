package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWeakBridgeForwardAccept(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureNetTools(root); err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	network := "htm-fwd-" + id
	var br string
	t.Cleanup(func() {
		if br != "" {
			_ = forgetSameBridge(br)
		}
		DockerIgnore("network", "rm", network)
	})
	if _, err := Docker(30*time.Second, "network", "create", "--driver", "bridge", "--internal",
		"--subnet", SubnetForID(id).CIDR, network); err != nil {
		t.Fatal(err)
	}
	br, err = acceptSameBridge(network)
	if err != nil {
		t.Fatal(err)
	}
	if !bridgeForwardRule(t, network, "iptables-nft") {
		t.Fatal("iptables-nft forward accept missing")
	}
	if !bridgeForwardRule(t, network, "iptables-legacy") {
		t.Fatal("iptables-legacy forward accept missing")
	}
	if err := forgetSameBridge(br); err != nil {
		t.Fatal(err)
	}
	if bridgeForwardRule(t, network, "iptables-nft") || bridgeForwardRule(t, network, "iptables-legacy") {
		t.Fatal("forward accept still present")
	}
}

func TestDestroyWeakMissingNetTools(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
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
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	// Restore the image before a later DestroyWeak, which needs it for the rule.
	t.Cleanup(func() { _ = DestroyWeak(id) })
	t.Cleanup(func() {
		if err := EnsureNetTools(root); err != nil {
			t.Errorf("restore %s: %v", NetToolsImage, err)
		}
	})
	acl := filepath.Join(root, "examples", "echo", "squid.conf")
	if _, err := CreateWeak(root, id, CreateOpts{Image: image, ACL: acl, NoInbound: true}); err != nil {
		t.Fatal(err)
	}
	network := "htm-" + id
	sub := SubnetForID(id)
	got, err := Docker(15*time.Second, "network", "inspect", "-f", "{{range .IPAM.Config}}{{.Subnet}}{{end}}", network)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != sub.CIDR {
		t.Fatalf("subnet %q, want %s", strings.TrimSpace(got), sub.CIDR)
	}
	raw, err := os.ReadFile(filepath.Join(VarDir(root), id, "bridge.name"))
	if err != nil {
		t.Fatal(err)
	}
	bridge := strings.TrimSpace(string(raw))
	if bridge == "" {
		t.Fatal("empty bridge name")
	}
	if !bridgeForwardRule(t, network, "iptables-nft") || !bridgeForwardRule(t, network, "iptables-legacy") {
		t.Fatal("FORWARD accept missing before destroy")
	}
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", "htm-box-"+id); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(30*time.Second, "rmi", NetToolsImage); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"destroy", id})
	if err := EnsureNetTools(root); err != nil {
		t.Fatal(err)
	}
	boxLeft := dockerNameExists("inspect", "-f", "{{.Id}}", "htm-box-"+id)
	netLeft := dockerNameExists("network", "inspect", network)
	nftLeft := forwardAcceptOnBridge(bridge, "iptables-nft")
	legacyLeft := forwardAcceptOnBridge(bridge, "iptables-legacy")
	if code == 0 && boxLeft && netLeft && nftLeft && legacyLeft {
		t.Fatal("destroy reported success while the box, the network, and both FORWARD accepts remain")
	}
	if code != 0 || boxLeft || netLeft || nftLeft || legacyLeft {
		t.Fatalf("destroy code=%d box=%v network=%v nft=%v legacy=%v", code, boxLeft, netLeft, nftLeft, legacyLeft)
	}
}

func dockerNameExists(args ...string) bool {
	_, err := Docker(15*time.Second, args...)
	return err == nil
}

func forwardAcceptOnBridge(bridge, ipt string) bool {
	script := `command -v "$IPT" >/dev/null 2>&1 || exit 2
"$IPT" -C FORWARD -i "$BR" -o "$BR" -j ACCEPT >/dev/null 2>&1
`
	_, err := runOnHostNet(script, "BR="+bridge, "IPT="+ipt)
	return err == nil
}

func TestBridgeIPv4(t *testing.T) {
	if got, ok := bridgeIPv4("10.20.0.9|10.20.0.0/24\n"); !ok || got != "10.20.0.9" {
		t.Fatalf("gateway = %q %v", got, ok)
	}
	if got, ok := bridgeIPv4("|10.41.0.0/24\n"); !ok || got != "10.41.0.1" {
		t.Fatalf("subnet = %q %v", got, ok)
	}
	if got, ok := bridgeIPv4("|\n"); ok {
		t.Fatalf("empty = %q", got)
	}
}

func bridgeForwardRule(t *testing.T, network, ipt string) bool {
	t.Helper()
	gw, err := networkIPv4Gateway(network)
	if err != nil {
		t.Fatal(err)
	}
	script := `br=$(ip -o -4 addr show | awk -v ip="$GW/" 'index($4, ip) == 1 { print $2; exit }')
test -n "$br"
command -v "$IPT" >/dev/null 2>&1 || exit 2
"$IPT" -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1
`
	_, err = Docker(30*time.Second, "run", "--rm",
		"--network", "host",
		"--cap-add", "NET_ADMIN",
		"-e", "GW="+gw,
		"-e", "IPT="+ipt,
		"--entrypoint", "sh",
		NetToolsImage, "-c", script,
	)
	return err == nil
}
