// Command hello is the golden path's test fixture: the smallest app build-app.yml accepts (one
// image, <app>-web, since there's no cmd/ folder).
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Version is set at build time (build arg VERSION).
var Version = "dev"

func handler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "hello from %s\n", Version)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
