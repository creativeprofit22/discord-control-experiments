package state

import (
	"errors"
	"os"
	"path/filepath"
)

func openRootFile(rootPath, target string, flag int, permission os.FileMode) (*os.File, error) {
	if !contained(rootPath, target) {
		return nil, errors.New("state file escaped configured root")
	}
	relative, err := filepath.Rel(rootPath, target)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(relative, flag, permission)
}

func readRootFile(rootPath, target string) ([]byte, error) {
	if !contained(rootPath, target) {
		return nil, errors.New("state file escaped configured root")
	}
	relative, err := filepath.Rel(rootPath, target)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(relative)
}
