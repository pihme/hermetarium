// Calls the OpenCode terminal gate the way the box does: a page, a repository
// create, the open-web denials, and one pass-through connect.
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
	call(client, http.MethodPost, "https://api.github.com/graphql")
	call(client, http.MethodPatch, "https://api.github.com/repos/octo/hello")
	call(client, http.MethodPost, "https://api.github.com/repos/octo/hello/git/refs")
	// ".." is still ".." when Squid matches urlpath_regex. "%25" is still "%".
	call(client, http.MethodPost, "https://api.github.com/repos/octo/../pulls")
	call(client, http.MethodPost, "https://api.github.com/repos/octo/a%25b/pulls")
	call(client, http.MethodPut, "https://api.github.com/repos/octo/hello/contents/README.md")
	call(client, http.MethodGet, "http://api.github.com/user")
	showUnverifiedCert("pinned.example")
	fmt.Println("PAGE_DONE")
}

func showUnverifiedCert(host string) {
	// Skip verify so a passed-through certificate is still reported.
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		conn, err := tls.Dial("tcp", host+":443", &tls.Config{ServerName: host, InsecureSkipVerify: true})
		if err != nil {
			last = err
			continue
		}
		state := conn.ConnectionState()
		_ = conn.Close()
		if len(state.PeerCertificates) == 0 {
			continue
		}
		c := state.PeerCertificates[0]
		fmt.Printf("CERT %s subject=%s issuer=%s\n", host, c.Subject.CommonName, c.Issuer.CommonName)
		return
	}
	fmt.Println("CERT_FAIL", host, last)
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
