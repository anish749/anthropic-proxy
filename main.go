package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/anish/anthropic-proxy/proxy"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	logRequests := flag.Bool("log", false, "log requests to the requests/ directory")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	p := proxy.New(*logRequests)

	fmt.Printf("anthropic-proxy listening on http://localhost%s\n", addr)
	fmt.Printf("forwarding to https://api.anthropic.com\n\n")
	fmt.Println("To use with Claude Code, run:")
	fmt.Printf("  ANTHROPIC_BASE_URL=http://localhost%s claude\n\n", addr)

	log.Fatal(http.ListenAndServe(addr, p))
}
