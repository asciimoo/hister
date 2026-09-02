// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"fmt"
)

// Exit codes for `hister service status`. Ownership (ours vs foreign) does not change them.
const (
	ExitRunning      = 0
	ExitFailure      = 1
	ExitStopped      = 3
	ExitNotInstalled = 4
)

var (
	ErrUnsupported      = errors.New("background service install is not available on this platform")
	ErrNotInstalled     = errors.New("hister service is not installed")
	ErrAlreadyInstalled = errors.New("hister service is already installed; use --force to replace it")
	ErrForeign          = errors.New("service definition is managed externally")
	ErrUnstableBinary   = errors.New("refusing to install a binary path that will change on the next upgrade")
)

// Manager is the native user-level service controller.
type Manager interface {
	Platform() string
	DefinitionPath() string
	Logs() []string
	LoginNote() string
	Install(def Definition, opts InstallOptions) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (Status, error)
}

// InstallOptions controls whether an existing owned definition may be replaced
// and whether the process should be started after writing.
type InstallOptions struct {
	Force   bool
	NoStart bool
}

// State is the process state reported by Status.
type State int

const (
	StateNotInstalled State = iota
	StateStopped
	StateRunning
)

// Status is the result of querying the native service manager.
type Status struct {
	State             State
	PID               int
	DefinitionPath    string
	Platform          string
	ExternallyManaged bool
	Failed            bool
}

func (s Status) ExitCode() int {
	switch s.State {
	case StateRunning:
		return ExitRunning
	case StateStopped:
		return ExitStopped
	case StateNotInstalled:
		return ExitNotInstalled
	default:
		return ExitFailure
	}
}

func (s Status) String() string {
	switch s.State {
	case StateRunning:
		if s.PID > 0 {
			return fmt.Sprintf("running (pid %d)", s.PID)
		}
		return "running"
	case StateStopped:
		if s.Failed {
			return "installed, failed"
		}
		return "installed, not running"
	case StateNotInstalled:
		return "not installed"
	default:
		return "unknown"
	}
}

// New returns the platform service manager.
func New() (Manager, error) {
	return newPlatformManager()
}
