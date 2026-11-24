package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A build step reaches the network in a container layer with the image's own
// trust store. Inside a habitat the gate re-signs TLS with a CA that no stock
// base image knows, so `RUN apk add` fails there while every host tool works.
// The CA is also not an input an image may carry: put a COPY of it in a
// Dockerfile and a released image trusts one habitat's CA.
//
// So the trust is added to the build and not to the image, and only when
// MITMCertPath exists. The overlay Dockerfile puts our syntax directive in
// front of the original and gives each RUN the CA by --mount=type=bind, which
// BuildKit drops with the rest of the RUN. Its source is one hidden stage that
// lives as long as the build: a COPY of the context's ca.crt and nothing else,
// so a base image needs no trust store and no package for one.
//
// The CA is in the context, because a CLI older than 23.0 has no
// --build-context. That is why the stage is hidden rather than named: a
// context file no COPY reaches is not in a layer of the final image, and no
// FROM names it.

const (
	// buildCAHostPath is the one path a habitat CA is read from. It is the
	// path the day-one guide documents, so no second path has to exist.
	buildCAHostPath = MITMCertPath
	// buildCADockerfile is the overlay's name in the build directory. The
	// original Dockerfile stays the stamp that says which image was built.
	buildCADockerfile = "Dockerfile.hermetarium-build-ca"
	// buildCACert is the file added to the build context, and the path inside
	// the RUN that holds it.
	buildCACert      = "ca.crt"
	buildCAContainer = "/tmp/hermetarium-build-ca.crt"
	// buildCAStage carries the CA for the length of the build.
	buildCAStage = "htm-build-ca"
	// buildCASyntax is the frontend the overlay needs. The classic builder
	// has no --mount.
	buildCASyntax = "# syntax=docker/dockerfile:1.7"
	// buildCAArg is a build argument whose default is a stage name. Naming an
	// image this way leaves no other FROM in the file with an unknown name:
	// a FROM of a missing stage stops the whole build at parse time, even in
	// a stage nothing uses.
	buildCAArg = "_HTM_BUILD_CA"
	// buildCAEnv is prefixed to the RUN command. Only SSL_CERT_FILE is set:
	// the packages that read another variable of MITMTrustEnv ship one, and a
	// replaced value would drop the store an image that has one trusts.
	buildCAEnv = "env SSL_CERT_FILE=" + buildCAContainer
	// buildCAMount is the one flag form the overlay adds, so an image with no
	// trust store and no package for one still gets the file.
	buildCAMount = "--mount=type=bind,from=" + buildCAStage + ",source=/" + buildCAStage + "/" + buildCACert + ",target=" + buildCAContainer + ",ro"
)

// BuildCA is the CA builds in this habitat trust. Enabled is false when the
// host has no habitat CA, which is the case in CI.
type BuildCA struct {
	Enabled bool
	Path    string
}

// HabitatBuildCA reports whether a build here has to trust a habitat CA.
// Only absence turns it off: an unreadable file has to fail the build, not
// skip it, or the package manager's TLS error reads as a bug in our code.
func HabitatBuildCA() BuildCA {
	if _, err := os.Stat(buildCAHostPath); err != nil {
		return BuildCA{}
	}
	return BuildCA{Enabled: true, Path: buildCAHostPath}
}

// Prepare puts ca.crt in the build context the overlay copies from. It is the
// only file the harness adds there; no Dockerfile in the tree names it.
func (ca BuildCA) Prepare(dir string) error {
	if !ca.Enabled {
		return nil
	}
	b, err := os.ReadFile(ca.Path)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, buildCACert), b, 0o644)
}

// Overlays writes the overlay beside df and returns the build options that
// select it. With no CA it returns opts unchanged, so a caller passes the
// same options on both paths.
func (ca BuildCA) Overlays(dir, df string, opts ...string) ([]string, error) {
	if !ca.Enabled {
		return opts, nil
	}
	text, err := os.ReadFile(df)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, buildCADockerfile)
	if err := os.WriteFile(path, []byte(ca.overlay(string(text))), 0o644); err != nil {
		return nil, err
	}
	return append([]string{"-f", path}, opts...), nil
}

// overlay gives every RUN the CA. A Dockerfile with no stage cannot take the
// two the CA needs, and is left alone rather than made to fail.
func (ca BuildCA) overlay(df string) string {
	if !hasFrom(df) {
		return df
	}
	var out strings.Builder
	out.WriteString(buildCASyntax + "\n")
	out.WriteString("ARG " + buildCAArg + "=" + buildCAStage + "\n")
	out.WriteString("FROM ${" + buildCAArg + "} AS htm-build-ca-unused\n")
	out.WriteString("FROM scratch AS " + buildCAStage + "\n")
	out.WriteString("COPY " + buildCACert + " /" + buildCAStage + "/" + buildCACert + "\n")
	for _, line := range strings.Split(df, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case isCommentDirective(trimmed):
			// A parser directive of the original would take the place of the
			// syntax line, or become a comment in the middle of the file.
		case isKeyword(trimmed, "RUN"):
			out.WriteString("RUN " + buildCAMount + " " + buildCAEnv + " " + strings.TrimSpace(trimmed[len("RUN"):]) + "\n")
		default:
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

// hasFrom reports whether df defines a stage, which the overlay's own FROMs
// need to belong to the same file.
func hasFrom(df string) bool {
	for _, line := range strings.Split(df, "\n") {
		if isKeyword(strings.TrimSpace(line), "FROM") {
			return true
		}
	}
	return false
}

// isKeyword matches a Dockerfile instruction name, with any spacing before it
// and the whitespace that has to follow it.
func isKeyword(line, name string) bool {
	if len(line) <= len(name) || !strings.EqualFold(line[:len(name)], name) {
		return false
	}
	return line[len(name)] == ' ' || line[len(name)] == '\t'
}

// isCommentDirective matches a line a frontend may read as front matter:
// # syntax= or # escape=, before or after the first instruction.
func isCommentDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "# syntax=") || strings.HasPrefix(lower, "#syntax=") ||
		strings.HasPrefix(lower, "# escape=") || strings.HasPrefix(lower, "#escape=")
}

// Docker is the build, with BuildKit selected for that call. The overlay's
// --mount needs BuildKit, and a caller's DOCKER_BUILDKIT=0 would drop the
// trust without a word. The value is set for the docker process only: the
// daemon and every other build on the machine keep the builder they had.
func (ca BuildCA) Docker(timeout time.Duration, args ...string) (string, error) {
	if !ca.Enabled {
		return Docker(timeout, args...)
	}
	buildKitEnv.Lock()
	dockerExtraEnv = []string{"DOCKER_BUILDKIT=1"}
	out, err := Docker(timeout, args...)
	dockerExtraEnv = nil
	buildKitEnv.Unlock()
	return out, err
}

// buildKitEnv guards dockerExtraEnv for the length of one build.
var buildKitEnv sync.Mutex
