// github-mock records the request line and answers 200.
// Plain HTTP, plus TLS when CERT_DIR holds one leaf per server name.
// The gate copies the origin certificate it sees, so one leaf for every
// name would make a bumped host show the wrong subject.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	handler := http.HandlerFunc(record)
	errCh := make(chan error, 2)
	go func() {
		errCh <- http.ListenAndServe(getenv("HTTP_ADDR", ":18083"), handler)
	}()
	if dir := os.Getenv("CERT_DIR"); dir != "" {
		cfg, err := certConfig(dir)
		if err != nil {
			log.Fatal(err)
		}
		ln, err := tls.Listen("tcp", getenv("TLS_ADDR", ":443"), cfg)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			errCh <- http.Serve(ln, handler)
		}()
	}
	log.Fatal(<-errCh)
}

func certConfig(dir string) (*tls.Config, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var loaded []tls.Certificate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".crt") {
			continue
		}
		base := strings.TrimSuffix(name, ".crt")
		c, err := tls.LoadX509KeyPair(filepath.Join(dir, name), filepath.Join(dir, base+".key"))
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, c)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("no certs in %s", dir)
	}
	byName := map[string]*tls.Certificate{}
	for i := range loaded {
		leaf, err := x509.ParseCertificate(loaded[i].Certificate[0])
		if err != nil {
			return nil, err
		}
		names := append([]string{}, leaf.DNSNames...)
		if leaf.Subject.CommonName != "" {
			names = append(names, leaf.Subject.CommonName)
		}
		for _, n := range names {
			byName[n] = &loaded[i]
		}
	}
	fallback := &loaded[0]
	return &tls.Config{
		GetCertificate: func(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if c, ok := byName[hi.ServerName]; ok {
				return c, nil
			}
			return fallback, nil
		},
	}, nil
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func record(w http.ResponseWriter, r *http.Request) {
	path := os.Getenv("MOCK_REPORT")
	if path != "" {
		line := fmt.Sprintf("method=%s path=%s host=%s auth=%s\n", r.Method, r.RequestURI, r.Host, strings.Join(r.Header.Values("Authorization"), ", "))
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
