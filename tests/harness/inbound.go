package harness

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// AssertInboundNoVendor checks that a request on the published inbound port
// that names the vendor host is not forwarded to the vendor with the key. With
// the mock vendor, a forwarded request would come back 200 with the mock's
// tool call.
func AssertInboundNoVendor(t *testing.T, inboundURL, vendorHost string) {
	t.Helper()
	body := `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(inboundURL, "/")+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = vendorHost
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("inbound with Host %s: %v", vendorHost, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	got := string(b)
	if strings.Contains(got, "toolu_1") || strings.Contains(got, "call_1") {
		t.Fatalf("inbound with Host %s reached the vendor with the key: %d %s", vendorHost, resp.StatusCode, got)
	}
	if resp.StatusCode/100 == 2 {
		t.Fatalf("inbound with Host %s: expected a refusal, got %d %s", vendorHost, resp.StatusCode, got)
	}
}
