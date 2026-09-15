package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func InboundURLFromDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil {
		return "", err
	}
	var meta struct {
		InboundURL string `json:"inboundUrl"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return "", err
	}
	if meta.InboundURL == "" {
		return "", fmt.Errorf("no inboundUrl in %s", dir)
	}
	return meta.InboundURL, nil
}

func LoadInboundURL(root, id string) (string, error) {
	return InboundURLFromDir(filepath.Join(VarDir(root), id))
}

func inboundURL(hostPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
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

// Call sends one HTTP POST through the published inbound URL and waits for the body.
func Call(rawURL, body string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
