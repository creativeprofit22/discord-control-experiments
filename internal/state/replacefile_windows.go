//go:build windows

package state

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func replaceAutomationFile(source, destination string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encode automation temporary path: %w", err)
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode automation projection path: %w", err)
	}
	flags := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
	if err := windows.MoveFileEx(sourcePath, destinationPath, flags); err != nil {
		return fmt.Errorf("replace automation projection: %w", err)
	}
	return nil
}
