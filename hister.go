package main

import (
	"os"

	"github.com/asciimoo/hister/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(cmd.ProcessExitCode(err))
	}
}
