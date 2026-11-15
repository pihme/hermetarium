package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSquidLine(t *testing.T) {
	line := "1757162698.123     12 10.20.0.3 TCP_MISS/200 45 GET http://probe.hermetarium.test/hello - HIER_DIRECT/127.0.0.1 text/plain"
	rec, ok := ParseSquidLine(line)
	if !ok {
		t.Fatal("parse failed")
	}
	if rec.Destination != ProbeHost || rec.Protocol != "http" || rec.Port != 80 || rec.Upstream != "" {
		t.Fatalf("dest %q %+v", rec.Destination, rec)
	}
	if !rec.Allowed || rec.Method != "GET" || rec.Bytes != 45 {
		t.Fatalf("%+v", rec)
	}
	denied := "1757162698.000 0 10.20.0.3 TCP_DENIED/403 12 GET http://example.com/ - HIER_NONE/- text/html"
	rec, ok = ParseSquidLine(denied)
	if !ok || rec.Allowed || rec.Destination != "example.com" {
		t.Fatalf("denied %+v ok=%v", rec, ok)
	}
	in := "1757162698.123     4 172.17.0.1 TCP_MISS/200 4 POST http://127.0.0.1:18081/ - HIER_DIRECT/10.20.0.3 text/plain 18081"
	rec, ok = ParseSquidLine(in)
	if !ok || rec.Direction != "in" || rec.Method != "POST" || rec.Bytes != 4 || rec.Protocol != "http" || rec.Port != 18081 || rec.Upstream != "" {
		t.Fatalf("inbound %+v ok=%v", rec, ok)
	}
	httpsLine := "1757162698.123     20 10.20.0.3 TCP_MISS/200 100 GET https://deb.debian.org/debian/dists/bookworm/InRelease - HIER_DIRECT/1.2.3.4 text/plain"
	rec, ok = ParseSquidLine(httpsLine)
	if !ok || rec.Protocol != "https" || rec.Destination != "deb.debian.org" || rec.Port != 443 || rec.Path != "/debian/dists/bookworm/InRelease" || !rec.Allowed || rec.TLS != "" || rec.Upstream != "" {
		t.Fatalf("https %+v ok=%v", rec, ok)
	}
	bumped := "1757162698.123 5 10.20.0.3 TCP_DENIED/403 100 PUT https://api.github.com/repos/o/r/contents/README.md - HIER_NONE/- text/html 3130 bump api.github.com"
	rec, ok = ParseSquidLine(bumped)
	if !ok || rec.Allowed || rec.TLS != "decrypted" || rec.Destination != "api.github.com" || rec.Method != "PUT" || rec.Path != "/repos/o/r/contents/README.md" || rec.Protocol != "https" || rec.Port != 443 || rec.Upstream != "" {
		t.Fatalf("bump %+v ok=%v", rec, ok)
	}
	// Local port 3130 is where Squid listened. The authority carries 443.
	spliced := "1757162698.123 5 10.20.0.3 TCP_TUNNEL/200 0 CONNECT pinned.example:443 - HIER_DIRECT/10.1.0.2 - 3130 splice pinned.example"
	rec, ok = ParseSquidLine(spliced)
	if !ok || !rec.Allowed || rec.TLS != "passthrough" || rec.Destination != "pinned.example" || rec.Protocol != "https" || rec.Port != 443 || rec.Upstream != "10.1.0.2" || rec.Method != "CONNECT" {
		t.Fatalf("splice %+v ok=%v", rec, ok)
	}
	// Open-web access.log. Intercepted CONNECT is an IP authority; the name is SNI.
	bumpedConnect := "1794765518.066     10 10.60.0.3 NONE_NONE/000 0 CONNECT 10.60.0.2:443 - ORIGINAL_DST/10.60.0.2 - 443 bump api.github.com"
	rec, ok = ParseSquidLine(bumpedConnect)
	if !ok || rec.Protocol != "https" || rec.Port != 443 || rec.Destination != "api.github.com" || rec.Upstream != "10.60.0.2" || rec.TLS != "decrypted" || rec.Method != "CONNECT" || rec.Path != "" {
		t.Fatalf("bump connect %+v ok=%v", rec, ok)
	}
	splicedIP := "1794765518.067      0 10.60.0.3 NONE_NONE/000 0 CONNECT 10.60.0.2:443 - HIER_NONE/- - 443 splice pinned.example"
	rec, ok = ParseSquidLine(splicedIP)
	if !ok || rec.Protocol != "https" || rec.Port != 443 || rec.Destination != "pinned.example" || rec.Upstream != "10.60.0.2" || rec.TLS != "passthrough" || rec.Method != "CONNECT" {
		t.Fatalf("splice ip %+v ok=%v", rec, ok)
	}
	splicedTunnel := "1794765518.069      2 10.60.0.3 TCP_TUNNEL/200 3138 CONNECT pinned.example:443 - ORIGINAL_DST/10.60.0.2 - 443 splice pinned.example"
	rec, ok = ParseSquidLine(splicedTunnel)
	if !ok || !rec.Allowed || rec.Protocol != "https" || rec.Port != 443 || rec.Destination != "pinned.example" || rec.Upstream != "10.60.0.2" || rec.TLS != "passthrough" || rec.Bytes != 3138 {
		t.Fatalf("splice tunnel %+v ok=%v", rec, ok)
	}
	dir := t.TempDir()
	access := filepath.Join(dir, "access.log")
	if err := os.WriteFile(access, []byte(bumpedConnect+"\n"+splicedIP+"\n"+splicedTunnel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, "io.jsonl")
	if err := SyncAccessLog(access, jsonl); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadIoLog(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].Upstream != "10.60.0.2" || recs[0].Destination != "api.github.com" || recs[0].Protocol != "https" || recs[0].Port != 443 || recs[1].Upstream != "10.60.0.2" || recs[1].Destination != "pinned.example" || recs[2].Upstream != "10.60.0.2" {
		t.Fatalf("decoded %+v", recs)
	}
}

func TestSyncMergesDNS(t *testing.T) {
	dir := t.TempDir()
	access := filepath.Join(dir, "access.log")
	if err := os.WriteFile(access, []byte("1757162698.123     12 10.20.0.3 TCP_MISS/200 45 GET http://probe.hermetarium.test/hello - HIER_DIRECT/127.0.0.1 text/plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dns := `{"time":"2026-01-01T00:00:00Z","direction":"out","protocol":"dns","destination":"example.com","port":53,"method":"A","result":"NXDOMAIN","allowed":false}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "dns.jsonl"), []byte(dns), 0o644); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, "io.jsonl")
	if err := SyncAccessLog(access, jsonl); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadIoLog(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Protocol != "http" || recs[1].Protocol != "dns" || recs[1].Result != "NXDOMAIN" || recs[1].Destination != "example.com" || recs[1].Allowed {
		t.Fatalf("%+v", recs)
	}
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "dns.jsonl"), []byte(dns), 0o644); err != nil {
		t.Fatal(err)
	}
	jsonl = filepath.Join(dir2, "io.jsonl")
	if err := SyncAccessLog(filepath.Join(dir2, "access.log"), jsonl); err != nil {
		t.Fatal(err)
	}
	recs, err = ReadIoLog(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Protocol != "dns" || recs[0].Result != "NXDOMAIN" {
		t.Fatalf("missing access.log %+v", recs)
	}
}
