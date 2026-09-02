// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !darwin && !linux

package svc

import (
	"fmt"
	"runtime"
)

func newPlatformManager() (Manager, error) {
	return nil, fmt.Errorf("%w on %s", ErrUnsupported, runtime.GOOS)
}
