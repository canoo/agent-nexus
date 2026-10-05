package main

import (
	"os"
	"path/filepath"
)

// atomicWriteFile replaces a regular file only after its complete replacement
// is written. Existing modes and symlink targets are preserved. It does not
// serialize read-modify-write operations with other processes.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			path = resolved
		}
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return &os.PathError{Op: "replace", Path: path, Err: os.ErrInvalid}
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".nexus-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
