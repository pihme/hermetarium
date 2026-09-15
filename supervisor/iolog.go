package supervisor

import (
	"bufio"
	"encoding/json"
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
	Port        int    `json:"port,omitempty"`
	Method      string `json:"method,omitempty"`
	Allowed     bool   `json:"allowed"`
	Bytes       int    `json:"bytes,omitempty"`
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
	if u, err := url.Parse(rawURL); err == nil {
		if u.Hostname() != "" {
			dest = u.Hostname()
		}
		if u.Port() != "" {
			if p, err := strconv.Atoi(u.Port()); err == nil {
				port = p
			}
		} else if u.Scheme == "https" {
			port = 443
		}
	}
	status := f[3]
	dir := "out"
	if len(f) >= 11 {
		if lp, err := strconv.Atoi(f[10]); err == nil && lp == InboundPort {
			dir = "in"
		}
	}
	return Record{
		Time:        time.Unix(unix, ms*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano),
		Direction:   dir,
		Protocol:    "http",
		Destination: dest,
		Port:        port,
		Method:      f[5],
		Allowed:     !strings.Contains(status, "DENIED"),
		Bytes:       bytes,
	}, true
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil || !os.IsPermission(err) {
		return b, err
	}
	out, err := Docker(15*time.Second, "run", "--rm",
		"-v", filepath.Dir(path)+":/log:ro",
		"alpine:3.20", "cat", "/log/"+filepath.Base(path),
	)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func SyncAccessLog(accessPath, jsonlPath string) error {
	b, err := readFile(accessPath)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(jsonlPath, nil, 0o644)
		}
		return err
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
