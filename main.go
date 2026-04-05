package main

import "github.com/anish/anthropic-proxy/cmd"

// version is set at build time via ldflags.
var version = "dev"

func main() {
	cmd.Execute(version)
}
