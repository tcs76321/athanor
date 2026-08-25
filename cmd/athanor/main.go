// Command athanor is the daemon entry point.
//
// M0-T1 placeholder: this binary exists so `go build ./...` succeeds from a
// clean clone. Subsystem wiring (config, store, logging) lands in M0-T2..T5;
// a runnable daemon arrives with M0-T7 (/healthz).
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("athanor", version)
		return
	}
	fmt.Printf("athanor %s — daemon skeleton; subsystems arrive in M0-T2..T7\n", version)
}
