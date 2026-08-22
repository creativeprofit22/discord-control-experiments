package architecture

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func TestProjectImportDAG(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command("go", "list", "-json", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(stdout)
	var packages []listedPackage
	for {
		var item listedPackage
		err := decoder.Decode(&item)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, item)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}

	const module = "github.com/ken-kai/discord-control-experiments/internal/"
	adapters := map[string]bool{"discord": true, "mcp": true, "interactions": true, "gateway": true, "jobs": true}
	for _, item := range packages {
		if !strings.HasPrefix(item.ImportPath, module) {
			continue
		}
		owner := strings.Split(strings.TrimPrefix(item.ImportPath, module), "/")[0]
		for _, imported := range item.Imports {
			if !strings.HasPrefix(imported, module) {
				continue
			}
			dependency := strings.Split(strings.TrimPrefix(imported, module), "/")[0]
			switch {
			case owner == "domain":
				t.Errorf("domain imports project package %s", imported)
			case owner == "application" && dependency != "domain" && dependency != "reconcile":
				t.Errorf("application imports disallowed project package %s", imported)
			case adapters[owner] && adapters[dependency]:
				t.Errorf("adapter %s imports adapter %s", owner, dependency)
			case (owner == "config" || owner == "observability" || owner == "buildinfo") && (dependency == "discord" || dependency == "policy" || dependency == "reconcile" || adapters[dependency]):
				t.Errorf("foundation package %s imports disallowed package %s", owner, dependency)
			}
		}
	}
}
