// Porter carries operator HTTP turns to an official coding CLI in the habitat.
// One process, one working directory; later POSTs pass --continue.
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Version is set at link time (-X main.Version=…). Local builds are "dev".
var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(Version)
		return
	}
	log.Printf("porter %s", Version)
	h := getenv("HARNESS", "claude")
	s := &server{harness: h}
	http.HandleFunc("/", s.serve)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type server struct {
	mu      sync.Mutex
	harness string
	started bool
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	prompt := strings.TrimSpace(string(body))
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.turn(prompt)
	if err != nil {
		http.Error(w, err.Error()+"\n"+out, 502)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, out)
}

func (s *server) turn(prompt string) (string, error) {
	cmd, err := s.cmd(prompt)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = getenv("HARNESS_WORKDIR", "/work")
	_ = os.MkdirAll(cmd.Dir, 0o755)
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, stderr.String())
	}
	s.started = true
	return out, nil
}

func (s *server) cmd(prompt string) (*exec.Cmd, error) {
	env := os.Environ()
	switch s.harness {
	case "claude":
		args := []string{"-p", "--output-format", "text", "--dangerously-skip-permissions"}
		if s.started {
			args = append([]string{"--continue"}, args...)
		}
		args = append(args, prompt)
		c := exec.Command("claude", args...)
		c.Env = append(env,
			"CI=true",
			"IS_SANDBOX=1",
			"CLAUDE_CODE_BUBBLEWRAP=1",
		)
		return c, nil
	case "grok":
		args := []string{"-p", prompt}
		if s.started {
			args = []string{"-p", "--continue", prompt}
		}
		return exec.Command("grok", args...), nil
	case "dsh":
		return exec.Command("dsh", "--profile", "headless", prompt), nil
	default:
		return nil, fmt.Errorf("unknown HARNESS %q", s.harness)
	}
}
