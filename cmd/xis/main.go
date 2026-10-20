// Command xis shows what an AI coding agent did to your code and why.
package main

import (
	"os"

	"github.com/aaxis-em/xis/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
