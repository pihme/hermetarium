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
}
