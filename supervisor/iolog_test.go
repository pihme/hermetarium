package supervisor

import "testing"

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
	if !ok || rec.Protocol != "https" || rec.Destination != "deb.debian.org" || rec.Port != 443 || rec.Path != "/debian/dists/bookworm/InRelease" || !rec.Allowed || rec.TLS != "" {
		t.Fatalf("https %+v ok=%v", rec, ok)
	}
	bumped := "1757162698.123 5 10.20.0.3 TCP_DENIED/403 100 PUT https://api.github.com/repos/o/r/contents/README.md - HIER_NONE/- text/html 3130 bump api.github.com"
	rec, ok = ParseSquidLine(bumped)
	if !ok || rec.Allowed || rec.TLS != "decrypted" || rec.Destination != "api.github.com" || rec.Method != "PUT" || rec.Path != "/repos/o/r/contents/README.md" {
		t.Fatalf("bump %+v ok=%v", rec, ok)
	}
	spliced := "1757162698.123 5 10.20.0.3 TCP_TUNNEL/200 0 CONNECT pinned.example:443 - HIER_DIRECT/10.1.0.2 - 3130 splice pinned.example"
	rec, ok = ParseSquidLine(spliced)
	if !ok || !rec.Allowed || rec.TLS != "passthrough" || rec.Destination != "pinned.example" {
		t.Fatalf("splice %+v ok=%v", rec, ok)
	}
}
