package main

import (
	"os"

	"github.com/mauroociappina/ayrton/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}