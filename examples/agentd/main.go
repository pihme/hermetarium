// agentd is the HTTP inhabitant for coding-agent examples.
// It holds one session and drives a vendor API (Anthropic or OpenAI-compatible)
// through AGENTD_BASE_URL. Tools run as the process user (root in the image).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func main() {
	s := &session{
		vendor: getenv("AGENTD_VENDOR", "anthropic"),
		base:   strings.TrimRight(getenv("AGENTD_BASE_URL", "http://claude.hermetarium.test"), "/"),
		key:    getenv("AGENTD_API_KEY", "placeholder"),
	}
	http.HandleFunc("/", s.serve)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type session struct {
	mu      sync.Mutex
	vendor  string
	base    string
	key     string
	history []map[string]any
}

func (s *session) serve(w http.ResponseWriter, r *http.Request) {
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
	text, err := s.turn(prompt)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, text)
}

func (s *session) turn(prompt string) (string, error) {
	s.history = append(s.history, map[string]any{"role": "user", "content": prompt})
	for i := 0; i < 8; i++ {
		if s.vendor == "openai" {
			text, err := s.roundOpenAI()
			if err != nil {
				return "", err
			}
			if text != "" {
				return text, nil
			}
			continue
		}
		text, err := s.roundAnthropic()
		if err != nil {
			return "", err
		}
		if text != "" {
			return text, nil
		}
	}
	return "", fmt.Errorf("agent loop exceeded tool rounds")
}

func (s *session) roundAnthropic() (string, error) {
	payload := map[string]any{
		"model":      "mock",
		"max_tokens": 1024,
		"messages":   s.history,
		"tools": []map[string]any{{
			"name":        "bash",
			"description": "Run a shell command",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required":   []string{"command"},
			},
		}},
	}
	raw, err := s.post(s.base+"/v1/messages", payload, map[string]string{
		"x-api-key":         s.key,
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("anthropic decode: %s %s", err, raw)
	}
	var blocks []map[string]any
	var out strings.Builder
	used := false
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			out.WriteString(c.Text)
			blocks = append(blocks, map[string]any{"type": "text", "text": c.Text})
		case "tool_use":
			used = true
			cmd := toolCommand(c.Input)
			result := runBash(cmd)
			blocks = append(blocks, map[string]any{
				"type": "tool_result", "tool_use_id": c.ID, "content": result,
			})
		}
	}
	if used {
		s.history = append(s.history, map[string]any{"role": "assistant", "content": resp.Content})
		s.history = append(s.history, map[string]any{"role": "user", "content": blocks})
		return "", nil
	}
	s.history = append(s.history, map[string]any{"role": "assistant", "content": out.String()})
	return out.String(), nil
}

func (s *session) roundOpenAI() (string, error) {
	payload := map[string]any{
		"model":    "mock",
		"messages": s.history,
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "bash",
				"description": "Run a shell command",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"command": map[string]any{"type": "string"}},
					"required":   []string{"command"},
				},
			},
		}},
	}
	raw, err := s.post(s.base+"/v1/chat/completions", payload, map[string]string{
		"Authorization": "Bearer " + s.key,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("openai decode: %s %s", err, raw)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai empty choices: %s", raw)
	}
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		s.history = append(s.history, map[string]any{
			"role": "assistant", "content": msg.Content, "tool_calls": msg.ToolCalls,
		})
		for _, tc := range msg.ToolCalls {
			cmd := toolCommand([]byte(tc.Function.Arguments))
			s.history = append(s.history, map[string]any{
				"role": "tool", "tool_call_id": tc.ID, "content": runBash(cmd),
			})
		}
		return "", nil
	}
	s.history = append(s.history, map[string]any{"role": "assistant", "content": msg.Content})
	return msg.Content, nil
}

func (s *session) post(url string, payload any, headers map[string]string) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s %s", url, resp.Status, raw)
	}
	return raw, nil
}

func toolCommand(input json.RawMessage) string {
	var obj struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &obj)
	return obj.Command
}

func runBash(command string) string {
	if command == "" {
		return "empty command"
	}
	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out) + "\n" + err.Error())
	}
	return strings.TrimSpace(string(out))
}
