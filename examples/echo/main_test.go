package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEcho(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ping"))
	w := httptest.NewRecorder()
	echo(w, req)
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || string(body) != "ping" {
		t.Fatalf("status %d body %q", res.StatusCode, body)
	}
}
