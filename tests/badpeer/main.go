package main

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
)

// badpeer is an HTTPS vendor. It records each request, including Authorization,
// so a test can see whether the gate sent the key.
func main() {
	cert, err := tls.LoadX509KeyPair(os.Getenv("CERT"), os.Getenv("KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:"+os.Getenv("PORT"), &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		log.Fatal(err)
	}
	err = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.OpenFile(os.Getenv("LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(f, r.Method+" "+r.Host+" "+r.Header.Get("Authorization")+"\n")
		_ = f.Close()
		w.WriteHeader(http.StatusOK)
	}))
	if err != nil {
		log.Fatal(err)
	}
}
