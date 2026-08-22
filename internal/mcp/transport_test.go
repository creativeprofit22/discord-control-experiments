package mcp

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBoundedReaderAcceptsOneJSONValuePerLine(t *testing.T) {
	reader := &validatedLineReadCloser{reader: newBuffered(`{"jsonrpc":"2.0"}` + "\n"), maxBytes: 64}
	value, err := io.ReadAll(reader)
	if err != nil || string(value) != `{"jsonrpc":"2.0"}`+"\n" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}
func TestBoundedReaderRejectsMalformedAndOversizedFrames(t *testing.T) {
	for _, input := range []string{"{bad}\n", strings.Repeat("x", 65) + "\n", "\n"} {
		reader := &validatedLineReadCloser{reader: newBuffered(input), maxBytes: 64}
		if _, err := io.ReadAll(reader); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
func TestTransportSupportsOnlyApprovedProtocol(t *testing.T) {
	transport := &boundedStdioTransport{}
	if !transport.SupportsProtocolVersion(ProtocolVersion) || transport.SupportsProtocolVersion("2025-06-18") {
		t.Fatal("protocol version was not pinned")
	}
}
func newBuffered(value string) *bufio.Reader {
	return bufio.NewReaderSize(bytes.NewBufferString(value), 64)
}
