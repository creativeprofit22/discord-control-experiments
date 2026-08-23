package interactions

import (
	"os"
	"testing"
)

func TestCommandCatalogValidAndGolden(t *testing.T) {
	catalog := CommandCatalog()
	if err := ValidateCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	encoded, err := CanonicalCatalogJSON()
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/command-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(golden) != string(append(encoded, '\n')) {
		t.Fatal("generated command catalog drift")
	}
	if len(catalog) != 1 || catalog[0].DMPermission || len(catalog[0].IntegrationTypes) != 1 || catalog[0].IntegrationTypes[0] != 0 {
		t.Fatal("command escaped guild-only catalog")
	}
}
func TestCatalogRejectsDuplicates(t *testing.T) {
	catalog := CommandCatalog()
	catalog[0].Options = append(catalog[0].Options, catalog[0].Options[0])
	if err := ValidateCatalog(catalog); err == nil {
		t.Fatal("duplicate option accepted")
	}
}
