// Command autablectl is the command-line client for a running autable
// server. See docs/cli.md.
package main

import (
	"os"

	"autable/internal/ctl"
)

func main() {
	os.Exit(ctl.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
