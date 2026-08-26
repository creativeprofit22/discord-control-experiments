package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseArchivesAreDeterministicAndMetadataIsValidated(t *testing.T) {
	payload := []byte("release-binary")
	for _, extension := range []string{"zip", "tar.gz"} {
		first := filepath.Join(t.TempDir(), "first."+extension)
		second := filepath.Join(t.TempDir(), "second."+extension)
		var err error
		if extension == "zip" {
			err = writeZip(first, "discord-control.exe", payload)
			if err == nil {
				err = writeZip(second, "discord-control.exe", payload)
			}
		} else {
			err = writeTarGzip(first, "discord-control", payload)
			if err == nil {
				err = writeTarGzip(second, "discord-control", payload)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		firstData, _ := os.ReadFile(first)
		secondData, _ := os.ReadFile(second)
		if !bytes.Equal(firstData, secondData) {
			t.Fatalf("%s archives differ", extension)
		}
	}
	for _, value := range []string{"", "v1.0.0;rm", "value with spaces"} {
		if safeMetadata(value) {
			t.Fatalf("unsafe metadata %q accepted", value)
		}
	}
	if !safeMetadata("v1.0.0-rc.1") {
		t.Fatal("valid release version rejected")
	}
}
