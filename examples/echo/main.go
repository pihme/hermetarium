// Echo inhabitant: HTTP server that returns the request body.
// Later examples replace this process with an agent that accepts commands.
package main

import (
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", echo)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func echo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, r.Body)
}
