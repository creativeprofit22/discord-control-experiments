package mcp

import (
	"errors"
	"log/slog"
	"testing"
)

func TestToolRegistrationFailsClosedAndSorts(t *testing.T) {
	server, err := New(slog.Default(), true)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := []ToolDescriptor{{Name: "zeta"}, {Name: "alpha"}}
	if err := server.RegisterTools(descriptors, func(ToolDescriptor) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	names := server.ToolNames()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("names = %#v", names)
	}
	if err := server.RegisterTools(descriptors, func(ToolDescriptor) (bool, error) { return false, errors.New("classifier failed") }); err == nil {
		t.Fatal("expected classifier failure")
	}
	if names := server.ToolNames(); len(names) != 0 {
		t.Fatalf("fail-open tools = %#v", names)
	}
	if err := server.RegisterTools([]ToolDescriptor{{Name: "same"}, {Name: "same"}}, func(ToolDescriptor) (bool, error) { return true, nil }); err == nil {
		t.Fatal("expected duplicate rejection")
	}
}
