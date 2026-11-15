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
	if rec.Destination != ProbeHost {
		t.Fatalf("dest %q", rec.Destination)
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
	if !ok || rec.Direction != "in" || rec.Method != "POST" || rec.Bytes != 4 {
		t.Fatalf("inbound %+v ok=%v", rec, ok)
	}
	httpsLine := "1757162698.123     20 10.20.0.3 TCP_MISS/200 100 GET https://deb.debian.org/debian/dists/bookworm/InRelease - HIER_DIRECT/1.2.3.4 text/plain"
	rec, ok = ParseSquidLine(httpsLine)
	if !ok || rec.Protocol != "https" || rec.Destination != "deb.debian.org" || rec.Port != 443 || rec.Path != "/debian/dists/bookworm/InRelease" || !rec.Allowed {
		t.Fatalf("https %+v ok=%v", rec, ok)
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
