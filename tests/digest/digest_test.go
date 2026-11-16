package digest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	pathSecret = "super-secret-key-value"
	pathBody   = "REQUESTBODY-do-not-mail"
	gateSecret = "gate-file-key-DO-NOT-MAIL"
)

func TestDigestDeniedThenQuiet(t *testing.T) {
	r := newRun(t)
	r.writeLog([]supervisor.Record{
		{
			Time: "2026-11-16T10:00:00Z", Direction: "out", Protocol: "https",
			Destination: "ok-tunnel.example", Method: "CONNECT", Allowed: true, TLS: "passthrough",
		},
		{
			Time: "2026-11-16T10:00:01Z", Direction: "out", Protocol: "https",
			Destination: "evil.example", Method: "GET", Allowed: false,
			Path: `/x?q="allowed":true&body=` + pathBody + `&key=` + pathSecret,
		},
		{
			Time: "2026-11-16T10:00:02Z", Direction: "in", Protocol: "http",
			Destination: "inbound.example", Method: "POST", Allowed: false,
		},
	})
	out := r.run(t, false)
	mail := r.mail(t)
	if strings.Count(mail, "---MAIL---") != 1 {
		t.Fatalf("mails\n%s\n%s", mail, out)
	}
	if !strings.Contains(mail, "To: owner@example.com") || !strings.Contains(mail, "From: wall@example.com") {
		t.Fatalf("headers\n%s", mail)
	}
	if !strings.Contains(mail, "Habitat: hab1") || !strings.Contains(mail, "evil.example 1") {
		t.Fatalf("destination\n%s", mail)
	}
	if !strings.Contains(mail, "Span: 2026-11-16T10:00:01Z to 2026-11-16T10:00:01Z") {
		t.Fatalf("span\n%s", mail)
	}
	for _, absent := range []string{pathSecret, pathBody, gateSecret, "ok-tunnel.example", "inbound.example", `"allowed":true`} {
		if strings.Contains(mail, absent) {
			t.Fatalf("mail contains %q\n%s", absent, mail)
		}
	}
	if !strings.Contains(r.calls(t), "logs hab1") {
		t.Fatalf("logs not run\n%s", r.calls(t))
	}
	r.run(t, false)
	if strings.Count(r.mail(t), "---MAIL---") != 1 {
		t.Fatalf("second run sent mail\n%s", r.mail(t))
	}
}

func TestDigestManyAreOneMail(t *testing.T) {
	r := newRun(t)
	r.writeLog([]supervisor.Record{
		{
			Time: "2026-11-16T09:00:00Z", Direction: "out", Protocol: "https",
			Destination: "ok.example", Method: "GET", Allowed: true,
		},
	})
	r.run(t, false)
	if _, err := os.Stat(r.mailPath); err == nil {
		t.Fatal("empty run sent mail")
	}

	var recs []supervisor.Record
	for i, ts := range []string{"2026-11-16T10:01:00Z", "2026-11-16T10:02:00Z", "2026-11-16T10:03:00Z"} {
		rec := supervisor.Record{
			Time: ts, Direction: "out", Protocol: "https",
			Destination: "a.example", Method: "GET", Allowed: false,
		}
		if i == 0 {
			rec.Path = "/a?key=" + pathSecret + "&body=" + pathBody
		}
		recs = append(recs, rec)
	}
	recs = append(recs,
		supervisor.Record{
			Time: "2026-11-16T10:04:00Z", Direction: "out", Protocol: "https",
			Destination: "b.example", Method: "PUT", Allowed: false,
		},
		supervisor.Record{
			Time: "2026-11-16T10:05:00Z", Direction: "out", Protocol: "https",
			Destination: "b.example", Method: "PUT", Allowed: false,
		},
		supervisor.Record{
			Time: "2026-11-16T10:06:00Z", Direction: "out", Protocol: "https",
			Destination: "pinned.example", Method: "CONNECT", Allowed: true, Aborted: true, TLS: "passthrough",
		},
		supervisor.Record{
			Time: "2026-11-16T10:07:00Z", Direction: "out", Protocol: "https",
			Destination: "ok.example", Method: "GET", Allowed: true,
		},
	)
	r.writeLog(recs)
	out := r.run(t, false)
	mail := r.mail(t)
	if strings.Count(mail, "---MAIL---") != 1 {
		t.Fatalf("mails\n%s\n%s", mail, out)
	}
	for _, want := range []string{
		"Habitat: hab1",
		"Span: 2026-11-16T10:01:00Z to 2026-11-16T10:06:00Z",
		"a.example 3",
		"b.example 2",
		"Closed during the TLS handshake:",
		"pinned.example 1",
	} {
		if !strings.Contains(mail, want) {
			t.Fatalf("missing %q\n%s", want, mail)
		}
	}
	for _, absent := range []string{pathSecret, pathBody, gateSecret, "ok.example"} {
		if strings.Contains(mail, absent) {
			t.Fatalf("mail contains %q\n%s", absent, mail)
		}
	}
}

func TestDigestSpanOrdersFractionalSeconds(t *testing.T) {
	// RFC3339Nano drops trailing zeros. These are .000, .100, and .120
	// in the same second. As text, the .120 form sorts first.
	const (
		at000 = "2026-11-16T10:00:01Z"
		at100 = "2026-11-16T10:00:01.1Z"
		at120 = "2026-11-16T10:00:01.12Z"
	)
	r := newRun(t)
	r.writeLog([]supervisor.Record{
		{
			Time: at120, Direction: "out", Protocol: "https",
			Destination: "later.example", Method: "GET", Allowed: false,
		},
		{
			Time: at100, Direction: "out", Protocol: "https",
			Destination: "mid.example", Method: "GET", Allowed: false,
		},
		{
			Time: at000, Direction: "out", Protocol: "http",
			Destination: "early.example", Method: "GET", Allowed: false,
		},
		{
			Time: at100, Direction: "out", Protocol: "dns",
			Destination: "blocked.example", Method: "A", Result: "NXDOMAIN 192.0.2.9 secret-dns-answer",
			Allowed: false,
		},
	})
	out := r.run(t, false)
	mail := r.mail(t)
	if strings.Count(mail, "---MAIL---") != 1 {
		t.Fatalf("mails\n%s\n%s", mail, out)
	}
	want := "Span: " + at000 + " to " + at120
	if !strings.Contains(mail, want) {
		t.Fatalf("span\n%s", mail)
	}
	for _, dest := range []string{"early.example 1", "mid.example 1", "later.example 1", "blocked.example 1"} {
		if !strings.Contains(mail, dest) {
			t.Fatalf("missing %s\n%s", dest, mail)
		}
	}
	if strings.Contains(mail, "secret-dns-answer") || strings.Contains(mail, "192.0.2.9") || strings.Contains(mail, "NXDOMAIN") {
		t.Fatalf("dns answer in mail\n%s", mail)
	}
}

func TestDigestFailedMailDoesNotAdvance(t *testing.T) {
	r := newRun(t)
	r.writeLog([]supervisor.Record{
		{
			Time: "2026-11-16T10:00:01Z", Direction: "out", Protocol: "http",
			Destination: "evil.example", Method: "GET", Allowed: false,
		},
	})
	out, err := r.command(true).CombinedOutput()
	if err == nil {
		t.Fatalf("failed mail exited 0\n%s", out)
	}
	if _, statErr := os.Stat(r.state); statErr == nil {
		t.Fatalf("state advanced\n%s", out)
	}
	r.run(t, false)
	mail := r.mail(t)
	if strings.Count(mail, "---MAIL---") != 1 || !strings.Contains(mail, "evil.example 1") {
		t.Fatalf("retry\n%s", mail)
	}
}

type run struct {
	script   string
	root     string
	fixture  string
	callPath string
	mailPath string
	state    string
	envFile  string
	herm     string
}

func newRun(t *testing.T) *run {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	checkout := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	root := t.TempDir()
	id := "hab1"
	inst := filepath.Join(root, "var", id)
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "squid.conf"), []byte(gateSecret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &run{
		script:   filepath.Join(checkout, "omakase", "digest.sh"),
		root:     root,
		fixture:  filepath.Join(root, "fixture.jsonl"),
		callPath: filepath.Join(root, "calls"),
		mailPath: filepath.Join(root, "mail"),
		state:    filepath.Join(inst, "digest.state"),
		envFile:  filepath.Join(root, "mail.env"),
		herm:     filepath.Join(root, "hermetarium"),
	}
	writeExe(t, r.herm, `#!/bin/sh
printf '%s\n' "$*" >> "$CALLS"
if [ "$1" != logs ]; then
  echo "unexpected $*" >&2
  exit 1
fi
cat "$FIXTURE" > "$IOJSONL"
exit 0
`)
	writeExe(t, filepath.Join(root, "sendmail-stub"), `#!/bin/sh
if [ "${MAIL_FAIL:-}" = 1 ]; then
  cat >/dev/null
  exit 1
fi
cat >> "$MAIL_OUT"
printf '\n---MAIL---\n' >> "$MAIL_OUT"
exit 0
`)
	env := "MAIL_TO=owner@example.com\nMAIL_FROM=wall@example.com\nMAIL_COMMAND=" + filepath.Join(root, "sendmail-stub") + "\n"
	if err := os.WriteFile(r.envFile, []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *run) writeLog(recs []supervisor.Record) {
	var b strings.Builder
	for _, rec := range recs {
		raw, err := json.Marshal(rec)
		if err != nil {
			panic(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(r.fixture, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
}

func (r *run) command(fail bool) *exec.Cmd {
	cmd := exec.Command(r.script)
	env := []string{
		"HERMETARIUM_ROOT=" + r.root,
		"HERMETARIUM=" + r.herm,
		"HERMETARIUM_ID=hab1",
		"HERMETARIUM_MAIL_ENV=" + r.envFile,
		"CALLS=" + r.callPath,
		"FIXTURE=" + r.fixture,
		"IOJSONL=" + filepath.Join(r.root, "var", "hab1", "io.jsonl"),
		"MAIL_OUT=" + r.mailPath,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.root,
	}
	if fail {
		env = append(env, "MAIL_FAIL=1")
	}
	cmd.Env = env
	return cmd
}

func (r *run) run(t *testing.T, fail bool) string {
	t.Helper()
	out, err := r.command(fail).CombinedOutput()
	if fail {
		if err == nil {
			t.Fatalf("exited 0\n%s", out)
		}
		return string(out)
	}
	if err != nil {
		t.Fatalf("digest.sh: %v\n%s", err, out)
	}
	return string(out)
}

func (r *run) mail(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.mailPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (r *run) calls(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.callPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
