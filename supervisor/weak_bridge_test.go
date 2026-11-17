package supervisor

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	// Registered first so it runs after DestroyWeak drops the container.
	t.Cleanup(func() {
		if _, err := Docker(30*time.Second, "rmi", image); err != nil {
			t.Errorf("remove %s: %v", image, err)
		}
	})
	t.Cleanup(func() { _ = DestroyWeak(id) })
	t.Cleanup(func() {
		if err := EnsureNetTools(root); err != nil {
			t.Errorf("restore %s: %v", NetToolsImage, err)
		}
	})
	acl := filepath.Join(root, "examples", "echo", "squid.conf")
	if _, err := CreateWeak(root, id, WithTestLabel(t, CreateOpts{Image: image, ACL: acl, NoInbound: true})); err != nil {
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
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", ContainerName(id, "box")); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(30*time.Second, "rmi", NetToolsImage); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"destroy", id})
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", NetToolsImage); err != nil {
		t.Fatalf("rebuild %s: %v", NetToolsImage, err)
	}
	boxLeft := dockerNameExists("inspect", "-f", "{{.Id}}", ContainerName(id, "box"))
	netLeft := dockerNameExists("network", "inspect", network)
	nftLeft := forwardAcceptOnBridge(bridge, "iptables-nft")
	legacyLeft := forwardAcceptOnBridge(bridge, "iptables-legacy")
	if code != 0 || boxLeft || netLeft || nftLeft || legacyLeft {
		t.Fatalf("destroy code=%d box=%v network=%v nft=%v legacy=%v", code, boxLeft, netLeft, nftLeft, legacyLeft)
	}
}

func TestDestroyWeakForgetError(t *testing.T) {
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
	var bridge string
	t.Cleanup(func() {
		if _, err := Docker(30*time.Second, "rmi", image); err != nil {
			t.Errorf("remove %s: %v", image, err)
		}
	})
	t.Cleanup(func() {
		_ = DestroyWeak(id)
		if bridge != "" {
			if err := forgetSameBridge(bridge); err != nil {
				t.Errorf("remove forward accept on %s: %v", bridge, err)
			}
		}
	})
	acl := filepath.Join(root, "examples", "echo", "squid.conf")
	if _, err := CreateWeak(root, id, WithTestLabel(t, CreateOpts{Image: image, ACL: acl, NoInbound: true})); err != nil {
		t.Fatal(err)
	}
	namePath := filepath.Join(VarDir(root), id, "bridge.name")
	raw, err := os.ReadFile(namePath)
	if err != nil {
		t.Fatal(err)
	}
	bridge = strings.TrimSpace(string(raw))
	if bridge == "" {
		t.Fatal("empty bridge name")
	}
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", ContainerName(id, "box")); err != nil {
		t.Fatal(err)
	}
	network := "htm-" + id
	if _, err := Docker(15*time.Second, "network", "inspect", network); err != nil {
		t.Fatal(err)
	}
	// forgetSavedBridge refuses an empty saved name and must not skip the rest.
	if err := os.WriteFile(namePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	code := Run([]string{"destroy", id})
	w.Close()
	os.Stderr = old
	msg, _ := io.ReadAll(r)
	if code == 0 {
		t.Fatalf("destroy exited 0, stderr=%q", msg)
	}
	if !bytes.Contains(msg, []byte("empty bridge name")) {
		t.Fatalf("stderr %q", msg)
	}
	if dockerNameExists("inspect", "-f", "{{.Id}}", ContainerName(id, "box")) {
		t.Fatal("box still present")
	}
	if dockerNameExists("network", "inspect", network) {
		t.Fatal("network still present")
	}
	if _, err := os.Stat(namePath); err != nil {
		t.Fatal(err)
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

func TestNftFilterTableDrop(t *testing.T) {
	t.Run("commands", func(t *testing.T) {
		cases := []struct {
			name       string
			owned      bool
			json       string
			wantDelete bool
			wantDrop   bool
		}{
			{name: "empty", owned: true, wantDelete: true, wantDrop: true,
				json: `{"nftables":[{"table":{"name":"filter"}},{"chain":{"name":"FORWARD","policy":"accept"}}]}`},
			{name: "builtins", owned: true, wantDelete: true, wantDrop: true,
				json: `{"nftables":[{"chain":{"name":"INPUT","policy":"accept"}},{"chain":{"name":"FORWARD","policy":"accept"}},{"chain":{"name":"OUTPUT","policy":"accept"}}]}`},
			{name: "spaced", owned: true, wantDelete: true, wantDrop: true,
				json: `{"nftables": [{"metainfo": {"version": "1.1.6", "release_name": "Commodore Bullmoose #7", "json_schema_version": 1}}, {"table": {"family": "ip", "name": "filter", "handle": 1}}, {"chain": {"family": "ip", "table": "filter", "name": "FORWARD", "handle": 1, "type": "filter", "hook": "forward", "prio": 0, "policy": "accept"}}]}`},
			{name: "preexisting", owned: false, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}}]}`},
			{name: "rule", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"rule":{"chain":"FORWARD"}}]}`},
			{name: "set", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD"}},{"set":{"name":"peers"}}]}`},
			{name: "map", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"map":{"name":"peers"}}]}`},
			{name: "counter", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"counter":{"name":"hits"}}]}`},
			{name: "flowtable", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"flowtable":{"name":"ft"}}]}`},
			{name: "drop", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables": [{"chain": {"name": "FORWARD", "policy": "drop"}}]}`},
			{name: "user", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}},{"chain":{"name":"USERCHAIN"}}]}`},
			{name: "quota", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}},{"quota":{"name":"q"}}]}`},
			{name: "limit", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}},{"limit":{"name":"lim"}}]}`},
			{name: "synproxy", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}},{"synproxy":{"name":"sp","flags":["timestamp"]}}]}`},
			{name: "flags-string", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"table":{"family":"ip","name":"filter","handle":1,"flags":"dormant"}},{"chain":{"family":"ip","table":"filter","name":"FORWARD","handle":1,"type":"filter","hook":"forward","prio":0,"policy":"accept"}},{"chain":{"family":"ip","table":"filter","name":"INPUT","handle":2,"type":"filter","hook":"input","prio":0,"policy":"accept"}},{"chain":{"family":"ip","table":"filter","name":"OUTPUT","handle":3,"type":"filter","hook":"output","prio":0,"policy":"accept"}}]}`},
			{name: "flags-array", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"table":{"family":"ip","name":"filter","handle":1,"flags":["dormant","persist"]}},{"chain":{"family":"ip","table":"filter","name":"FORWARD","handle":1,"type":"filter","hook":"forward","prio":0,"policy":"accept"}}]}`},
			{name: "comment", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"family":"ip","table":"filter","name":"FORWARD","handle":1,"type":"filter","hook":"forward","prio":0,"policy":"accept","comment":"keep"}}]}`},
			{name: "nat", owned: true, wantDelete: false, wantDrop: false,
				json: `{"nftables":[{"chain":{"family":"ip","table":"filter","name":"FORWARD","handle":1,"type":"nat","hook":"prerouting","prio":-100,"policy":"accept"}}]}`},
			{name: "full", owned: true, wantDelete: true, wantDrop: true,
				json: `{"nftables":[{"table":{"family":"ip","name":"filter","handle":1}},{"chain":{"family":"ip","table":"filter","name":"INPUT","handle":2,"type":"filter","hook":"input","prio":0,"policy":"accept"}},{"chain":{"family":"ip","table":"filter","name":"FORWARD","handle":3,"type":"filter","hook":"forward","prio":0,"policy":"accept"}},{"chain":{"family":"ip","table":"filter","name":"OUTPUT","handle":4,"type":"filter","hook":"output","prio":0,"policy":"accept"}}]}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				logPath := filepath.Join(t.TempDir(), "nft.log")
				bin := t.TempDir()
				body := `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
if [ "$1" = "-j" ]; then
  printf '%s\n' "$JSON"
fi
exit 0
`
				if err := os.WriteFile(filepath.Join(bin, "nft"), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("sh", "-c", dropOwnedFilterTableScript)
				// awk is the host one. nft in bin has to win over a real nft.
				cmd.Env = []string{
					"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
					"LOG=" + logPath,
					"JSON=" + tc.json,
				}
				if tc.owned {
					cmd.Env = append(cmd.Env, "OWNED=1")
				}
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("script: %v\n%s%s", err, stderr.String(), stdout.String())
				}
				logb, err := os.ReadFile(logPath)
				if err != nil && tc.owned {
					t.Fatal(err)
				}
				log := string(logb)
				deleted := false
				for _, line := range strings.Split(log, "\n") {
					if strings.TrimSpace(line) == "" {
						continue
					}
					if strings.Contains(line, "delete") {
						if strings.TrimSpace(line) != "delete table ip filter" {
							t.Fatalf("delete command %q\nlog:\n%s", line, log)
						}
						deleted = true
					}
				}
				if deleted != tc.wantDelete {
					t.Fatalf("delete=%v, want %v\nlog:\n%s", deleted, tc.wantDelete, log)
				}
				if got := scriptDroppedFilterTable(stdout.String()); got != tc.wantDrop {
					t.Fatalf("dropped=%v, want %v\nstdout:\n%s", got, tc.wantDrop, stdout.String())
				}
				if !tc.owned && strings.TrimSpace(log) != "" {
					t.Fatalf("nft ran for a pre-existing table:\n%s", log)
				}
			})
		}
	})

	t.Run("owner", func(t *testing.T) {
		for _, mode := range []string{"absent", "present"} {
			t.Run(mode, func(t *testing.T) {
				dir := t.TempDir()
				logPath := filepath.Join(dir, "cmd.log")
				countPath := filepath.Join(dir, "count")
				writeExec(t, filepath.Join(dir, "ip"), `#!/bin/sh
echo "2: br-test    inet 10.9.0.1/24 brd 10.9.0.255 scope global br-test"
`)
				ipt := `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
if [ "$2" = "-C" ]; then
  exit 1
fi
exit 0
`
				writeExec(t, filepath.Join(dir, "iptables-legacy"), ipt)
				writeExec(t, filepath.Join(dir, "iptables-nft"), ipt)
				writeExec(t, filepath.Join(dir, "nft"), `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
n=0
if [ -f "$COUNT" ]; then
  n=$(cat "$COUNT")
fi
n=$((n + 1))
printf '%s\n' "$n" > "$COUNT"
if [ "$MODE" = "present" ]; then
  exit 0
fi
if [ "$n" -eq 1 ]; then
  echo "Error: No such file or directory" >&2
  exit 1
fi
exit 0
`)
				cmd := exec.Command("sh", "-c", acceptSameBridgeScript)
				// awk is the host one. The fakes in dir have to win over real nft and iptables.
				cmd.Env = []string{
					"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
					"GW=10.9.0.1",
					"LOG=" + logPath,
					"COUNT=" + countPath,
					"MODE=" + mode,
				}
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("script: %v\n%s%s", err, stderr.String(), stdout.String())
				}
				br, owned, err := parseBridgeOwner(stdout.String())
				if err != nil {
					t.Fatal(err)
				}
				if br != "br-test" {
					t.Fatalf("bridge %q", br)
				}
				if owned != (mode == "absent") {
					t.Fatalf("owned=%v, mode=%s\nstdout:\n%s", owned, mode, stdout.String())
				}
				logb, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(logb), "-I FORWARD 1 -i br-test -o br-test -j ACCEPT") {
					t.Fatalf("insert was not recorded:\n%s", logb)
				}
				if strings.Contains(string(logb), "delete table") {
					t.Fatalf("accept deleted the table:\n%s", logb)
				}
			})
		}
	})

	t.Run("marker", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HERMETARIUM_ROOT", root)
		if br, owned, err := parseBridgeOwner("br-abc\nowned\n"); err != nil || br != "br-abc" || !owned {
			t.Fatalf("parse owned: %q %v %v", br, owned, err)
		}
		if br, owned, err := parseBridgeOwner("br-abc\n"); err != nil || br != "br-abc" || owned {
			t.Fatalf("parse plain: %q %v %v", br, owned, err)
		}
		if _, _, err := parseBridgeOwner("br abc\n"); err == nil {
			t.Fatal("accepted a bridge name with a space")
		}

		var runs [][]string
		forgets := 0
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			runs = append(runs, append([]string(nil), args...))
			if len(args) == 0 {
				return "", os.ErrInvalid
			}
			if args[0] == "network" {
				return "10.9.0.1|10.9.0.0/24\n", nil
			}
			script := args[len(args)-1]
			switch script {
			case acceptSameBridgeScript:
				return "br-abc\nowned\n", nil
			case forgetSameBridgeScript:
				forgets++
				// First destroy sees an empty table. The next still has a rule.
				if forgets == 1 {
					return "dropped\n", nil
				}
				return "", nil
			default:
				return "", os.ErrInvalid
			}
		})
		br, err := acceptSameBridge("htm-x")
		if err != nil || br != "br-abc" {
			t.Fatalf("accept: %v %q", err, br)
		}
		if !hasFilterMarker(root) {
			t.Fatal("owned marker was not written")
		}
		if err := forgetSameBridge("br-abc"); err != nil {
			t.Fatal(err)
		}
		if hasFilterMarker(root) {
			t.Fatal("marker remains after dropped")
		}
		if err := rememberFilterTableOwned(); err != nil {
			t.Fatal(err)
		}
		if err := forgetSameBridge("br-abc"); err != nil {
			t.Fatal(err)
		}
		if !hasFilterMarker(root) {
			t.Fatal("marker cleared while a rule remained")
		}
		ownedRuns := 0
		plainRuns := 0
		for _, args := range runs {
			if len(args) == 0 || args[0] != "run" {
				continue
			}
			script := args[len(args)-1]
			if script != forgetSameBridgeScript {
				continue
			}
			if !strings.Contains(script, "nft delete table ip filter") {
				t.Fatal("forget script does not delete table ip filter")
			}
			owned := false
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-e" && args[i+1] == "OWNED=1" {
					owned = true
				}
			}
			if owned {
				ownedRuns++
			} else {
				plainRuns++
			}
		}
		if ownedRuns != 2 || plainRuns != 0 {
			t.Fatalf("owned forget runs=%d plain=%d", ownedRuns, plainRuns)
		}

		runs = nil
		if err := clearFilterTableOwned(root); err != nil {
			t.Fatal(err)
		}
		if err := forgetSameBridge("br-abc"); err != nil {
			t.Fatal(err)
		}
		for _, args := range runs {
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-e" && args[i+1] == "OWNED=1" {
					t.Fatal("pre-existing table passed OWNED=1")
				}
			}
		}
	})

	t.Run("delete already gone", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "nft.log")
		bin := t.TempDir()
		// Existence list and the empty -j list succeed. Delete then loses
		// the table; the next list is "No such file or directory".
		writeExec(t, filepath.Join(bin, "nft"), `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
n=0
if [ -f "$COUNT" ]; then
  IFS= read -r n < "$COUNT" || n=0
fi
n=$((n + 1))
printf '%s\n' "$n" > "$COUNT"
if [ "$1" = "delete" ]; then
  echo "Error: Could not process rule: No such file or directory" >&2
  exit 1
fi
if [ "$1" = "-j" ] && [ "$n" -gt 2 ]; then
  echo "Error: No such file or directory" >&2
  exit 1
fi
if [ "$1" = "-j" ]; then
  printf '%s\n' '{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}}]}'
fi
exit 0
`)
		stdout, err := runOwnedFilterScript(t, bin, logPath, "")
		if err != nil {
			t.Fatalf("script: %v\n%s", err, stdout)
		}
		if !scriptDroppedFilterTable(stdout) {
			t.Fatalf("stdout:\n%s", stdout)
		}
		logb, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(logb), "delete table ip filter") != 1 {
			t.Fatalf("log:\n%s", logb)
		}
	})

	// Artificial. Real nft delete table succeeds when the table is not empty,
	// so this re-list does not run there. The fake returns EBUSY.
	t.Run("delete loses to content", func(t *testing.T) {
		for _, mode := range []string{"rule", "set"} {
			t.Run(mode, func(t *testing.T) {
				logPath := filepath.Join(t.TempDir(), "nft.log")
				bin := t.TempDir()
				body := `{"nftables":[{"rule":{"chain":"FORWARD"}}]}`
				if mode == "set" {
					body = `{"nftables":[{"set":{"name":"peers"}}]}`
				}
				writeExec(t, filepath.Join(bin, "nft"), `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
n=0
if [ -f "$COUNT" ]; then
  IFS= read -r n < "$COUNT" || n=0
fi
n=$((n + 1))
printf '%s\n' "$n" > "$COUNT"
if [ "$1" = "delete" ]; then
  echo "Error: Device or resource busy" >&2
  exit 1
fi
if [ "$1" = "-j" ] && [ "$n" -gt 2 ]; then
  printf '%s\n' "$BODY"
  exit 0
fi
if [ "$1" = "-j" ]; then
  printf '%s\n' '{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}}]}'
fi
exit 0
`)
				stdout, err := runOwnedFilterScript(t, bin, logPath, "BODY="+body)
				if err != nil {
					t.Fatalf("script: %v\n%s", err, stdout)
				}
				if scriptDroppedFilterTable(stdout) {
					t.Fatalf("dropped a table that gained %s\n%s", mode, stdout)
				}
			})
		}
	})

	t.Run("overlap", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HERMETARIUM_ROOT", root)
		if err := rememberFilterTableOwned(); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		// The table is already gone. Both destroys must succeed and the
		// marker must not stay set. The flock keeps their scripts apart.
		writeExec(t, filepath.Join(bin, "nft"), `#!/bin/sh
echo "Error: No such file or directory" >&2
exit 1
`)
		var mu sync.Mutex
		inDocker := 0
		maxIn := 0
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) == 0 || args[0] != "run" {
				return "", os.ErrInvalid
			}
			mu.Lock()
			inDocker++
			if inDocker > maxIn {
				maxIn = inDocker
			}
			mu.Unlock()
			time.Sleep(150 * time.Millisecond)
			stdout, err := runFilterDocker(bin, args)
			mu.Lock()
			inDocker--
			mu.Unlock()
			return stdout, err
		})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = forgetSameBridge("br-overlap")
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("destroy %d: %v", i, err)
			}
		}
		if maxIn != 1 {
			t.Fatalf("overlapping nft runs = %d, want 1", maxIn)
		}
		if hasFilterMarker(root) {
			t.Fatal("marker remains after the table is gone")
		}
	})

	t.Run("marker write fails", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HERMETARIUM_ROOT", root)
		if err := os.MkdirAll(filterOwnedPath(root), 0o755); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "nft.log")
		writeExec(t, filepath.Join(bin, "nft"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+logPath+"'\nprintf '%s\\n' '{\"nftables\":[{\"rule\":{\"chain\":\"FORWARD\"}}]}'\nexit 0\n")
		var owned bool
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) == 0 {
				return "", os.ErrInvalid
			}
			if args[0] == "network" {
				return "10.9.0.1|10.9.0.0/24\n", nil
			}
			script := args[len(args)-1]
			if script == acceptSameBridgeScript {
				return "br-abc\nowned\n", nil
			}
			if script != forgetSameBridgeScript {
				return "", os.ErrInvalid
			}
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-e" && args[i+1] == "OWNED=1" {
					owned = true
				}
			}
			return runFilterDocker(bin, args)
		})
		if _, err := acceptSameBridge("htm-x"); err == nil {
			t.Fatal("expected marker write to fail")
		}
		if !owned {
			t.Fatal("rollback did not pass OWNED=1")
		}
		logb, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(logb), "delete table") {
			t.Fatalf("deleted a table that still has a rule:\n%s", logb)
		}
		fi, err := os.Stat(filterOwnedPath(root))
		if err != nil || !fi.IsDir() {
			t.Fatalf("marker path: %v", err)
		}
	})

	t.Run("replaced marker stays", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HERMETARIUM_ROOT", root)
		if err := rememberFilterTableOwned(); err != nil {
			t.Fatal(err)
		}
		stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
			if len(args) == 0 || args[0] != "run" {
				return "", os.ErrInvalid
			}
			// Same inode if the file is replaced in place. The token still changes.
			if err := os.WriteFile(filterOwnedPath(root), []byte("replaced\n"), 0o644); err != nil {
				return "", err
			}
			return "dropped\n", nil
		})
		if err := forgetSameBridge("br-abc"); err != nil {
			t.Fatal(err)
		}
		if !hasFilterMarker(root) {
			t.Fatal("replaced marker was removed")
		}
		now, err := readIPFilterMarker(root)
		if err != nil {
			t.Fatal(err)
		}
		if now != "replaced" {
			t.Fatalf("marker %q", now)
		}
	})
}

func TestNftFilterTableNetns(t *testing.T) {
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
	script := `set -eu
echo phase-empty
gw=$(ip -o -4 addr show scope global | awk 'NR==1 { print $4 }')
gw=${gw%%/*}
test -n "$gw"
out=$(GW="$gw" sh -c "$ACCEPT")
br=$(printf '%s\n' "$out" | head -n 1)
test -n "$br"
printf '%s\n' "$out" | grep -qx owned
BR="$br" OWNED=1 sh -c "$FORGET"
if nft list table ip filter >/dev/null 2>&1; then
  echo "empty table remains" >&2
  exit 1
fi

echo phase-preexisting
nft add table ip filter
out=$(GW="$gw" sh -c "$ACCEPT")
printf '%s\n' "$out" | grep -qx owned && { echo "pre-existing table marked owned" >&2; exit 1; }
BR="$br" sh -c "$FORGET"
nft list table ip filter >/dev/null

echo phase-rule
nft add rule ip filter FORWARD ip saddr 192.0.2.1 accept
iptables-nft -w -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1 || iptables-nft -w -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT
BR="$br" OWNED=1 sh -c "$FORGET"
nft list table ip filter >/dev/null
nft -j list table ip filter | grep -q '"rule"'
if iptables-nft -w -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1; then
  echo "bridge rule remains" >&2
  exit 1
fi
nft delete table ip filter

echo phase-set
nft add table ip filter
nft add chain ip filter FORWARD '{ type filter hook forward priority 0; policy accept; }'
nft add set ip filter peers '{ type ipv4_addr; }'
iptables-nft -w -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT
BR="$br" OWNED=1 sh -c "$FORGET"
nft list set ip filter peers >/dev/null
nft delete table ip filter

echo phase-policy
out=$(GW="$gw" sh -c "$ACCEPT")
printf '%s\n' "$out" | grep -qx owned
iptables-nft -w -P FORWARD DROP
BR="$br" OWNED=1 sh -c "$FORGET"
nft list table ip filter >/dev/null
nft -j list table ip filter | grep -q '"policy": "drop"'
if iptables-nft -w -C FORWARD -i "$br" -o "$br" -j ACCEPT >/dev/null 2>&1; then
  echo "bridge rule remains beside drop policy" >&2
  exit 1
fi
nft delete table ip filter

echo phase-user
nft add table ip filter
nft add chain ip filter FORWARD '{ type filter hook forward priority 0; policy accept; }'
nft add chain ip filter mine
iptables-nft -w -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT
BR="$br" OWNED=1 sh -c "$FORGET"
nft list chain ip filter mine >/dev/null
nft delete table ip filter

echo phase-objects
nft add table ip filter
nft add chain ip filter FORWARD '{ type filter hook forward priority 0; policy accept; }'
nft add quota ip filter q 25 mbytes
nft add limit ip filter lim rate 3/second
nft add synproxy ip filter sp mss 1460 wscale 7 timestamp sack-perm
iptables-nft -w -I FORWARD 1 -i "$br" -o "$br" -j ACCEPT
BR="$br" OWNED=1 sh -c "$FORGET"
nft list quota ip filter q >/dev/null
nft list limit ip filter lim >/dev/null
nft list synproxy ip filter sp >/dev/null
nft delete table ip filter

echo phase-builtins
out=$(GW="$gw" sh -c "$ACCEPT")
printf '%s\n' "$out" | grep -qx owned
iptables-nft -w -P INPUT ACCEPT
iptables-nft -w -P OUTPUT ACCEPT
BR="$br" OWNED=1 sh -c "$FORGET"
if nft list table ip filter >/dev/null 2>&1; then
  echo "accept chains remain" >&2
  exit 1
fi
`
	out, err := Docker(2*time.Minute, "run", "--rm", "--cap-add", "NET_ADMIN",
		"-e", "ACCEPT="+acceptSameBridgeScript,
		"-e", "FORGET="+forgetSameBridgeScript,
		"--entrypoint", "sh", NetToolsImage, "-c", script)
	if err != nil {
		t.Fatalf("netns: %v\n%s", err, out)
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func hasFilterMarker(root string) bool {
	_, err := readIPFilterMarker(root)
	return err == nil
}

func TestNftFilterRulesBeforeTable(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	bin := t.TempDir()
	ipt := `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
name=${0##*/}
n=0
if [ -f "$COUNT.$name" ]; then
  IFS= read -r n < "$COUNT.$name" || n=0
fi
n=$((n + 1))
printf '%s\n' "$n" > "$COUNT.$name"
if [ "$2" = "-C" ]; then
  if [ "$n" -eq 1 ]; then
    exit 0
  fi
  exit 1
fi
exit 0
`
	writeExec(t, filepath.Join(bin, "iptables-legacy"), ipt)
	writeExec(t, filepath.Join(bin, "iptables-nft"), ipt)
	writeExec(t, filepath.Join(bin, "nft"), `#!/bin/sh
printf '%s\n' "$*" >> "$LOG"
if [ "$1" = "-j" ]; then
  printf '%s\n' '{"nftables":[{"chain":{"name":"FORWARD","policy":"accept"}}]}'
fi
exit 0
`)
	cmd := exec.Command("sh", "-c", forgetSameBridgeScript)
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"LOG=" + logPath,
		"COUNT=" + logPath + ".n",
		"BR=br-test",
		"OWNED=1",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("script: %v\n%s%s", err, stderr.String(), stdout.String())
	}
	logb, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(logb), "\n")
	lastRule := -1
	deleteAt := -1
	deletes := 0
	rules := 0
	for i, line := range lines {
		if strings.Contains(line, "-D FORWARD") {
			rules++
			lastRule = i
		}
		if strings.TrimSpace(line) == "delete table ip filter" {
			deletes++
			deleteAt = i
		}
	}
	// Both backends drop their FORWARD accept before the table goes.
	if deletes != 1 || rules != 2 || deleteAt < lastRule {
		t.Fatalf("table delete must follow the FORWARD deletes\nlog:\n%s", logb)
	}
	if !scriptDroppedFilterTable(stdout.String()) {
		t.Fatalf("stdout:\n%s", stdout.String())
	}
}

func runOwnedFilterScript(t *testing.T, bin, logPath, extra string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", dropOwnedFilterTableScript)
	// awk parses the listing. nft in bin has to win over a real nft.
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"LOG=" + logPath,
		"OWNED=1",
		"COUNT=" + logPath + ".n",
	}
	if extra != "" {
		cmd.Env = append(cmd.Env, extra)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stderr.String() + stdout.String(), err
	}
	return stdout.String(), nil
}

func runFilterDocker(bin string, args []string) (string, error) {
	script := args[len(args)-1]
	cmd := exec.Command("sh", "-c", script)
	// bin only: a host iptables would edit the real filter table.
	cmd.Env = []string{"PATH=" + bin}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-e" {
			cmd.Env = append(cmd.Env, args[i+1])
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}
