package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	s := &session{vendor: "anthropic", base: "http://example", key: "k"}
	w := httptest.NewRecorder()
	s.serve(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestToolCommand(t *testing.T) {
	if got := toolCommand([]byte(`{"command":"id -u"}`)); got != "id -u" {
		t.Fatalf("%q", got)
	}
}
