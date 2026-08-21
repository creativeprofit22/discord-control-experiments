package cli

import (
	"errors"
	"fmt"
	"io"
)

type Command struct {
	Name       string
	ConfigPath string
}

func Parse(args []string) (Command, error) {
	if len(args) == 1 && args[0] == "version" {
		return Command{Name: "version"}, nil
	}
	if len(args) == 3 && args[0] == "run" && args[1] == "--config" && args[2] != "" {
		return Command{Name: "run", ConfigPath: args[2]}, nil
	}
	return Command{}, errors.New("usage: discord-control version | run --config PATH")
}

func WriteVersion(w io.Writer, version, commit string) error {
	_, err := fmt.Fprintf(w, "%s (%s)\n", version, commit)
	return err
}
