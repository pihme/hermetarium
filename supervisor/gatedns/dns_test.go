package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
	resp := g.answer(dnsQuery("probe.hermetarium.test", 1), false)
	if len(resp) < 16 || resp[3]&0x0F != 0 {
		t.Fatalf("stand-in rcode/len %d %d", resp[3], len(resp))
	}
	if got := net.IP(resp[len(resp)-4:]).String(); got != "172.16.0.1" {
		t.Fatalf("stand-in A %s", got)
	}
	resp = g.answer(dnsQuery("example.com", 1), false)
	if len(resp) < 12 || resp[3]&0x0F != 3 {
		t.Fatalf("closed rcode %v", resp)
	}
	resp = g.answer(dnsQuery("Example.COM", 28), false)
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
	resp := g.answer(dnsQuery(name, 1), false)
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
	resp := g.answer(q, false)
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
	resp := g.answer(dnsQuery(name, 1), false)
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

func TestForwardTCPAndTruncatedUDP(t *testing.T) {
	t.Run("tcp client", func(t *testing.T) {
		up := startFakeDNS(t, func(q []byte) []byte {
			return respA(q, "192.0.2.1", true)
		}, func(q []byte) []byte {
			return respPad(respA(q, "192.0.2.2", false), 700)
		})
		g := forwardGate(t, up.port)
		_, tcp := serveGate(t, g)
		resp := queryTCP(t, tcp.Addr().String(), dnsQuery("example.com", 1))
		if len(resp) < 700 || resultOf(resp) != "A 192.0.2.2" {
			t.Fatalf("tcp client len %d result %s", len(resp), resultOf(resp))
		}
		if u, c := up.counts(); u != 0 || c != 1 {
			t.Fatalf("transports udp=%d tcp=%d", u, c)
		}
	})

	t.Run("truncated udp", func(t *testing.T) {
		up := startFakeDNS(t, func(q []byte) []byte {
			return respA(q, "192.0.2.1", true)
		}, func(q []byte) []byte {
			return respA(q, "192.0.2.2", false)
		})
		g := forwardGate(t, up.port)
		udp, _ := serveGate(t, g)
		resp := queryUDP(t, udp.LocalAddr(), dnsQuery("example.com", 1))
		if resultOf(resp) != "A 192.0.2.2" || dnsTruncated(resp) {
			t.Fatalf("result %s tc %v", resultOf(resp), dnsTruncated(resp))
		}
		if u, c := up.counts(); u != 1 || c != 1 {
			t.Fatalf("transports udp=%d tcp=%d", u, c)
		}
	})

	t.Run("udp without tc", func(t *testing.T) {
		up := startFakeDNS(t, func(q []byte) []byte {
			return respA(q, "192.0.2.1", false)
		}, func(q []byte) []byte {
			return respA(q, "192.0.2.2", false)
		})
		g := forwardGate(t, up.port)
		udp, _ := serveGate(t, g)
		resp := queryUDP(t, udp.LocalAddr(), dnsQuery("example.com", 1))
		if resultOf(resp) != "A 192.0.2.1" {
			t.Fatalf("result %s", resultOf(resp))
		}
		if u, c := up.counts(); u != 1 || c != 0 {
			t.Fatalf("transports udp=%d tcp=%d", u, c)
		}
	})

	t.Run("truncated udp stays when tcp is large", func(t *testing.T) {
		up := startFakeDNS(t, func(q []byte) []byte {
			return respA(q, "192.0.2.1", true)
		}, func(q []byte) []byte {
			return respPad(respA(q, "192.0.2.9", false), 800)
		})
		g := forwardGate(t, up.port)
		udp, _ := serveGate(t, g)
		resp := queryUDP(t, udp.LocalAddr(), dnsQuery("example.com", 1))
		if resultOf(resp) != "A 192.0.2.1" || !dnsTruncated(resp) {
			t.Fatalf("result %s tc %v", resultOf(resp), dnsTruncated(resp))
		}
		if _, c := up.counts(); c != 1 {
			t.Fatalf("tcp forwards %d", c)
		}
	})
}

func TestUDPConcurrencyBound(t *testing.T) {
	const slots = 2
	const burst = 8
	upUDP, upTCP, port := listenBoth(t)
	var udpN, tcpN, inflight, maxSeen atomic.Int32
	hold := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(hold) }) }
	note := func(cur int32) {
		for {
			m := maxSeen.Load()
			if cur <= m || maxSeen.CompareAndSwap(m, cur) {
				return
			}
		}
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := upUDP.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			udpN.Add(1)
			note(inflight.Add(1))
			go func(addr net.Addr, q []byte) {
				<-hold
				inflight.Add(-1)
				_, _ = upUDP.WriteTo(respA(q, "192.0.2.1", false), addr)
			}(addr, q)
		}
	}()
	go func() {
		for {
			c, err := upTCP.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				var n uint16
				if err := binary.Read(c, binary.BigEndian, &n); err != nil || n > 65535 {
					return
				}
				q := make([]byte, n)
				if _, err := io.ReadFull(c, q); err != nil {
					return
				}
				tcpN.Add(1)
				note(inflight.Add(1))
				<-hold
				inflight.Add(-1)
				resp := respA(q, "192.0.2.2", false)
				_ = binary.Write(c, binary.BigEndian, uint16(len(resp)))
				_, _ = c.Write(resp)
			}(c)
		}
	}()

	g := forwardGate(t, port)
	g.slots = make(chan struct{}, slots)
	gateUDP, gateTCP := serveGate(t, g)
	t.Cleanup(func() {
		letGo()
		upUDP.Close()
		upTCP.Close()
	})

	client, err := net.DialUDP("udp4", nil, gateUDP.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	q := dnsQuery("example.com", 1)
	for i := 0; i < slots; i++ {
		if _, err := client.Write(q); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for udpN.Load() < slots && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := udpN.Load(); got != slots {
		t.Fatalf("forwarded %d while held, cap %d", got, slots)
	}
	if got := maxSeen.Load(); got > slots {
		t.Fatalf("inflight %d, cap %d", got, slots)
	}
	if _, err := tryTCP(gateTCP.Addr().String(), q); err == nil {
		t.Fatal("tcp answered while the pool was full")
	}
	if got := tcpN.Load(); got != 0 {
		t.Fatalf("upstream tcp %d while the pool was full", got)
	}

	letGo()
	if !waitIdle(g.slots) {
		t.Fatal("slots still held after the first burst")
	}
	resp := queryTCP(t, gateTCP.Addr().String(), q)
	if resultOf(resp) != "A 192.0.2.2" {
		t.Fatalf("tcp after release: %s", resultOf(resp))
	}
	if got := tcpN.Load(); got != 1 {
		t.Fatalf("upstream tcp %d after a slot was released", got)
	}
	if !waitIdle(g.slots) {
		t.Fatalf("tcp slot still held")
	}

	before := udpN.Load()
	for i := 0; i < burst; i++ {
		if _, err := client.Write(q); err != nil {
			t.Fatal(err)
		}
		want := before + int32(i+1)
		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && (udpN.Load() < want || len(g.slots) != 0) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := udpN.Load() - before; got != want-before || len(g.slots) != 0 {
			t.Fatalf("second burst forwarded %d, slots %d, want %d", got, len(g.slots), i+1)
		}
	}
	if got := maxSeen.Load(); got > slots {
		t.Fatalf("inflight %d, cap %d", got, slots)
	}
}

func waitIdle(slots chan struct{}) bool {
	deadline := time.Now().Add(2 * time.Second)
	for len(slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return len(slots) == 0
}

func tryTCP(addr string, q []byte) ([]byte, error) {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
	pkt := make([]byte, 2+len(q))
	binary.BigEndian.PutUint16(pkt[:2], uint16(len(q)))
	copy(pkt[2:], q)
	if _, err := c.Write(pkt); err != nil {
		return nil, err
	}
	var n uint16
	if err := binary.Read(c, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func TestDNSLogCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dns.jsonl")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 2000), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &gateDNS{
		open:   false,
		gate:   net.ParseIP("172.16.0.1").To4(),
		names:  map[string]struct{}{},
		logCap: 900,
	}
	if err := g.openLog(path); err != nil {
		t.Fatal(err)
	}
	defer g.log.Close()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Fatalf("over-cap log size %d", st.Size())
	}
	var last string
	for i := 0; i < 40; i++ {
		last = fmt.Sprintf("h%02d.example", i)
		g.answer(dnsQuery(last, 1), false)
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(cur)) > g.logCap {
		t.Fatalf("dns.jsonl %d exceeds %d", len(cur), g.logCap)
	}
	if !bytes.Contains(cur, []byte(last)) {
		t.Fatalf("newest missing: %s", cur)
	}
	if bytes.Contains(cur, []byte("h00.example")) {
		t.Fatalf("oldest still in current log: %s", cur)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(cur), "\n"), "\n") {
		var rec dnsRec
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("partial line %q: %v", line, err)
		}
	}
	prev, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(prev) == 0 || int64(len(prev)) > g.logCap {
		t.Fatalf("rotated size %d", len(prev))
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatal("kept more than one previous log")
	}
}

func forwardGate(t *testing.T, port string) *gateDNS {
	t.Helper()
	return &gateDNS{
		open:   true,
		gate:   net.ParseIP("172.16.0.1").To4(),
		names:  map[string]struct{}{},
		up:     []string{"127.0.0.1"},
		upPort: port,
		slots:  make(chan struct{}, maxInflight),
		log:    newLog(t),
	}
}

func serveGate(t *testing.T, g *gateDNS) (net.PacketConn, net.Listener) {
	t.Helper()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.serveUDP(udp)
	go g.serveTCP(tcp)
	t.Cleanup(func() {
		udp.Close()
		tcp.Close()
	})
	return udp, tcp
}

type fakeDNS struct {
	udp  net.PacketConn
	tcp  net.Listener
	port string
	mu   sync.Mutex
	udpN int
	tcpN int
}

func startFakeDNS(t *testing.T, onUDP, onTCP func([]byte) []byte) *fakeDNS {
	t.Helper()
	udp, tcp, port := listenBoth(t)
	f := &fakeDNS{udp: udp, tcp: tcp, port: port}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			f.mu.Lock()
			f.udpN++
			f.mu.Unlock()
			resp := onUDP(q)
			if len(resp) > 0 {
				_, _ = udp.WriteTo(resp, addr)
			}
		}
	}()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				var n uint16
				if err := binary.Read(c, binary.BigEndian, &n); err != nil || n > 65535 {
					return
				}
				q := make([]byte, n)
				if _, err := io.ReadFull(c, q); err != nil {
					return
				}
				f.mu.Lock()
				f.tcpN++
				f.mu.Unlock()
				resp := onTCP(q)
				if len(resp) == 0 || len(resp) > 65535 {
					return
				}
				_ = binary.Write(c, binary.BigEndian, uint16(len(resp)))
				_, _ = c.Write(resp)
			}(c)
		}
	}()
	t.Cleanup(func() {
		udp.Close()
		tcp.Close()
	})
	return f
}

func (f *fakeDNS) counts() (udp, tcp int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.udpN, f.tcpN
}

func listenBoth(t *testing.T) (net.PacketConn, net.Listener, string) {
	t.Helper()
	var last error
	for i := 0; i < 8; i++ {
		udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := udp.LocalAddr().(*net.UDPAddr).Port
		tcp, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return udp, tcp, strconv.Itoa(port)
		}
		last = err
		udp.Close()
	}
	t.Fatal(last)
	return nil, nil, ""
}

func queryUDP(t *testing.T, addr net.Addr, q []byte) []byte {
	t.Helper()
	c, err := net.DialUDP("udp4", nil, addr.(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(q); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func queryTCP(t *testing.T, addr string, q []byte) []byte {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	pkt := make([]byte, 2+len(q))
	binary.BigEndian.PutUint16(pkt[:2], uint16(len(q)))
	copy(pkt[2:], q)
	if _, err := c.Write(pkt); err != nil {
		t.Fatal(err)
	}
	var n uint16
	if err := binary.Read(c, binary.BigEndian, &n); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func respA(q []byte, ip string, tc bool) []byte {
	_, _, end, ok := parseQuestion(q)
	if !ok {
		return nil
	}
	resp := localResp(q, end, 0, false, rrA(net.ParseIP(ip)))
	if tc {
		resp[2] |= 0x02
	}
	return resp
}

func respPad(resp []byte, size int) []byte {
	if len(resp) >= size {
		return resp
	}
	return append(resp, make([]byte, size-len(resp))...)
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
