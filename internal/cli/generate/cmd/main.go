package main

import (
	"flag"
	"fmt"
	"os"

	generate "github.com/ken-kai/discord-control-experiments/internal/cli/generate"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := generate.Generate(*root); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
