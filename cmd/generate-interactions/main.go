package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ken-kai/discord-control-experiments/internal/interactions"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	encoded, err := interactions.CanonicalCatalogJSON()
	if err != nil {
		panic(err)
	}
	testdata := filepath.Join(*root, "internal", "interactions", "testdata")
	docs := filepath.Join(*root, "docs", "interactions")
	if err := os.MkdirAll(testdata, 0o750); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(docs, 0o750); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(testdata, "command-catalog.json"), append(encoded, '\n'), 0o600); err != nil {
		panic(err)
	}
	markdown := "# `/discord-control` command reference\n\nGenerated from `internal/interactions/commands.go`; do not edit manually.\n\n" +
		"- `health` — check local and Discord health.\n- `policy capability:<choice>` — show the caller's effective decision.\n" +
		"- `jobs list` — list this guild's durable jobs.\n- `jobs status run_id:<id>` — inspect one job.\n" +
		"- `jobs cancel run_id:<id>` — prepare a one-time cancellation confirmation.\n- `jobs resume run_id:<id>` — review and approve a recoverable resume.\n\n" +
		"The command is guild-install and guild-context only. Default member permission: Manage Guild.\n"
	if err := os.WriteFile(filepath.Join(docs, "command-reference.md"), []byte(markdown), 0o600); err != nil {
		panic(err)
	}
	fmt.Println("generated interaction command artifacts")
}
