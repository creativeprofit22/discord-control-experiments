package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var releaseTime = time.Unix(0, 0).UTC()

type target struct{ OS, Arch string }

type manifest struct {
	SchemaVersion    string   `json:"schemaVersion"`
	Version          string   `json:"version"`
	Commit           string   `json:"commit"`
	ConfigVersions   []int    `json:"configVersions"`
	StateVersions    []int    `json:"stateVersions"`
	DesiredVersions  []int    `json:"desiredVersions"`
	BackupVersions   []int    `json:"backupVersions"`
	SupportedTargets []string `json:"supportedTargets"`
	Unsupported      []string `json:"unsupported"`
}

var targets = []target{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

func main() {
	version := flag.String("version", "", "release version")
	commit := flag.String("commit", "", "full source commit")
	output := flag.String("output", "dist", "new empty output directory")
	flag.Parse()
	if !safeMetadata(*version) || !safeMetadata(*commit) {
		fatal(errors.New("version and commit must use 1-128 ASCII letters, numbers, dots, underscores, or hyphens"))
	}
	verify := exec.Command("bash", "scripts/verify.sh")
	verify.Stdout, verify.Stderr = os.Stdout, os.Stderr
	if err := verify.Run(); err != nil {
		fatal(fmt.Errorf("CI-parity verification failed: %w", err))
	}
	if err := packageRelease(*version, *commit, *output); err != nil {
		fatal(err)
	}
}

func packageRelease(version, commit, output string) error {
	if info, err := os.Stat(output); err == nil && (!info.IsDir() || directoryNotEmpty(output)) {
		return errors.New("release output must be a new or empty directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "discord-control-release-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	checksums := map[string]string{}
	for _, item := range targets {
		name := "discord-control_" + version + "_" + item.OS + "_" + item.Arch
		extension := "tar.gz"
		binaryName := "discord-control"
		if item.OS == "windows" {
			extension, binaryName = "zip", "discord-control.exe"
		}
		first := filepath.Join(work, name+"-a", binaryName)
		second := filepath.Join(work, name+"-b", binaryName)
		if err := buildTarget(first, version, commit, item); err != nil {
			return err
		}
		if err := buildTarget(second, version, commit, item); err != nil {
			return err
		}
		firstData, err := os.ReadFile(first)
		if err != nil {
			return err
		}
		secondData, err := os.ReadFile(second)
		if err != nil || !bytes.Equal(firstData, secondData) {
			return errors.New("target rebuild was not byte-identical: " + item.OS + "/" + item.Arch)
		}
		archiveName := name + "." + extension
		archivePath := filepath.Join(output, archiveName)
		if item.OS == "windows" {
			err = writeZip(archivePath, binaryName, firstData)
		} else {
			err = writeTarGzip(archivePath, binaryName, firstData)
		}
		if err != nil {
			return err
		}
		checksums[archiveName], err = fileDigest(archivePath)
		if err != nil {
			return err
		}
	}
	releaseManifest := manifest{SchemaVersion: "discord-control.release/v1", Version: version, Commit: commit, ConfigVersions: []int{1}, StateVersions: []int{1}, DesiredVersions: []int{1, 2}, BackupVersions: []int{1}, Unsupported: []string{"discord-gateway", "remote-mcp"}}
	for _, item := range targets {
		releaseManifest.SupportedTargets = append(releaseManifest.SupportedTargets, item.OS+"/"+item.Arch)
	}
	manifestData, _ := json.MarshalIndent(releaseManifest, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "release-manifest.json"), append(manifestData, '\n'), 0o600); err != nil {
		return err
	}
	for source, destination := range map[string]string{"reports/licenses.csv": "licenses.csv", "reports/sbom.cdx.json": "sbom.cdx.json"} {
		if err := copyFile(source, filepath.Join(output, destination)); err != nil {
			return fmt.Errorf("required release report %s: %w", source, err)
		}
	}
	smoke := []byte("{\n  \"schemaVersion\": \"discord-control.smoke/v1\",\n  \"nativeEvidenceRequired\": true\n}\n")
	if err := os.WriteFile(filepath.Join(output, "smoke-requirements.json"), smoke, 0o600); err != nil {
		return err
	}
	for _, name := range []string{"release-manifest.json", "licenses.csv", "sbom.cdx.json", "smoke-requirements.json"} {
		digest, err := fileDigest(filepath.Join(output, name))
		if err != nil {
			return err
		}
		checksums[name] = digest
	}
	names := make([]string, 0, len(checksums))
	for name := range checksums {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines strings.Builder
	for _, name := range names {
		fmt.Fprintf(&lines, "%s  %s\n", checksums[name], name)
	}
	return os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(lines.String()), 0o600)
}

func buildTarget(destination, version, commit string, item target) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	command := exec.Command(goBinary, "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid= -X github.com/ken-kai/discord-control-experiments/internal/buildinfo.Version="+version+" -X github.com/ken-kai/discord-control-experiments/internal/buildinfo.Commit="+commit, "-o", destination, "./cmd/discord-control")
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+item.OS, "GOARCH="+item.Arch)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s/%s: %w: %s", item.OS, item.Arch, err, output)
	}
	return nil
}

func writeZip(filename, name string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o755)
	header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	entry, err := writer.CreateHeader(header)
	if err == nil {
		_, err = entry.Write(data)
	}
	return errors.Join(err, writer.Close(), file.Sync(), file.Close())
}

func writeTarGzip(filename, name string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(file)
	gzipWriter.Header.ModTime, gzipWriter.Header.OS = releaseTime, 255
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), ModTime: releaseTime, Uid: 0, Gid: 0, Format: tar.FormatUSTAR}
	writeErr := tarWriter.WriteHeader(header)
	if writeErr == nil {
		_, writeErr = tarWriter.Write(data)
	}
	return errors.Join(writeErr, tarWriter.Close(), gzipWriter.Close(), file.Sync(), file.Close())
}

func fileDigest(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), errors.Join(copyErr, file.Close())
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}

func directoryNotEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err != nil || len(entries) != 0
}
func safeMetadata(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-", character) {
			return false
		}
	}
	return true
}
func fatal(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
