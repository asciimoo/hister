// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build darwin

package svc

func newPlatformManager() (Manager, error) {
	return newLaunchdManager(defaultRunner, "")
}
