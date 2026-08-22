package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type boundedStdioTransport struct {
	reader   io.ReadCloser
	writer   io.WriteCloser
	maxBytes int
}

func (t *boundedStdioTransport) SupportsProtocolVersion(version string) bool {
	return version == ProtocolVersion
}
func (t *boundedStdioTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	if t.reader == nil || t.writer == nil || t.maxBytes < 1 {
		return nil, errors.New("invalid bounded stdio transport")
	}
	inner := &sdk.IOTransport{Reader: &validatedLineReadCloser{reader: bufio.NewReaderSize(t.reader, t.maxBytes+1), closer: t.reader, maxBytes: t.maxBytes}, Writer: &lockedWriteCloser{writer: t.writer}}
	return inner.Connect(ctx)
}

type validatedLineReadCloser struct {
	reader   *bufio.Reader
	closer   io.Closer
	maxBytes int
	pending  []byte
	terminal error
}

func (r *validatedLineReadCloser) Read(destination []byte) (int, error) {
	if len(r.pending) > 0 {
		count := copy(destination, r.pending)
		r.pending = r.pending[count:]
		return count, nil
	}
	if r.terminal != nil {
		return 0, r.terminal
	}
	line, err := r.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		r.terminal = errors.New("MCP message exceeds configured limit")
		return 0, r.terminal
	}
	if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
		r.terminal = err
		return 0, err
	}
	trimmed := line
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\r' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if len(trimmed) == 0 || len(trimmed) > r.maxBytes || !json.Valid(trimmed) {
		r.terminal = errors.New("MCP message is not one valid JSON value")
		return 0, r.terminal
	}
	r.pending = append(append([]byte(nil), trimmed...), '\n')
	if errors.Is(err, io.EOF) {
		r.terminal = io.EOF
	}
	count := copy(destination, r.pending)
	r.pending = r.pending[count:]
	return count, nil
}
func (r *validatedLineReadCloser) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type lockedWriteCloser struct {
	mu     sync.Mutex
	writer io.WriteCloser
}

func (w *lockedWriteCloser) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(value)
}
func (w *lockedWriteCloser) Close() error { return nil }

type nopReadCloser struct{ io.Reader }

func (nopReadCloser) Close() error { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
