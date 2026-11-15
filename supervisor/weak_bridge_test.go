package supervisor

import (
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
	t.Cleanup(func() {
		_ = forgetSameBridge(network)
		DockerIgnore("network", "rm", network)
	})
	if _, err := Docker(30*time.Second, "network", "create", "--driver", "bridge", "--internal",
		"--subnet", "10.254.254.0/24", "--gateway", "10.254.254.1", network); err != nil {
		t.Fatal(err)
	}
	if err := acceptSameBridge(network); err != nil {
		t.Fatal(err)
	}
	if !bridgeForwardRule(t, network, "iptables-nft") {
		t.Fatal("iptables-nft forward accept missing")
	}
	if err := forgetSameBridge(network); err != nil {
		t.Fatal(err)
	}
	if bridgeForwardRule(t, network, "iptables-nft") || bridgeForwardRule(t, network, "iptables-legacy") {
		t.Fatal("forward accept still present")
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
