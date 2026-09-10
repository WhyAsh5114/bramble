// demo-service is a throwaway HTTP backend for the client-forwarding
// runbook (brambled/README.md) — not part of the brambled binary, not
// shipped, exists only so the VPS side of the real two-machine test has a
// real, unmodified-by-the-proxy service to answer with something
// identifiable (hostname, request time), proving the response really
// crossed the tunnel rather than being faked locally.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	port := flag.String("port", "9100", "port to listen on, 127.0.0.1 only")
	flag.Parse()

	hostname, _ := os.Hostname()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/task" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"task":       "report-private-service-health",
				"host":       hostname,
				"status":     "healthy",
				"observedAt": time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		fmt.Fprintf(w, "demo-service on %s, request path=%s, served at %s\n", hostname, r.URL.Path, time.Now().UTC().Format(time.RFC3339))
	})

	addr := "127.0.0.1:" + *port
	fmt.Fprintf(os.Stderr, "demo-service listening on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
