// vendor-mock is a Messages / Chat Completions origin for coding-agent tests.
// It requires the supervisor-injected key and returns a bash tool call (id -u).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	// A gate path prefix still ends in the vendor path.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/messages"):
			anthropic(w, r)
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			openai(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	addr := getenv("LISTEN", ":18082")
	log.Fatal(http.ListenAndServe(addr, nil))
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func expectKey() string {
	return getenv("MOCK_EXPECT_KEY", "htm-test-key")
}

func authorized(r *http.Request) bool {
	want := expectKey()
	if r.Header.Get("x-api-key") == want {
		return true
	}
	auth := r.Header.Get("Authorization")
	return auth == "Bearer "+want
}

func anthropic(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(body, `"tool_result"`) {
		_, _ = io.WriteString(w, `{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"0"}]}`)
		return
	}
	_, _ = io.WriteString(w, `{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"id -u"}}]}`)
}

func openai(w http.ResponseWriter, r *http.Request) {
	writeReport(r)
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	if strings.Contains(string(raw), `"stream":true`) || strings.Contains(string(raw), `"stream": true`) {
		streamOpenAI(w)
		return
	}
	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(raw, &req)
	w.Header().Set("Content-Type", "application/json")
	for _, m := range req.Messages {
		if m.Role == "tool" {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"0"}}]}`)
			return
		}
	}
	_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"id -u\"}"}}]}}]}`)
}

func writeReport(r *http.Request) {
	path := os.Getenv("MOCK_REPORT")
	if path == "" {
		return
	}
	var b strings.Builder
	for _, v := range r.Header.Values("Authorization") {
		fmt.Fprintf(&b, "auth=%s\n", v)
	}
	fmt.Fprintf(&b, "host=%s\npath=%s\n", r.Host, r.URL.Path)
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
}

func streamOpenAI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, _ := w.(http.Flusher)
	for i, s := range []string{"one", "two", "three"} {
		if i > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		fmt.Fprintf(w, "data: %s\n\n", s)
		if fl != nil {
			fl.Flush()
		}
	}
}
