package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	maxAuditRecordBytes = 64 << 10
	maxAuditFieldBytes  = 1024
	maxIntentFields     = 64
)

var (
	ErrInvalidPath = errors.New("invalid audit path")
	ErrIntegrity   = errors.New("audit integrity failure")
	ErrRecordLimit = errors.New("audit record exceeds limit")
)

type Sanitizer func(string) (sanitized string, changed bool, err error)

type recordBody struct {
	Sequence     uint64            `json:"sequence"`
	PreviousHash string            `json:"previousHash"`
	Event        domain.AuditEvent `json:"event"`
}

type record struct {
	recordBody
	Hash string `json:"hash"`
}

type head struct {
	Sequence uint64 `json:"sequence"`
	Hash     string `json:"hash"`
}

type JSONL struct {
	mu        sync.Mutex
	root      *os.Root
	file      *os.File
	path      string
	headPath  string
	maxBytes  int64
	size      int64
	sequence  uint64
	lastHash  string
	sanitizer Sanitizer
}

func Open(baseDir, relativePath string, maxBytes int64, sanitizer Sanitizer) (*JSONL, error) {
	cleanRelative, valid := cleanRelativePath(relativePath)
	if maxBytes < maxAuditRecordBytes || !valid {
		return nil, ErrInvalidPath
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, ErrInvalidPath
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return nil, ErrInvalidPath
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, ErrInvalidPath
	}
	parent := filepath.Dir(cleanRelative)
	if parent != "." {
		if err := root.MkdirAll(parent, 0o700); err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("create audit directory: %w", err)
		}
	}
	if info, err := root.Lstat(cleanRelative); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxBytes {
			_ = root.Close()
			return nil, ErrInvalidPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = root.Close()
		return nil, ErrInvalidPath
	}
	file, err := root.OpenFile(cleanRelative, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("open audit: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = root.Close()
		return nil, fmt.Errorf("restrict audit: %w", err)
	}
	sink := &JSONL{root: root, file: file, path: cleanRelative, headPath: cleanRelative + ".head", maxBytes: maxBytes, sanitizer: sanitizer}
	if err := sink.verify(); err != nil {
		_ = file.Close()
		_ = root.Close()
		return nil, err
	}
	return sink, nil
}

func cleanRelativePath(value string) (string, bool) {
	if value == "" || value == "." || filepath.IsAbs(value) {
		return "", false
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	clean := filepath.Clean(filepath.FromSlash(normalized))
	return clean, clean != "." && !filepath.IsAbs(clean)
}

func (j *JSONL) verify() error {
	reader, err := j.root.Open(j.path)
	if err != nil {
		return fmt.Errorf("verify audit: %w", err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxAuditRecordBytes+1)
	var sequence uint64
	previous := ""
	var size int64
	for scanner.Scan() {
		line := scanner.Bytes()
		size += int64(len(line) + 1)
		if len(line) == 0 || len(line) > maxAuditRecordBytes {
			return ErrIntegrity
		}
		var item record
		if err := json.Unmarshal(line, &item); err != nil {
			return ErrIntegrity
		}
		if item.Sequence != sequence+1 || item.PreviousHash != previous || !validHash(item.Hash) {
			return ErrIntegrity
		}
		expected, err := hashBody(item.recordBody)
		if err != nil || subtleStringMismatch(expected, item.Hash) {
			return ErrIntegrity
		}
		sequence, previous = item.Sequence, item.Hash
	}
	if err := scanner.Err(); err != nil {
		return ErrIntegrity
	}
	if size > j.maxBytes {
		return ErrRecordLimit
	}
	if info, err := j.root.Stat(j.path); err != nil || info.Size() != size && info.Size() != 0 {
		return ErrIntegrity
	}
	if err := verifyHead(j.root, j.headPath, sequence, previous); err != nil {
		return err
	}
	j.sequence, j.lastHash, j.size = sequence, previous, size
	return nil
}

func verifyHead(root *os.Root, path string, sequence uint64, hash string) error {
	data, err := root.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if sequence == 0 {
			return nil
		}
		return ErrIntegrity
	}
	if err != nil || len(data) > 4096 {
		return ErrIntegrity
	}
	var stored head
	if json.Unmarshal(data, &stored) != nil || stored.Sequence != sequence || subtleStringMismatch(stored.Hash, hash) {
		return ErrIntegrity
	}
	return nil
}

func (j *JSONL) Append(_ context.Context, event domain.AuditEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return errors.New("audit closed")
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	if j.sanitizer != nil {
		encoded, err := json.Marshal(event)
		if err != nil {
			return ErrIntegrity
		}
		sanitized, _, err := j.sanitizer(string(encoded))
		if err != nil || json.Unmarshal([]byte(sanitized), &event) != nil {
			return ErrIntegrity
		}
	}
	body := recordBody{Sequence: j.sequence + 1, PreviousHash: j.lastHash, Event: event}
	hash, err := hashBody(body)
	if err != nil {
		return err
	}
	line, err := json.Marshal(record{recordBody: body, Hash: hash})
	if err != nil || len(line)+1 > maxAuditRecordBytes {
		return ErrRecordLimit
	}
	if j.size+int64(len(line)+1) > j.maxBytes {
		return ErrRecordLimit
	}
	if _, err := j.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("sync audit: %w", err)
	}
	if err := writeHead(j.root, j.headPath, head{Sequence: body.Sequence, Hash: hash}); err != nil {
		return err
	}
	j.sequence, j.lastHash, j.size = body.Sequence, hash, j.size+int64(len(line)+1)
	return nil
}

func writeHead(root *os.Root, path string, value head) error {
	encoded, _ := json.Marshal(value)
	temporary := path + ".tmp"
	if err := root.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("write audit head: %w", err)
	}
	if file, err := root.OpenFile(temporary, os.O_WRONLY, 0); err != nil {
		return err
	} else if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	} else {
		_ = file.Close()
	}
	if err := root.Rename(temporary, path); err != nil {
		_ = root.Remove(temporary)
		return fmt.Errorf("replace audit head: %w", err)
	}
	return root.Chmod(path, 0o600)
}

func hashBody(body recordBody) (string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validHash(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && err == nil
}
func subtleStringMismatch(a, b string) bool { return a != b }

func validateEvent(event domain.AuditEvent) error {
	encoded, err := json.Marshal(event)
	if err != nil || len(encoded) > maxAuditRecordBytes/2 || len(event.Intent) > maxIntentFields {
		return ErrRecordLimit
	}
	values := []string{string(event.ActorID), string(event.ApproverID), string(event.Adapter), string(event.GuildID), string(event.Operation), string(event.Capability), string(event.Reason), string(event.ApprovalID), string(event.Mode), string(event.DestructiveClass), event.Result, string(event.CorrelationID), string(event.ParentCorrelationID), event.AuditReason, event.OperationHash}
	for _, value := range values {
		if len(value) > maxAuditFieldBytes || strings.ContainsAny(value, "\r\n\x00") {
			return ErrRecordLimit
		}
	}
	for key, value := range event.Intent {
		if len(key) > 128 || len(value) > maxAuditFieldBytes || strings.ContainsAny(key+value, "\r\n\x00") {
			return ErrRecordLimit
		}
	}
	return nil
}

func (j *JSONL) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	fileErr := j.file.Close()
	rootErr := j.root.Close()
	j.file = nil
	j.root = nil
	return errors.Join(fileErr, rootErr)
}

func (j *JSONL) Head() (uint64, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.sequence, j.lastHash
}

var _ io.Closer = (*JSONL)(nil)
