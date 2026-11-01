//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import "os"

func replaceManagedFile(source, destination string) error {
	return os.Rename(source, destination)
}

func syncManagedDirectory(directory *os.File) error {
	return directory.Sync()
}
