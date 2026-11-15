package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// errNoInbound is the url answer when create was passed --no-inbound.
var errNoInbound = errors.New("this habitat has no inbound address")

// An empty URL without noInbound is a broken instance, not --no-inbound.
var errEmptyInboundURL = errors.New("instance metadata has an empty inbound URL")

func InboundURLFromDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil {
		return "", err
	}
	var meta struct {
		InboundURL string `json:"inboundUrl"`
		NoInbound  bool   `json:"noInbound"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return "", err
	}
	if meta.NoInbound {
		return "", errNoInbound
	}
	if meta.InboundURL == "" {
		return "", errEmptyInboundURL
	}
	return meta.InboundURL, nil
}

func LoadInboundURL(root, id string) (string, error) {
	return InboundURLFromDir(filepath.Join(VarDir(root), id))
}

func inboundURL(hostPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
}

func inboundPublish(noInbound bool) []string {
	if noInbound {
		return nil
	}
	return []string{"-p", fmt.Sprintf("127.0.0.1::%d", InboundPort)}
}

func finishInbound(container string, noInbound bool, timeout time.Duration) (string, error) {
	if noInbound {
		return "", nil
	}
	hostPort, err := containerHostPort(container, InboundPort)
	if err != nil {
		return "", err
	}
	url := inboundURL(hostPort)
	if err := waitInbound(url, timeout); err != nil {
		return "", err
	}
	return url, nil
}

func containerHostPort(container string, inner int) (int, error) {
	out, err := Docker(10*time.Second, "port", container, fmt.Sprintf("%d/tcp", inner))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		_, p, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("docker port %s %d: %q", container, inner, out)
}

func waitInbound(rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			cancel()
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil {
		last = errWait("inbound not ready: " + rawURL)
	}
	return errWait("inbound not ready: " + rawURL + ": " + last.Error())
}

// Call sends one HTTP POST through the published inbound URL and waits for the body.
func Call(rawURL, body string) (string, error) {
	return CallTimeout(rawURL, body, 3*time.Minute)
}

func CallTimeout(rawURL, body string, d time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return string(b), fmt.Errorf("inbound %s: status %d", rawURL, resp.StatusCode)
	}
	return string(b), nil
}
