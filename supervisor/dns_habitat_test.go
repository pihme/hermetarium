package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateDNS(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-dns:local"
	ctx := t.TempDir()
	// One executable. The strong wall runs the image command with a shell
	// split, so a sh -c script would lose every word after the first.
	script := "#!/bin/sh\n" +
		"ns=$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf)\n" +
		"echo DNS_NS=$ns\n" +
		"echo DNS_EXAMPLE=$(getent hosts example.com | awk '{print $1; exit}')\n" +
		"echo DNS_PROBE=$(nslookup probe.hermetarium.test 2>/dev/null | awk 'BEGIN{p=0} /^$/{p=1; next} p && /Address/ {print $NF; exit}')\n" +
		"echo DNS_LOCAL=$(awk '/^127\\.0\\.0\\.1[[:space:]]/ {print $1; exit}' /etc/hosts)\n" +
		"if timeout 3 nslookup example.com 1.1.1.1 >/dev/null 2>&1; then echo DNS_DIRECT=OK; else echo DNS_DIRECT=FAIL; fi\n" +
		"ans=$(timeout 3 nslookup example.com 127.0.0.11 2>/dev/null | awk 'BEGIN{p=0} /^$/{p=1; next} p && /Address/ {print $NF; exit}')\n" +
		"if [ -n \"$ans\" ]; then echo DNS_DOCKER=OK; else echo DNS_DOCKER=FAIL; fi\n" +
		"echo DNS_DONE\n" +
		"exec sleep 3600\n"
	if err := os.WriteFile(filepath.Join(ctx, "probe.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	df := "FROM " + AlpineImage + "\nCOPY probe.sh /probe.sh\nCMD [\"/probe.sh\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(5*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	base := "" +
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
		"cache deny all\n"
	closed := base + "http_access deny all\n"
	open := base + "acl web myportname intercept\nhttp_access allow web\nhttp_access deny all\n"

	for _, wall := range []string{"weak", "strong"} {
		for _, mode := range []string{"closed", "open"} {
			wall, mode := wall, mode
			t.Run(wall+"/"+mode, func(t *testing.T) {
				conf := closed
				if mode == "open" {
					conf = open
				}
				acl := filepath.Join(t.TempDir(), "squid.conf")
				if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
					t.Fatal(err)
				}
				id := newTestID(t)
				opts := CreateOpts{Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128}
				var dir, gate string
				switch wall {
				case "weak":
					t.Cleanup(func() { _ = DestroyWeak(id) })
					inst, err := CreateWeak(root, id, WithTestLabel(t, opts))
					if err != nil {
						t.Fatal(err)
					}
					dir, gate = inst.Dir, inst.GateIP
				default:
					t.Cleanup(func() { _ = DestroyStrong(id) })
					inst, err := CreateStrong(root, id, WithTestLabel(t, opts))
					if err != nil {
						t.Fatal(err)
					}
					dir, gate = inst.Dir, strongGateIP
				}
				logs := waitMarker(t, wall, id, "DNS_DONE")
				ns := markerValue(logs, "DNS_NS")
				if ns == "" || ns == "127.0.0.11" {
					t.Fatalf("nameserver %q\n%s", ns, logs)
				}
				if markerValue(logs, "DNS_DIRECT") != "FAIL" {
					t.Fatalf("query to 1.1.1.1 was answered\n%s", logs)
				}
				// 127.0.0.11 is Docker's resolver. The strong wall has none.
				if wall == "weak" && markerValue(logs, "DNS_DOCKER") != "FAIL" {
					t.Fatalf("127.0.0.11 resolved an outside name\n%s", logs)
				}
				if !strings.Contains(markerValue(logs, "DNS_LOCAL"), "127.0.0.1") {
					t.Fatalf("localhost\n%s", logs)
				}
				if markerValue(logs, "DNS_PROBE") != gate {
					t.Fatalf("stand-in did not resolve to %s\n%s", gate, logs)
				}
				if err := SyncInstanceLog(dir); err != nil {
					t.Fatal(err)
				}
				recs, err := ReadIoLog(filepath.Join(dir, "io.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				probe, ok := findDNS(recs, ProbeHost, "A")
				if !ok || !probe.Allowed || probe.Result != gate {
					t.Fatalf("probe log %+v\n%s", probe, readDNS(dir))
				}
				ex := dnsFor(recs, "example.com")
				if len(ex) == 0 {
					t.Fatalf("example.com missing\n%s\n%s", readDNS(dir), logs)
				}
				if mode == "closed" {
					for _, r := range ex {
						if r.Allowed || r.Result != "NXDOMAIN" || markerValue(logs, "DNS_EXAMPLE") != "" {
							t.Fatalf("closed example %+v\n%s", ex, logs)
						}
					}
					return
				}
				answered := false
				for _, r := range ex {
					if r.Allowed && r.Result != "NXDOMAIN" && r.Result != "NODATA" && r.Result != "SERVFAIL" {
						answered = true
					}
				}
				if !answered || markerValue(logs, "DNS_EXAMPLE") == "" {
					t.Fatalf("open example %+v\n%s\n%s", ex, logs, readDNS(dir))
				}
			})
		}
	}
}

func markerValue(logs, key string) string {
	p := key + "="
	// The strong wall echoes the guest command. The probe output is last.
	i := strings.LastIndex(logs, p)
	if i < 0 {
		return ""
	}
	rest := logs[i+len(p):]
	if n := strings.IndexAny(rest, "\r\n"); n >= 0 {
		rest = rest[:n]
	}
	return strings.TrimSpace(rest)
}

func waitMarker(t *testing.T, wall, id, marker string) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		if wall == "weak" {
			logs, _ = Docker(10*time.Second, "logs", "htm-box-"+id)
		} else {
			logs = containerLogs("htm-fc-" + id)
		}
		if strings.Contains(logs, marker) {
			return logs
		}
		name := "htm-box-" + id
		if wall != "weak" {
			name = "htm-fc-" + id
		}
		if !containerRunning(name) && wall != "weak" {
			t.Fatalf("Firecracker container exited\n%s", logs)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("missing %s\n%s", marker, logs)
	return ""
}

func dnsFor(recs []Record, dest string) []Record {
	var out []Record
	for _, r := range recs {
		if r.Protocol == "dns" && r.Destination == dest {
			out = append(out, r)
		}
	}
	return out
}

func findDNS(recs []Record, dest, method string) (Record, bool) {
	for _, r := range recs {
		if r.Protocol == "dns" && r.Destination == dest && r.Method == method {
			return r, true
		}
	}
	return Record{}, false
}

func readDNS(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "dns.jsonl"))
	return string(b)
}
