package application

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestCommandContractsAreCompleteDeterministicAndSealed(t *testing.T) {
	contracts := CommandContracts()
	if len(contracts) != len(CommandCatalog()) {
		t.Fatalf("contracts=%d commands=%d", len(contracts), len(CommandCatalog()))
	}
	names := make([]string, len(contracts))
	for index, contract := range contracts {
		names[index] = contract.Name
		if contract.InputType.Kind() != reflect.Struct || contract.ResultType == nil || len(contract.Toolsets) == 0 || contract.Description == "" {
			t.Fatalf("incomplete contract %q: %#v", contract.Name, contract)
		}
		command, err := NewContractCommand(contract.Name, domain.GuildID("123"))
		if err != nil {
			t.Fatalf("construct %q: %v", contract.Name, err)
		}
		if command.CommandName() != contract.Name || command.GuildScope() != "123" {
			t.Fatalf("unsealed contract command %q", contract.Name)
		}
		if _, ok := LookupCommandSpec(contract.Name); !ok {
			t.Fatalf("contract bypasses operator registry: %q", contract.Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("contracts are not deterministic: %#v", names)
	}
}

func TestEveryCommandHasExactlyOneContract(t *testing.T) {
	seen := map[string]int{}
	for _, contract := range CommandContracts() {
		seen[contract.Name]++
	}
	for _, spec := range CommandCatalog() {
		if seen[spec.Name] != 1 {
			t.Fatalf("command %q has %d contracts", spec.Name, seen[spec.Name])
		}
	}
}

func TestDynamicDestructiveClassificationAndDescriptions(t *testing.T) {
	permissions := domain.PermissionBits("8")
	roles := []domain.RoleID{"3"}
	tests := []struct {
		name        string
		destructive domain.Command
		plain       domain.Command
		field       string
	}{
		{name: "create_role", destructive: domain.CreateRole{Permissions: &permissions}, plain: domain.CreateRole{}, field: "permissions"},
		{name: "modify_role", destructive: domain.ModifyRole{Permissions: &permissions}, plain: domain.ModifyRole{}, field: "permissions"},
		{name: "modify_member", destructive: domain.ModifyMember{Roles: &roles}, plain: domain.ModifyMember{}, field: "roles"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract, ok := LookupCommandContract(test.name)
			if !ok {
				t.Fatal("contract missing")
			}
			if got := DestructiveClassForCommand(test.destructive, contract.Spec); got != domain.DestructiveRoleEscalation {
				t.Fatalf("destructive class = %q", got)
			}
			if got := DestructiveClassForCommand(test.plain, contract.Spec); got != domain.DestructiveNone {
				t.Fatalf("plain class = %q", got)
			}
			want := "destructive approval required for apply when " + test.field + " is provided"
			if !strings.Contains(contract.Description, want) || !MayRequireDestructiveApproval(contract.Spec) {
				t.Fatalf("description = %q", contract.Description)
			}
		})
	}
}
