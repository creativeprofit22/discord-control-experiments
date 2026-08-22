package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed testdata/conformance/wire.schema.json
var pinnedWireSchema []byte

//go:embed testdata/conformance/SOURCE.json
var pinnedWireSource []byte

const (
	pinnedWireRevision = "cbd57657ec769b942263a5251afc05959fc4ce58"
	pinnedWireSHA256   = "ef70b61f99b6d2e5e3b46863822eab08dff6a45bedc7a08914e0e5b133f40203"
	pinnedWireUpstream = "https://github.com/modelcontextprotocol/modelcontextprotocol/blob/" + pinnedWireRevision + "/schema/2026-07-28/schema.json"
)

type wireSourceMetadata struct {
	Protocol string `json:"protocol"`
	Upstream string `json:"upstream"`
	Revision string `json:"revision"`
	SHA256   string `json:"sha256"`
}

func compilePinnedWireValidator(t *testing.T) *jsonschema.Resolved {
	t.Helper()

	var source wireSourceMetadata
	if err := json.Unmarshal(pinnedWireSource, &source); err != nil {
		t.Fatalf("decode pinned wire source metadata: %v", err)
	}
	if source.Protocol != ProtocolVersion || source.Revision != pinnedWireRevision || source.Upstream != pinnedWireUpstream || source.SHA256 != pinnedWireSHA256 {
		t.Fatalf("pinned wire source metadata = %#v, want protocol %q at revision %q with the recorded upstream and SHA-256", source, ProtocolVersion, pinnedWireRevision)
	}
	if digest := fmt.Sprintf("%x", sha256.Sum256(pinnedWireSchema)); digest != pinnedWireSHA256 {
		t.Fatalf("pinned wire schema SHA-256 = %s, want %s", digest, pinnedWireSHA256)
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(pinnedWireSchema, &schema); err != nil {
		t.Fatalf("decode pinned wire schema: %v", err)
	}
	// The official schema is a definitions catalog; JSONRPCMessage is its wire envelope.
	schema.Ref = "#/$defs/JSONRPCMessage"
	validator, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("compile pinned wire schema: %v", err)
	}
	if err := validator.Validate(map[string]any{"jsonrpc": "2.0"}); err == nil {
		t.Fatal("pinned wire validator accepted an incomplete JSON-RPC message")
	}
	return validator
}

type captureWriter struct {
	mu          sync.Mutex
	destination *io.PipeWriter
	captured    bytes.Buffer
}

func (w *captureWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.captured.Write(value)
	return w.destination.Write(value)
}
func (w *captureWriter) Close() error { return w.destination.Close() }
func (w *captureWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.captured.Bytes()...)
}

func TestBlackBoxStdioDiscoveryUsesPinnedWireAndPureStdout(t *testing.T) {
	wireValidator := compilePinnedWireValidator(t)
	clientToServerReader, clientToServerWriter := io.Pipe()
	serverToClientReader, serverToClientWriter := io.Pipe()
	capture := &captureWriter{destination: serverToClientWriter}
	operator := &fakeOperator{}
	options := testOptions(operator)
	options.Input = clientToServerReader
	options.Output = capture
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(ctx) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "conformance-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: serverToClientReader, Writer: clientToServerWriter}, nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeResult := session.InitializeResult()
	if initializeResult == nil || initializeResult.ServerInfo == nil {
		t.Fatal("initialize response omitted server implementation metadata")
	}
	if initializeResult.ServerInfo.Name != "discord-control" || initializeResult.ServerInfo.Version != options.ImplementationVersion {
		t.Fatalf("initialize server info = %#v, want name and configured version", initializeResult.ServerInfo)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != len(server.Inventory()) {
		t.Fatalf("listed=%d inventory=%d", len(listed.Tools), len(server.Inventory()))
	}
	previous := ""
	for _, tool := range listed.Tools {
		if tool.Name <= previous {
			t.Fatalf("nondeterministic order %q", tool.Name)
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Fatalf("mutation in read-only inventory: %s", tool.Name)
		}
		previous = tool.Name
	}
	if _, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "unknown_tool", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool was accepted")
	}
	_ = session.Close()
	_ = clientToServerWriter.Close()
	select {
	case <-serverErr:
	case <-time.After(time.Second):
		t.Fatal("server did not stop on EOF")
	}
	lines := bytes.Split(bytes.TrimSpace(capture.Bytes()), []byte{'\n'})
	if len(lines) < 2 {
		t.Fatalf("captured %d protocol lines", len(lines))
	}
	for index, line := range lines {
		var message any
		if err := json.Unmarshal(line, &message); err != nil {
			t.Fatalf("stdout line %d is not pure JSON: %q: %v", index, line, err)
		}
		if err := wireValidator.Validate(message); err != nil {
			t.Fatalf("stdout line %d violates pinned JSON-RPC wire schema: %q: %v", index, line, err)
		}
	}
}
