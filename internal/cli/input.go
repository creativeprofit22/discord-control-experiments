package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	DefaultInputLimit     int64 = 64 << 10
	MessageInputLimit     int64 = 256 << 10
	DiagnosticBundleLimit int64 = 4 << 20
)

// DecodeStrictJSON accepts exactly one UTF-8 JSON value and rejects duplicate
// object keys, unknown struct fields, trailing data, and over-limit payloads.
func DecodeStrictJSON(reader io.Reader, maximum int64, destination any) error {
	if reader == nil || destination == nil || maximum <= 0 {
		return &ParseError{Code: "invalid_input", Detail: "invalid JSON input source"}
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return &ParseError{Code: "read_input", Detail: "could not read JSON input"}
	}
	if int64(len(payload)) > maximum {
		return &ParseError{Code: "input_too_large", Detail: "JSON input exceeds command limit"}
	}
	if !utf8.Valid(payload) {
		return &ParseError{Code: "invalid_utf8", Detail: "JSON input must be UTF-8"}
	}
	if err := rejectDuplicateKeys(payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return &ParseError{Code: "malformed_json", Detail: "JSON input is malformed or contains unknown fields"}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &ParseError{Code: "trailing_json", Detail: "JSON input must contain exactly one value"}
	}
	return nil
}

type jsonFrame struct {
	object, expectKey bool
	keys              map[string]struct{}
}

func rejectDuplicateKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	stack := []jsonFrame{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return &ParseError{Code: "malformed_json", Detail: "JSON input is malformed"}
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				stack = append(stack, jsonFrame{object: true, expectKey: true, keys: map[string]struct{}{}})
			case '[':
				stack = append(stack, jsonFrame{})
			case '}', ']':
				if len(stack) == 0 {
					return &ParseError{Code: "malformed_json", Detail: "JSON structure is invalid"}
				}
				stack = stack[:len(stack)-1]
				markValueConsumed(stack)
			}
		case string:
			if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
				current := &stack[len(stack)-1]
				if _, exists := current.keys[value]; exists {
					return &ParseError{Code: "duplicate_key", Detail: "JSON object contains a duplicate key"}
				}
				current.keys[value] = struct{}{}
				current.expectKey = false
			} else {
				markValueConsumed(stack)
			}
		default:
			markValueConsumed(stack)
		}
	}
}

func markValueConsumed(stack []jsonFrame) {
	if len(stack) > 0 && stack[len(stack)-1].object && !stack[len(stack)-1].expectKey {
		stack[len(stack)-1].expectKey = true
	}
}

// OpenInput resolves files beneath root and consumes stdin only for an explicit
// "-" sentinel. Symlinks are rejected to prevent contained-path races.
func OpenInput(root, name string, stdin io.Reader) (io.ReadCloser, error) {
	if name == "-" {
		if stdin == nil {
			return nil, &ParseError{Code: "stdin_unavailable", Detail: "stdin was explicitly requested but is unavailable"}
		}
		return io.NopCloser(stdin), nil
	}
	path, err := ContainedPath(root, name, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, &ParseError{Code: "invalid_input_file", Detail: "input must be a regular contained file"}
	}
	absoluteRoot, _ := filepath.Abs(root)
	relative, relativeErr := filepath.Rel(absoluteRoot, path)
	if relativeErr != nil {
		return nil, &ParseError{Code: "invalid_path", Detail: "input path could not be contained"}
	}
	file, err := os.OpenInRoot(absoluteRoot, relative)
	if err != nil {
		return nil, &ParseError{Code: "open_input", Detail: "input file could not be opened"}
	}
	return file, nil
}

func ContainedPath(root, name string, forWrite bool) (string, error) {
	if root == "" || name == "" || name == "-" {
		return "", &ParseError{Code: "invalid_path", Detail: "path is empty or reserved"}
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", &ParseError{Code: "invalid_path", Detail: "path root could not be resolved"}
	}
	cleanRoot := filepath.Clean(absoluteRoot)
	candidate := name
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(cleanRoot, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", &ParseError{Code: "invalid_path", Detail: "path could not be resolved"}
	}
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(cleanRoot, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", &ParseError{Code: "path_escape", Detail: "path leaves the configured root"}
	}
	parent := filepath.Dir(candidate)
	if resolved, resolveErr := filepath.EvalSymlinks(parent); resolveErr == nil {
		relative, err = filepath.Rel(cleanRoot, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", &ParseError{Code: "path_symlink", Detail: "path parent resolves outside the configured root"}
		}
	} else if !forWrite {
		return "", &ParseError{Code: "invalid_path", Detail: "path parent does not exist"}
	}
	return candidate, nil
}

func AtomicWrite(root, name string, mode os.FileMode, write func(io.Writer) error) error {
	path, err := ContainedPath(root, name, true)
	if err != nil {
		return err
	}
	if mode&0o077 != 0 {
		return &ParseError{Code: "unsafe_permissions", Detail: "output permissions must not grant group or world access"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return &ParseError{Code: "path_symlink", Detail: "output cannot replace a symlink"}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".discord-control-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("restrict temporary output: %w", err)
	}
	buffered := bufio.NewWriter(temporary)
	if err := write(buffered); err != nil {
		return err
	}
	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace output atomically: %w", err)
	}
	committed = true
	return nil
}
