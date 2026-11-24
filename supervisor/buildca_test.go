package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A habitat CA turns a build into an overlay; no CA has to leave the build
// exactly as it was, because CI builds the same images without one.
func TestBuildCAOff(t *testing.T) {
	ca := BuildCA{}
	dir := t.TempDir()
	df := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(df, []byte("FROM alpine:3.24\nRUN apk add curl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ca.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, buildCACert)); !os.IsNotExist(err) {
		t.Fatalf("ca.crt written with no CA, stat err: %v", err)
	}
	opts := []string{"-f", df}
	got, err := ca.Overlays(dir, df, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\x00") != strings.Join(opts, "\x00") {
		t.Fatalf("options = %v, want %v", got, opts)
	}
	if _, err := os.Stat(filepath.Join(dir, buildCADockerfile)); !os.IsNotExist(err) {
		t.Fatalf("overlay written with no CA, stat err: %v", err)
	}
}

func TestBuildCAOverlay(t *testing.T) {
	const cert = "-----BEGIN CERTIFICATE-----\nnot a real one\n-----END CERTIFICATE-----\n"
	caDir := t.TempDir()
	caPath := filepath.Join(caDir, "mitm.crt")
	if err := os.WriteFile(caPath, []byte(cert), 0o644); err != nil {
		t.Fatal(err)
	}
	ca := BuildCA{Enabled: true, Path: caPath}
	ctx := t.TempDir()
	if err := ca.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(ctx, buildCACert))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != cert {
		t.Fatalf("context ca.crt:\n%s", b)
	}
	df := filepath.Join(ctx, "Dockerfile")
	const want = `# syntax=docker/dockerfile:1.4
FROM ${BASE} AS base
# RUN in a comment stays a comment
RUN apk add --no-cache curl \
    && rm -rf /var/cache/apk/*
RUN	apt-get update
COPY day-one.md /etc/hermetarium/day-one.md
`
	if err := os.WriteFile(df, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err := ca.Overlays(ctx, df, "-t", "img")
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(ctx, buildCADockerfile)
	if len(opts) < 3 || opts[0] != "-f" || opts[1] != overlayPath || opts[2] != "-t" {
		t.Fatalf("options = %v, want -f %s then the caller's", opts, overlayPath)
	}
	got, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	// One syntax line, and it is ours: a second one in the file would either
	// be ignored or break the parse.
	if strings.Count(text, "# syntax=") != 1 || !strings.HasPrefix(text, buildCASyntax+"\n") {
		t.Fatalf("syntax line:\n%s", text)
	}
	for _, line := range []string{
		"ARG " + buildCAArg + "=" + buildCAStage + "\n",
		"FROM ${" + buildCAArg + "} AS htm-build-ca-unused\n",
		"FROM scratch AS " + buildCAStage + "\n",
		"COPY " + buildCACert + " /" + buildCAStage + "/" + buildCACert + "\n",
		"FROM ${BASE} AS base\n",
		"# RUN in a comment stays a comment\n",
		"COPY day-one.md /etc/hermetarium/day-one.md\n",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("overlay lost %q:\n%s", line, text)
		}
	}
	// Each RUN gets the CA, and a continuation line keeps its place.
	for _, run := range []string{
		"RUN " + buildCAMount + " " + buildCAEnv + " apk add --no-cache curl \\\n    && rm -rf /var/cache/apk/*\n",
		"RUN " + buildCAMount + " " + buildCAEnv + " apt-get update\n",
	} {
		if !strings.Contains(text, run) {
			t.Fatalf("overlay RUN %q missing:\n%s", run, text)
		}
	}
}

func TestBuildCAOverlayKeepsAFileWithNoStage(t *testing.T) {
	ca := BuildCA{Enabled: true, Path: filepath.Join(t.TempDir(), "mitm.crt")}
	if err := os.WriteFile(ca.Path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const df = "# no stage\nRUN apk add curl\n"
	if got := ca.overlay(df); got != df {
		t.Fatalf("overlay = %q, want the file unchanged", got)
	}
}

// A caller with the classic builder still gets the overlay built, and the
// setting is gone again before the next docker call.
func TestBuildCADockerSelectsBuildKit(t *testing.T) {
	ca := BuildCA{Enabled: true, Path: "x"}
	var seen [][]string
	stubDocker(t, func(timeout time.Duration, args ...string) (string, error) {
		seen = append(seen, append([]string(nil), dockerExtraEnv...))
		return "ok", nil
	})
	if _, err := ca.Docker(time.Minute, "build", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(time.Minute, "inspect", "x"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("calls = %v", seen)
	}
	if len(seen[0]) != 1 || seen[0][0] != "DOCKER_BUILDKIT=1" {
		t.Fatalf("build environment = %v", seen[0])
	}
	if len(seen[1]) != 0 {
		t.Fatalf("later call inherited %v", seen[1])
	}
	if _, err := (BuildCA{}).Docker(time.Minute, "build", "."); err != nil {
		t.Fatal(err)
	}
	if len(seen[2]) != 0 {
		t.Fatalf("build with no CA set %v", seen[2])
	}
}
