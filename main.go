package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/anish/anthropic-proxy/proxy"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "login" {
		if err := proxy.RunLogin(); err != nil {
			log.Fatalf("login failed: %v", err)
		}
		return
	}

	port := flag.Int("port", 8080, "port to listen on")
	logRequests := flag.Bool("log", false, "log requests to the requests/ directory")
	swapCreds := flag.Bool("swap-creds", false, "replace client credentials with logged-in OAuth token")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	p := proxy.New(proxy.Options{
		LogRequests: *logRequests,
		SwapCreds:   *swapCreds,
	})

	fmt.Printf("anthropic-proxy listening on http://localhost%s\n", addr)
	fmt.Printf("forwarding to https://api.anthropic.com\n")
	if *swapCreds {
		fmt.Println("credential swap: ENABLED (using logged-in OAuth token)")
	}
	fmt.Println()
	fmt.Println("To use with Claude Code, run:")
	fmt.Printf("  ANTHROPIC_BASE_URL=http://localhost%s claude\n\n", addr)

	log.Fatal(http.ListenAndServe(addr, p))
}
