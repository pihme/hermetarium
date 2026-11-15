package main

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
)

func TestStandInAnswersAndClosedRefuses(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "dns")
	if err != nil {
		t.Fatal(err)
	}
	g := &gateDNS{
		open:  false,
		gate:  net.ParseIP("172.16.0.1").To4(),
		names: map[string]struct{}{"probe.hermetarium.test": {}},
		log:   f,
	}
	resp := g.answer(dnsQuery("probe.hermetarium.test", 1))
	if len(resp) < 16 || resp[3]&0x0F != 0 {
		t.Fatalf("stand-in rcode/len %d %d", resp[3], len(resp))
	}
	if got := net.IP(resp[len(resp)-4:]).String(); got != "172.16.0.1" {
		t.Fatalf("stand-in A %s", got)
	}
	resp = g.answer(dnsQuery("example.com", 1))
	if len(resp) < 12 || resp[3]&0x0F != 3 {
		t.Fatalf("closed rcode %v", resp)
	}
	resp = g.answer(dnsQuery("Example.COM", 28))
	if resp[3]&0x0F != 3 {
		t.Fatalf("closed AAAA rcode %d", resp[3]&0x0F)
	}
}

func TestForwardedResult(t *testing.T) {
	q := dnsQuery("example.com", 1)
	_, _, qend, ok := parseQuestion(q)
	if !ok {
		t.Fatal("question")
	}
	rr := rrA(net.ParseIP("93.184.216.34"))
	resp := localResp(q, qend, 0, false, rr)
	if got := resultOf(resp); !strings.Contains(got, "93.184.216.34") {
		t.Fatalf("result %q", got)
	}
}

func TestMalformedPacket(t *testing.T) {
	over := nameWithLastLabel(62)
	if dnsWireLen(over) != 256 {
		t.Fatalf("overlong wire length %d", dnsWireLen(over))
	}
	valid := dnsQuery("example.com", 1)
	response := append([]byte(nil), valid...)
	response[2] |= 0x80
	opcode := append([]byte(nil), valid...)
	opcode[2] |= 0x08
	loop := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0xC0, 0x0C, 0, 1, 0, 1}
	short := valid[:20]
	pkts := []struct {
		name string
		pkt  []byte
	}{
		{"truncated-header", []byte{0x12, 0x34, 0x01, 0x00, 0, 1}},
		{"truncated-body", short},
		{"truncated-label", []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 5, 'a', 'b'}},
		{"truncated-pointer", []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0xC0}},
		{"pointer-past-end", []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0xC0, 0x40, 0, 1, 0, 1}},
		{"reserved-prefix", []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0x80, 0, 1, 0, 1}},
		{"response", response},
		{"opcode", opcode},
		{"pointer-loop", loop},
		{"name-over-255", dnsQuery(over, 1)},
	}
	for _, open := range []bool{false, true} {
		state := "closed"
		if open {
			state = "open"
		}
		for _, tc := range pkts {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				assertNoOutsideAnswer(t, open, tc.pkt)
			})
		}
	}
}

func TestLongLabel(t *testing.T) {
	q := dnsQuery(strings.Repeat("a", 64), 1)
	for _, open := range []bool{false, true} {
		assertNoOutsideAnswer(t, open, q)
	}
}

func TestLongNamesAnswerAndLog(t *testing.T) {
	cases := []struct {
		name string
		q    string
	}{
		{"9-labels", nameWithLabels(9)},
		{"10-labels", nameWithLabels(10)},
		{"11-labels", nameWithLabels(11)},
		{"255-octets", nameWithLastLabel(61)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "255-octets" && dnsWireLen(tc.q) != 255 {
				t.Fatalf("fixture wire length %d, want 255", dnsWireLen(tc.q))
			}
			q := dnsQuery(tc.q, 1)
			parsed, _, qend, ok := parseQuestion(q)
			if !ok || parsed != tc.q || qend != len(q) {
				t.Fatalf("parse %q ok=%v end %d len %d", parsed, ok, qend, len(q))
			}
			assertLogged(t, false, tc.q, "NXDOMAIN", false)
			assertLogged(t, true, tc.q, "SERVFAIL", true)
		})
	}
}

func TestLongStandInStillLocal(t *testing.T) {
	name := nameWithLabels(10)
	f := newLog(t)
	g := &gateDNS{
		open:  false,
		gate:  net.ParseIP("172.16.0.1").To4(),
		names: map[string]struct{}{name: {}},
		log:   f,
	}
	resp := g.answer(dnsQuery(name, 1))
	if len(resp) < 16 || resp[3]&0x0F != 0 {
		t.Fatalf("stand-in len %d", len(resp))
	}
	if got := net.IP(resp[len(resp)-4:]).String(); got != "172.16.0.1" {
		t.Fatalf("stand-in A %s", got)
	}
	rec := oneRecord(t, f)
	if rec.Destination != name || rec.Result != "172.16.0.1" || !rec.Allowed {
		t.Fatalf("rec %+v", rec)
	}
}

func assertNoOutsideAnswer(t *testing.T, open bool, q []byte) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dns")
	if err != nil {
		t.Fatal(err)
	}
	g := &gateDNS{
		open:  open,
		gate:  net.ParseIP("172.16.0.1").To4(),
		names: map[string]struct{}{"probe.hermetarium.test": {}},
		up:    []string{"127.0.0.1"},
		log:   f,
	}
	resp := g.answer(q)
	if len(resp) != 0 {
		t.Fatalf("open %v response rcode %d len %d", open, resp[3]&0x0F, len(resp))
	}
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		b, _ := os.ReadFile(f.Name())
		t.Fatalf("logged an answer: %s", b)
	}
}

func assertLogged(t *testing.T, open bool, name, result string, allowed bool) {
	t.Helper()
	f := newLog(t)
	g := &gateDNS{
		open:  open,
		gate:  net.ParseIP("172.16.0.1").To4(),
		names: map[string]struct{}{"probe.hermetarium.test": {}},
		log:   f,
	}
	resp := g.answer(dnsQuery(name, 1))
	if len(resp) < 12 {
		t.Fatalf("open %v no response", open)
	}
	want := byte(3)
	if result == "SERVFAIL" {
		want = 2
	}
	if resp[3]&0x0F != want {
		t.Fatalf("open %v rcode %d, want %d", open, resp[3]&0x0F, want)
	}
	rec := oneRecord(t, f)
	if rec.Destination != name || rec.Result != result || rec.Allowed != allowed || rec.Method != "A" {
		t.Fatalf("rec %+v", rec)
	}
}

func newLog(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dns")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func oneRecord(t *testing.T, f *os.File) dnsRec {
	t.Helper()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSuffix(string(b), "\n")
	lines := strings.Split(text, "\n")
	if text == "" || len(lines) != 1 {
		t.Fatalf("log lines: %q", b)
	}
	var rec dnsRec
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func nameWithLabels(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = string(rune('a' + i))
	}
	return strings.Join(parts, ".")
}

func nameWithLastLabel(n int) string {
	return strings.Join([]string{
		strings.Repeat("a", 63),
		strings.Repeat("b", 63),
		strings.Repeat("c", 63),
		strings.Repeat("d", n),
	}, ".")
}

func dnsWireLen(name string) int {
	n := 1
	for _, label := range strings.Split(name, ".") {
		n += 1 + len(label)
	}
	return n
}

func dnsQuery(name string, typ uint16) []byte {
	var b []byte
	b = append(b, 0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0)
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0)
	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], typ)
	binary.BigEndian.PutUint16(tail[2:4], 1)
	return append(b, tail[:]...)
}
