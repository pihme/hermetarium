// Calls the model stand-in and prints each response line as it arrives.
package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	body := `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	client := &http.Client{Timeout: 15 * time.Second}
	// Squid probes the peer at startup and ignores it until that probe succeeds.
	// The mock starts after Squid, so the first tries can be a 502.
	var resp *http.Response
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodPost, "http://model.hermetarium.test/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			fail(err)
		}
		req.Header.Set("Authorization", "Bearer placeholder-key")
		req.Header.Set("Content-Type", "application/json")
		got, err := client.Do(req)
		if err != nil {
			if attempt == 9 {
				fail(err)
			}
			continue
		}
		if got.StatusCode == http.StatusOK || attempt == 9 {
			resp = got
			break
		}
		got.Body.Close()
	}
	defer resp.Body.Close()
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	sc := bufio.NewScanner(resp.Body)
	var prev time.Time
	for sc.Scan() {
		now := time.Now()
		gap := int64(0)
		if !prev.IsZero() {
			gap = now.Sub(prev).Milliseconds()
		}
		prev = now
		fmt.Printf("CHUNK %d %s\n", gap, sc.Text())
	}
	if err := sc.Err(); err != nil {
		fail(err)
	}
	fmt.Println("STREAM_DONE")
}

func fail(err error) {
	fmt.Println("CLIENT_FAIL", err)
	os.Exit(1)
}
