// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func replaceManagedFile(source, destination string) error {
	from, err := managedWindowsPath(source)
	if err != nil {
		return err
	}
	to, err := managedWindowsPath(destination)
	if err != nil {
		return err
	}
	// Both files are in the same directory. Do not permit a copy fallback;
	// request the durability barrier as part of the replacement itself.
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}

func syncManagedDirectory(_ *os.File) error {
	// MoveFileEx already requested write through. FlushFileBuffers cannot sync
	// the read only directory handle, so it must not be used as another barrier.
	return nil
}

func managedWindowsPath(path string) (*uint16, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Preserve the long path support provided by os.Rename, including UNC paths.
	if !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
		} else {
			path = `\\?\` + path
		}
	}
	return windows.UTF16PtrFromString(path)
}
