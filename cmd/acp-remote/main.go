// Command acp-remote is a universal remote ACP proxy.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: acp-remote <command> [flags]")
		fmt.Fprintln(os.Stderr, "  stdio  --provider local --command <cmd>")
		fmt.Fprintln(os.Stderr, "  serve  --listen <addr> --token <token>")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "stdio":
		err = runStdio(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
