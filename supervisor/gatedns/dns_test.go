package main

import (
	"encoding/binary"
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
		{"response", response},
		{"opcode", opcode},
		{"pointer-loop", loop},
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
