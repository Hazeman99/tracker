// Command tracker-api is the API server and its admin CLI.
package main

import (
	"os"

	"tracker/api/internal/cli"
)

func main() {
	restrictUmask()
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
