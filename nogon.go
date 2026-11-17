package main

import (
	"os"

	"github.com/asciimoo/nogon/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(cmd.ExitCode(err))
	}
}
