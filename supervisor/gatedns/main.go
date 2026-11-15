// Gate DNS. Listens on port 53 in the gate network namespace.
// Stand-in names always answer with the gate address. Other names are
// forwarded only when the gate file opens the web, and only to the
// resolvers Squid itself uses.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type dnsRec struct {
	Time        string `json:"time"`
	Direction   string `json:"direction"`
	Protocol    string `json:"protocol"`
	Destination string `json:"destination"`
	Port        int    `json:"port,omitempty"`
	Method      string `json:"method,omitempty"`
	Result      string `json:"result,omitempty"`
	Allowed     bool   `json:"allowed"`
}

type gateDNS struct {
	open  bool
	gate  net.IP
	names map[string]struct{}
	up    []string
	mu    sync.Mutex
	log   *os.File
}

func main() {
	g, err := loadGateDNS("/log/dns.conf", "/log/dns.jsonl")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gatedns:", err)
		os.Exit(1)
	}
	defer g.log.Close()
	udp, err := net.ListenPacket("udp4", ":53")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gatedns:", err)
		os.Exit(1)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp4", ":53")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gatedns:", err)
		os.Exit(1)
	}
	defer tcp.Close()
	// The strong wall waits for this before it starts the guest.
	if err := os.WriteFile("/log/dns.ready", nil, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gatedns:", err)
		os.Exit(1)
	}
	go g.serveTCP(tcp)
	buf := make([]byte, 4096)
	for {
		n, addr, err := udp.ReadFrom(buf)
		if err != nil {
			continue
		}
		q := append([]byte(nil), buf[:n]...)
		go func(addr net.Addr) {
			resp := g.answer(q)
			if len(resp) > 0 {
				_, _ = udp.WriteTo(resp, addr)
			}
		}(addr)
	}
}

func loadGateDNS(confPath, logPath string) (*gateDNS, error) {
	b, err := os.ReadFile(confPath)
	if err != nil {
		return nil, err
	}
	g := &gateDNS{names: map[string]struct{}{}}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "open":
			g.open = f[1] == "1"
		case "gate":
			g.gate = net.ParseIP(f[1]).To4()
		case "name":
			g.names[strings.ToLower(f[1])] = struct{}{}
		case "up":
			if ip := net.ParseIP(f[1]); ip != nil && ip.To4() != nil {
				g.up = append(g.up, f[1])
			}
		}
	}
	if g.gate == nil {
		return nil, fmt.Errorf("dns.conf has no gate address")
	}
	// Skip a nameserver that is this process, so a forward cannot loop.
	var up []string
	for _, s := range g.up {
		if s == g.gate.String() {
			continue
		}
		up = append(up, s)
	}
	g.up = up
	g.log, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (g *gateDNS) serveTCP(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			var n uint16
			if err := binary.Read(c, binary.BigEndian, &n); err != nil || n > 4096 {
				return
			}
			q := make([]byte, n)
			if _, err := io.ReadFull(c, q); err != nil {
				return
			}
			resp := g.answer(q)
			if len(resp) == 0 {
				return
			}
			_ = binary.Write(c, binary.BigEndian, uint16(len(resp)))
			_, _ = c.Write(resp)
		}(c)
	}
}

func (g *gateDNS) answer(q []byte) []byte {
	name, typ, qend, ok := parseQuestion(q)
	if !ok {
		return nil
	}
	if _, stand := g.names[name]; stand && typ == "A" {
		resp := localResp(q, qend, 0, true, rrA(g.gate))
		g.record(name, typ, g.gate.String(), true)
		return resp
	}
	if _, stand := g.names[name]; stand {
		resp := localResp(q, qend, 0, true, nil)
		g.record(name, typ, "NODATA", true)
		return resp
	}
	if !g.open {
		resp := localResp(q, qend, 3, false, nil)
		g.record(name, typ, "NXDOMAIN", false)
		return resp
	}
	resp, err := g.forward(q)
	if err != nil {
		resp = localResp(q, qend, 2, false, nil)
		g.record(name, typ, "SERVFAIL", true)
		return resp
	}
	g.record(name, typ, resultOf(resp), true)
	return resp
}

func (g *gateDNS) forward(q []byte) ([]byte, error) {
	var last error
	for _, s := range g.up {
		conn, err := net.DialTimeout("udp", net.JoinHostPort(s, "53"), time.Second)
		if err != nil {
			last = err
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = conn.Write(q)
		if err != nil {
			conn.Close()
			last = err
			continue
		}
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		conn.Close()
		if err != nil || n < 12 {
			last = err
			continue
		}
		return buf[:n], nil
	}
	if last == nil {
		last = fmt.Errorf("no upstream resolver")
	}
	return nil, last
}

func (g *gateDNS) record(name, typ, result string, allowed bool) {
	rec := dnsRec{
		Time:        time.Now().UTC().Format(time.RFC3339Nano),
		Direction:   "out",
		Protocol:    "dns",
		Destination: name,
		Port:        53,
		Method:      typ,
		Result:      result,
		Allowed:     allowed,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, _ = g.log.Write(append(b, '\n'))
}

func localResp(q []byte, qend, rcode int, aa bool, rr []byte) []byte {
	resp := make([]byte, qend+len(rr))
	copy(resp, q[:qend])
	copy(resp[qend:], rr)
	resp[2] |= 0x80
	resp[2] &^= 0x02
	if aa {
		resp[2] |= 0x04
	} else {
		resp[2] &^= 0x04
	}
	resp[3] = byte(rcode & 0x0F)
	an := uint16(0)
	if len(rr) > 0 {
		an = 1
	}
	binary.BigEndian.PutUint16(resp[6:8], an)
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)
	return resp
}

func rrA(ip net.IP) []byte {
	b := make([]byte, 16)
	b[0], b[1] = 0xC0, 0x0C
	binary.BigEndian.PutUint16(b[2:4], 1)
	binary.BigEndian.PutUint16(b[4:6], 1)
	binary.BigEndian.PutUint32(b[6:10], 60)
	binary.BigEndian.PutUint16(b[10:12], 4)
	copy(b[12:16], ip.To4())
	return b
}

func parseQuestion(q []byte) (name, typ string, qend int, ok bool) {
	if len(q) < 12 || q[2]&0x80 != 0 || q[2]&0x78 != 0 {
		return "", "", 0, false
	}
	if binary.BigEndian.Uint16(q[4:6]) != 1 {
		return "", "", 0, false
	}
	name, qend, ok = parseName(q, 12)
	if !ok || qend+4 > len(q) {
		return "", "", 0, false
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" {
		return "", "", 0, false
	}
	typ = typeName(binary.BigEndian.Uint16(q[qend : qend+2]))
	return name, typ, qend + 4, true
}

func parseName(pkt []byte, off int) (string, int, bool) {
	// Pointer jumps are the loop guard. A name is limited to 255 octets
	// (RFC 1035), not to a fixed number of labels.
	const maxJumps = 10
	const maxName = 255
	var labels []string
	jumped := false
	end := off
	nameLen := 0
	jumps := 0
	for {
		if off >= len(pkt) {
			return "", 0, false
		}
		n := int(pkt[off])
		if n == 0 {
			nameLen++
			if nameLen > maxName {
				return "", 0, false
			}
			if !jumped {
				end = off + 1
			}
			return strings.Join(labels, "."), end, true
		}
		if n&0xC0 == 0xC0 {
			if off+1 >= len(pkt) {
				return "", 0, false
			}
			jumps++
			if jumps > maxJumps {
				return "", 0, false
			}
			if !jumped {
				end = off + 2
			}
			off = int(n&0x3F)<<8 | int(pkt[off+1])
			jumped = true
			continue
		}
		if n&0xC0 != 0 || off+1+n > len(pkt) {
			return "", 0, false
		}
		nameLen += 1 + n
		if nameLen > maxName {
			return "", 0, false
		}
		labels = append(labels, string(pkt[off+1:off+1+n]))
		off += 1 + n
	}
}

func typeName(t uint16) string {
	switch t {
	case 1:
		return "A"
	case 28:
		return "AAAA"
	case 5:
		return "CNAME"
	case 12:
		return "PTR"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 2:
		return "NS"
	default:
		return fmt.Sprintf("TYPE%d", t)
	}
}

func resultOf(pkt []byte) string {
	if len(pkt) < 12 {
		return "FORMERR"
	}
	switch pkt[3] & 0x0F {
	case 0:
	case 3:
		return "NXDOMAIN"
	case 2:
		return "SERVFAIL"
	default:
		return fmt.Sprintf("RCODE%d", pkt[3]&0x0F)
	}
	off, ok := questionEnd(pkt)
	if !ok {
		return "NODATA"
	}
	an := int(binary.BigEndian.Uint16(pkt[6:8]))
	var out []string
	for i := 0; i < an && i < 16; i++ {
		var kind string
		var data string
		off, kind, data, ok = nextRR(pkt, off)
		if !ok {
			break
		}
		if data != "" {
			out = append(out, kind+" "+data)
		}
	}
	if len(out) == 0 {
		return "NODATA"
	}
	return strings.Join(out, ",")
}

func questionEnd(pkt []byte) (int, bool) {
	_, end, ok := parseName(pkt, 12)
	if !ok || end+4 > len(pkt) {
		return 0, false
	}
	return end + 4, true
}

func nextRR(pkt []byte, off int) (int, string, string, bool) {
	_, off, ok := parseName(pkt, off)
	if !ok || off+10 > len(pkt) {
		return 0, "", "", false
	}
	typ := binary.BigEndian.Uint16(pkt[off : off+2])
	rdl := int(binary.BigEndian.Uint16(pkt[off+8 : off+10]))
	off += 10
	if off+rdl > len(pkt) {
		return 0, "", "", false
	}
	rdata := pkt[off : off+rdl]
	off += rdl
	switch typ {
	case 1:
		if len(rdata) == 4 {
			return off, "A", net.IP(rdata).String(), true
		}
	case 28:
		if len(rdata) == 16 {
			return off, "AAAA", net.IP(rdata).String(), true
		}
	case 5:
		if n, _, ok := parseName(pkt, off-rdl); ok {
			return off, "CNAME", n, true
		}
	}
	return off, typeName(typ), "", true
}
