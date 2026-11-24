package supervisor

import (
	"bufio"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Record struct {
	Time        string `json:"time"`
	Direction   string `json:"direction"`
	Protocol    string `json:"protocol"`
	Destination string `json:"destination"`
	// Upstream is the CONNECT server address (`%<a`, or the IP authority when that peer is "-").
	Upstream string `json:"upstream,omitempty"`
	Port     int    `json:"port,omitempty"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
	Result   string `json:"result,omitempty"`
	Allowed  bool   `json:"allowed"`
	// Reason says why a request was refused; it is empty when it was allowed.
	Reason string `json:"reason,omitempty"`
	// Aborted is a CONNECT closed before a tunnel existed (NONE_*_ABORTED).
	// The destination is the SNI host name when the gate file logged it.
	// TCP_TUNNEL_ABORTED is a tunnel that later closed, and is not this.
	Aborted bool `json:"aborted,omitempty"`
	Bytes   int  `json:"bytes,omitempty"`
	// TLS is set when the gate file logs bump mode: decrypted or passthrough.
	TLS string `json:"tls,omitempty"`
}

func ParseSquidLine(line string) (Record, bool) {
	f := strings.Fields(line)
	if len(f) < 7 {
		return Record{}, false
	}
	sec, frac, _ := strings.Cut(f[0], ".")
	unix, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return Record{}, false
	}
	ms := int64(0)
	if frac != "" {
		if v, err := strconv.ParseInt((frac + "000")[:3], 10, 64); err == nil {
			ms = v
		}
	}
	bytes, _ := strconv.Atoi(f[4])
	rawURL := f[6]
	dest := rawURL
	port := 80
	protocol := "http"
	path := ""
	parsedHost := false
	portSet := false
	portRejected := false
	authorityHost := ""
	if u, err := url.Parse(rawURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		protocol = u.Scheme
		if u.Hostname() != "" {
			dest = u.Hostname()
			parsedHost = true
		}
		if u.Path != "" && u.Path != "/" {
			path = u.Path
		}
		if u.RawQuery != "" {
			if path == "" {
				path = "/"
			}
			path += "?" + u.RawQuery
		}
		if u.Port() != "" {
			if p, err := strconv.Atoi(u.Port()); err == nil {
				port = p
				portSet = true
			}
		} else if u.Scheme == "https" {
			port = 443
			portSet = true
		}
	} else if host, p, ok := splitAuthority(rawURL); host != "" {
		// An IP is not a name. SNI names that connection.
		authorityHost = host
		dest = host
		if net.ParseIP(host) == nil {
			parsedHost = true
		}
		if ok {
			port = p
			portSet = true
		} else {
			// The authority named a port, and it was not 1..65535.
			port = 0
			portRejected = true
		}
	}
	status := f[3]
	// A gate file that logs %err_code %err_detail puts them at the end of the
	// line. `%6tr` (relative response time) is space-padded to ten characters,
	// so a slow request splits it into two fields and every later index shifts
	// by one; never index from the front or assume a fixed field count. Scan
	// the tail for the Squid error code and take the next field as the detail.
	// An `http_access deny` logs "-" for both, and a gate file that does not
	// log them has neither, so no code is found and the reason falls back.
	errCode, errDetail := "", ""
	for i := len(f) - 1; i >= 0 && i >= len(f)-4; i-- {
		if strings.HasPrefix(f[i], "ERR_") {
			errCode = errCodeOf(f[i])
			if i+1 < len(f) {
				errDetail = f[i+1]
			}
			break
		}
	}
	dir := "out"
	if len(f) >= 11 {
		if lp, err := strconv.Atoi(f[10]); err == nil && lp == InboundPort {
			dir = "in"
		}
	}
	// Bump mode and SNI follow the local port when the gate file logs them.
	tlsMode := ""
	if len(f) >= 12 {
		switch f[11] {
		case "bump":
			tlsMode = "decrypted"
		case "splice":
			tlsMode = "passthrough"
		}
	}
	if len(f) >= 13 && f[12] != "-" && !parsedHost {
		dest = f[12]
	}
	upstream := ""
	if f[5] == "CONNECT" {
		protocol = "https"
		if !portSet && !portRejected {
			port = 443
		}
		if len(f) >= 9 {
			upstream = peerAddress(f[8])
		}
		if upstream == "" && net.ParseIP(authorityHost) != nil {
			upstream = authorityHost
		}
	}
	return Record{
		Time:        time.Unix(unix, ms*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano),
		Direction:   dir,
		Protocol:    protocol,
		Destination: dest,
		Upstream:    upstream,
		Port:        port,
		Method:      f[5],
		Path:        path,
		Allowed:     !strings.Contains(status, "DENIED"),
		Aborted:     handshakeAbort(status, f[5]),
		Bytes:       bytes,
		TLS:         tlsMode,
		Reason:      refusalReason(status, errCode, errDetail),
	}, true
}

// handshakeAbort is NONE_*_ABORTED on CONNECT. No tunnel was established.
// TCP_TUNNEL_ABORTED is a later close of a tunnel that already carried bytes.
func handshakeAbort(status, method string) bool {
	if method != "CONNECT" {
		return false
	}
	head, _, _ := strings.Cut(status, "/")
	return strings.HasPrefix(head, "NONE") && strings.Contains(head, "ABORTED")
}

// splitAuthority parses host:port. A port outside 1..65535 returns the host and ok false.
func splitAuthority(raw string) (string, int, bool) {
	host, text, err := net.SplitHostPort(raw)
	if err != nil || host == "" {
		return "", 0, false
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return host, 0, false
	}
	return host, port, true
}

// peerAddress is the server IP in %Sh/%<a. "-" means Squid did not dial.
func peerAddress(hier string) string {
	_, addr, ok := strings.Cut(hier, "/")
	if !ok {
		return ""
	}
	addr = strings.Trim(addr, "[]")
	if net.ParseIP(addr) == nil {
		return ""
	}
	return addr
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil || !os.IsPermission(err) {
		return b, err
	}
	out, err := Docker(15*time.Second, "run", "--rm",
		"-v", filepath.Dir(path)+":/log:ro",
		AlpineImage, "cat", "/log/"+filepath.Base(path),
	)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func SyncAccessLog(accessPath, jsonlPath string) error {
	b, err := readFile(accessPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// Squid may not have created access.log yet. DNS lines still belong here.
		b = nil
	}
	out, err := os.Create(jsonlPath)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		rec, ok := ParseSquidLine(sc.Text())
		if !ok {
			continue
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// Lines rotated off dns.jsonl live in dns.jsonl.1. Read that file first,
	// then the current one, so a sync still has every retained query.
	dnsPath := filepath.Join(filepath.Dir(accessPath), "dns.jsonl")
	if err := appendDNSLog(enc, dnsPath+".1"); err != nil {
		return err
	}
	return appendDNSLog(enc, dnsPath)
}

func appendDNSLog(enc *json.Encoder, path string) error {
	b, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Protocol != "dns" {
			continue
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return sc.Err()
}

func ReadIoLog(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var recs []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		recs = append(recs, r)
	}
	return recs, sc.Err()
}

func LogHasDestination(path, host string) bool {
	recs, err := ReadIoLog(path)
	if err != nil {
		return false
	}
	for _, r := range recs {
		if r.Destination == host {
			return true
		}
	}
	return false
}

func LogHasDirection(path, dir string) bool {
	recs, err := ReadIoLog(path)
	if err != nil {
		return false
	}
	for _, r := range recs {
		if r.Direction == dir {
			return true
		}
	}
	return false
}

// The refusal reasons an I/O log record can carry.
const (
	ReasonNotAllowlisted = "not-allowlisted"
	ReasonCAUntrusted      = "ca-untrusted"
	ReasonNoSuchHost     = "no-such-host"
)

// errCodeOf strips the HTTP status Squid appends to %err_code, so
// "ERR_ACCESS_DENIED/403" becomes "ERR_ACCESS_DENIED".
func errCodeOf(tok string) string {
	name, _, _ := strings.Cut(tok, "/")
	return name
}

// refusalReason names why a request was refused, or "" when it was allowed.
// A refused line whose gate file does not log %err_code has no detail and
// counts as not-allowlisted.
func refusalReason(status, errCode, errDetail string) string {
	if !strings.Contains(status, "DENIED") {
		return ""
	}
	switch errCode {
	case "ERR_DNS_FAIL", "ERR_CANNOT_FORWARD":
		return ReasonNoSuchHost
	case "ERR_SECURE_CONNECT_FAIL":
		return ReasonCAUntrusted
	}
	if strings.Contains(errDetail, "certificate") {
		return ReasonCAUntrusted
	}
	return ReasonNotAllowlisted
}
