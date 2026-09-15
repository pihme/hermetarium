// vendor-mock is a Messages / Chat Completions origin for coding-agent tests.
// It requires the supervisor-injected key and returns a bash tool call (id -u).
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	http.HandleFunc("/v1/messages", anthropic)
	http.HandleFunc("/v1/chat/completions", openai)
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
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, _ := io.ReadAll(r.Body)
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
