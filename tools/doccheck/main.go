package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type manifest struct {
	SchemaVersion string    `json:"schemaVersion"`
	Commands      []command `json:"commands"`
}
type command struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

func main() {
	manifestPath := flag.String("manifest", "docs/testing/commands.json", "documented command manifest")
	binary := flag.String("binary", "", "built discord-control binary")
	evidence := flag.String("evidence", ".tools/documented-drills.json", "new drill evidence path")
	flag.Parse()
	if *binary == "" {
		fatal(errors.New("binary is required"))
	}
	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal(err)
	}
	var document manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.SchemaVersion != "discord-control.documented-commands/v1" || len(document.Commands) == 0 || len(document.Commands) > 32 {
		fatal(errors.New("documented command manifest is malformed or unsupported"))
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		fatal(err)
	}
	for _, item := range document.Commands {
		if item.Name == "" || len(item.Name) > 64 || len(item.Argv) == 0 || len(item.Argv) > 16 {
			fatal(errors.New("documented command entry is invalid"))
		}
		argv := make([]string, len(item.Argv))
		for index, value := range item.Argv {
			switch value {
			case "{binary}":
				value = *binary
			case "{go}":
				value = goBinary
			case "{evidence}":
				value = *evidence
			}
			argv[index] = value
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		process := exec.CommandContext(ctx, argv[0], argv[1:]...)
		output, runErr := process.CombinedOutput()
		cancel()
		if runErr != nil {
			fatal(fmt.Errorf("documented command %s failed: %w: %.4096s", item.Name, runErr, output))
		}
	}
}

func fatal(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
