package state

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	BackupSchemaVersion = 1
	MaxBackupEntries    = 10_000
	MaxBackupFileBytes  = 64 << 20
	MaxBackupBytes      = 512 << 20
)

var archiveTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

type BackupOptions struct {
	ConfigDirectory string
	ConfigPath      string
	WorkflowPaths   []string
	AuditPath       string
	StateDirectory  string
	Destination     string
}

type BackupEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type BackupManifest struct {
	SchemaVersion int           `json:"schemaVersion"`
	Entries       []BackupEntry `json:"entries"`
}

type capturedFile struct {
	entry BackupEntry
	data  []byte
}

func CreateBackup(options BackupOptions) (BackupManifest, error) {
	root, err := filepath.Abs(options.ConfigDirectory)
	if err != nil || options.ConfigPath == "" || options.Destination == "" {
		return BackupManifest{}, errors.New("backup config directory, config path, and destination are required")
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return BackupManifest{}, err
	}
	defer rootHandle.Close()
	roots := append([]string{options.ConfigPath}, options.WorkflowPaths...)
	if options.AuditPath != "" {
		roots = append(roots, options.AuditPath)
		auditHead := options.AuditPath + ".head"
		absoluteHead := auditHead
		if !filepath.IsAbs(absoluteHead) {
			absoluteHead = filepath.Join(root, absoluteHead)
		}
		if _, statErr := os.Stat(absoluteHead); statErr == nil {
			roots = append(roots, auditHead)
		}
	}
	if options.StateDirectory != "" {
		roots = append(roots, options.StateDirectory)
	}
	seen := map[string]struct{}{}
	files := make([]capturedFile, 0)
	for _, configured := range roots {
		absolute := configured
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, configured)
		}
		absolute, err = filepath.Abs(filepath.Clean(absolute))
		if err != nil || !contained(root, absolute) {
			return BackupManifest{}, fmt.Errorf("backup source escapes configuration directory: %q", configured)
		}
		if err := collectBackupPath(rootHandle, root, absolute, seen, &files); err != nil {
			return BackupManifest{}, err
		}
	}
	if len(files) == 0 || len(files) > MaxBackupEntries {
		return BackupManifest{}, errors.New("backup entry count is outside supported bounds")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].entry.Path < files[j].entry.Path })
	manifest := BackupManifest{SchemaVersion: BackupSchemaVersion, Entries: make([]BackupEntry, len(files))}
	for i := range files {
		manifest.Entries[i] = files[i].entry
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return BackupManifest{}, err
	}
	if err := writeBackupArchive(options.Destination, manifestData, files); err != nil {
		return BackupManifest{}, err
	}
	return manifest, nil
}

func collectBackupPath(rootHandle *os.Root, root, target string, seen map[string]struct{}, files *[]capturedFile) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	info, err := rootHandle.Lstat(filepath.ToSlash(relative))
	if err != nil {
		return fmt.Errorf("inspect backup source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup source contains a symbolic link")
	}
	if !info.IsDir() {
		return captureBackupFile(rootHandle, root, target, seen, files)
	}
	return filepath.WalkDir(target, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("backup source contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		return captureBackupFile(rootHandle, root, current, seen, files)
	})
}

func captureBackupFile(rootHandle *os.Root, root, filename string, seen map[string]struct{}, files *[]capturedFile) error {
	relative, err := filepath.Rel(root, filename)
	if err != nil || !validArchivePath(filepath.ToSlash(relative)) {
		return errors.New("backup source path is invalid")
	}
	name := filepath.ToSlash(relative)
	if _, duplicate := seen[name]; duplicate {
		return nil
	}
	before, err := rootHandle.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > MaxBackupFileBytes {
		return fmt.Errorf("backup source %q is not a supported regular file", name)
	}
	file, err := rootHandle.Open(name)
	if err != nil {
		return err
	}
	opened, statErr := file.Stat()
	data, readErr := io.ReadAll(io.LimitReader(file, MaxBackupFileBytes+1))
	closeErr := file.Close()
	after, afterErr := rootHandle.Lstat(name)
	if statErr != nil || readErr != nil || closeErr != nil || afterErr != nil {
		return errors.Join(statErr, readErr, closeErr, afterErr)
	}
	if len(data) > MaxBackupFileBytes || !os.SameFile(before, opened) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("backup source %q changed during capture", name)
	}
	digest := sha256.Sum256(data)
	seen[name] = struct{}{}
	*files = append(*files, capturedFile{entry: BackupEntry{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), Mode: 0o600}, data: data})
	return nil
}

func writeBackupArchive(destination string, manifest []byte, files []capturedFile) (err error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".discord-control-backup-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	writer := zip.NewWriter(temporary)
	if err := writeZipEntry(writer, "manifest.json", manifest); err == nil {
		for _, file := range files {
			if err = writeZipEntry(writer, file.entry.Path, file.data); err != nil {
				break
			}
		}
	}
	err = errors.Join(err, writer.Close(), temporary.Sync(), temporary.Close())
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil || !os.IsNotExist(err) {
		return errors.New("backup destination already exists")
	}
	return os.Rename(temporaryName, destination)
}

func writeZipEntry(writer *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o600)
	header.Modified = archiveTime
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func RestoreBackup(filename, destination string) (BackupManifest, error) {
	if destination == "" {
		return BackupManifest{}, errors.New("restore destination is required")
	}
	if _, err := os.Lstat(destination); err == nil || !os.IsNotExist(err) {
		return BackupManifest{}, errors.New("restore destination must not exist")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return BackupManifest{}, err
	}
	snapshot, err := snapshotBackup(filename, parent)
	if err != nil {
		return BackupManifest{}, err
	}
	defer os.Remove(snapshot)
	manifest, err := VerifyBackup(snapshot)
	if err != nil {
		return BackupManifest{}, err
	}
	reader, err := zip.OpenReader(snapshot)
	if err != nil {
		return BackupManifest{}, err
	}
	defer reader.Close()
	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		entries[file.Name] = file
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return BackupManifest{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(destination)
		}
	}()
	for _, expected := range manifest.Entries {
		data, readErr := readZipFile(entries[expected.Path])
		if readErr != nil {
			return BackupManifest{}, readErr
		}
		target := filepath.Join(destination, filepath.FromSlash(expected.Path))
		relative, relErr := filepath.Rel(destination, target)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return BackupManifest{}, errors.New("restore entry escaped staging directory")
		}
		if err := atomicRestoreFile(target, data); err != nil {
			return BackupManifest{}, err
		}
	}
	complete = true
	return manifest, nil
}

func snapshotBackup(filename, directory string) (string, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	archiveRoot, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	defer archiveRoot.Close()
	base := filepath.Base(absolute)
	before, err := archiveRoot.Lstat(base)
	if err != nil || !before.Mode().IsRegular() || before.Size() > MaxBackupBytes {
		return "", errors.New("backup archive is unavailable or oversized")
	}
	source, err := archiveRoot.Open(base)
	if err != nil {
		return "", err
	}
	opened, statErr := source.Stat()
	temporary, createErr := os.CreateTemp(directory, ".restore-source-*")
	if statErr != nil || createErr != nil {
		_ = source.Close()
		if temporary != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
		return "", errors.Join(statErr, createErr)
	}
	name := temporary.Name()
	copyErr := temporary.Chmod(0o600)
	if copyErr == nil {
		_, copyErr = io.Copy(temporary, io.LimitReader(source, MaxBackupBytes+1))
	}
	closeErr := source.Close()
	syncErr := temporary.Sync()
	temporaryCloseErr := temporary.Close()
	after, afterErr := archiveRoot.Lstat(base)
	if copyErr != nil || closeErr != nil || syncErr != nil || temporaryCloseErr != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		_ = os.Remove(name)
		return "", errors.New("backup archive changed while being staged")
	}
	copied, err := os.Stat(name)
	if err != nil || copied.Size() != before.Size() {
		_ = os.Remove(name)
		return "", errors.New("backup archive staging was incomplete")
	}
	return name, nil
}

func atomicRestoreFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".restore-entry-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	writeErr := file.Chmod(0o600)
	if writeErr == nil {
		_, writeErr = file.Write(data)
	}
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, target)
}

func VerifyBackup(filename string) (BackupManifest, error) {
	info, err := os.Stat(filename)
	if err != nil || info.Size() > MaxBackupBytes {
		return BackupManifest{}, errors.New("backup archive is unavailable or oversized")
	}
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("open backup archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) < 2 || len(reader.File) > MaxBackupEntries+1 {
		return BackupManifest{}, errors.New("backup archive entry count is outside supported bounds")
	}
	archiveEntries := map[string]*zip.File{}
	var totalUncompressed uint64
	for _, file := range reader.File {
		if !validArchivePath(file.Name) || file.FileInfo().Mode()&os.ModeType != 0 || file.UncompressedSize64 > MaxBackupFileBytes {
			return BackupManifest{}, errors.New("backup archive contains an unsafe entry")
		}
		totalUncompressed += file.UncompressedSize64
		if totalUncompressed > MaxBackupBytes {
			return BackupManifest{}, errors.New("backup archive contains an unsafe entry")
		}
		if _, duplicate := archiveEntries[file.Name]; duplicate {
			return BackupManifest{}, errors.New("backup archive contains duplicate entries")
		}
		archiveEntries[file.Name] = file
	}
	manifestFile, ok := archiveEntries["manifest.json"]
	if !ok {
		return BackupManifest{}, errors.New("backup manifest is missing")
	}
	manifestData, err := readZipFile(manifestFile)
	if err != nil {
		return BackupManifest{}, err
	}
	var manifest BackupManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != BackupSchemaVersion || len(manifest.Entries) != len(reader.File)-1 {
		return BackupManifest{}, errors.New("backup manifest is malformed or unsupported")
	}
	seen := map[string]struct{}{}
	for _, expected := range manifest.Entries {
		if !validArchivePath(expected.Path) || expected.Path == "manifest.json" || expected.Size < 0 || expected.Size > MaxBackupFileBytes || expected.Mode != 0o600 {
			return BackupManifest{}, errors.New("backup manifest contains an unsafe entry")
		}
		if _, duplicate := seen[expected.Path]; duplicate {
			return BackupManifest{}, errors.New("backup manifest contains duplicate entries")
		}
		seen[expected.Path] = struct{}{}
		file, ok := archiveEntries[expected.Path]
		if !ok {
			return BackupManifest{}, errors.New("backup manifest references a missing entry")
		}
		data, err := readZipFile(file)
		if err != nil || int64(len(data)) != expected.Size {
			return BackupManifest{}, errors.New("backup entry size mismatch")
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expected.SHA256 {
			return BackupManifest{}, errors.New("backup entry digest mismatch")
		}
	}
	return manifest, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, MaxBackupFileBytes+1))
	return data, errors.Join(readErr, reader.Close())
}

func validArchivePath(name string) bool {
	return name != "" && name == path.Clean(name) && !path.IsAbs(name) && name != "." && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\:\x00")
}
