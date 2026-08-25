//go:build !windows

package state

import "os"

func replaceAutomationFile(source, destination string) error {
	return os.Rename(source, destination)
}
