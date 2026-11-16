// Calls the OpenCode terminal gate the way the box does: a page, a repository
// create, and a delete. The status lines are what the test reads.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	pool, _ := x509.SystemCertPool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	for _, path := range []string{"/etc/ssl/hermetarium-mitm.crt", "/usr/local/share/ca-certificates/hermetarium-mitm.crt"} {
		b, err := os.ReadFile(path)
		if err == nil {
			pool.AppendCertsFromPEM(b)
		}
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	call(client, http.MethodGet, "https://other.example/hello")
	call(client, http.MethodPost, "https://api.github.com/user/repos")
	call(client, http.MethodDelete, "https://api.github.com/repos/octo/hello")
	fmt.Println("PAGE_DONE")
}

func call(client *http.Client, method, rawURL string) {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		req, err := http.NewRequest(method, rawURL, strings.NewReader("{}"))
		if err != nil {
			fmt.Println("HTTP_FAIL", method, rawURL, err)
			return
		}
		req.Header.Set("Authorization", "Bearer box-token")
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 && attempt < 7 {
			continue
		}
		fmt.Printf("HTTP %s %s %d\n", method, rawURL, resp.StatusCode)
		return
	}
	fmt.Println("HTTP_FAIL", method, rawURL, last)
}
