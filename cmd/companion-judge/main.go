// Command companion-judge is the agentic reviewer for Repo Companion.
//
// It is a scaffold: the review pipeline is not implemented yet.
package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.0.0-dev"

const usage = `Usage:
  companion-judge version
  companion-judge help

The review pipeline is not implemented yet.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "companion-judge is in early development; see README.md")
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "companion-judge %s\n", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
