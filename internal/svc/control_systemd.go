// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package svc

import (
	"fmt"
	"os"
)

func newPlatformManager() (Manager, error) {
	if !hasSystemd() {
		return nil, fmt.Errorf("%w: systemd is not available", ErrUnsupported)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	return newSystemdUserManager(defaultRunner, home, runtimeDir, systemdUserBusUp(runtimeDir))
}

func hasSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	return systemdUserBusUp("")
}
