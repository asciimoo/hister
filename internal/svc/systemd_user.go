// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdUnitName = "hister.service"

func newSystemdUserManager(runner CommandRunner, home, runtimeDir string, busUp bool) (*userManager, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if runner == nil {
		runner = defaultRunner
	}
	unitDirParent, err := userConfigDir(home)
	if err != nil {
		return nil, err
	}
	unitDir := filepath.Join(unitDirParent, "systemd", "user")
	unitPath := filepath.Join(unitDir, systemdUnitName)
	wantsPath := filepath.Join(unitDir, "default.target.wants", systemdUnitName)

	m := &userManager{
		platform:       "systemd --user",
		definitionPath: unitPath,
		wantsPath:      wantsPath,
		wantsTarget:    unitPath,
		loginNote:      "This systemd user service runs while you are logged in. To keep it running after logout and at boot:\n  loginctl enable-linger",
		runner:         runner,
	}
	m.render = RenderSystemdUser
	m.withPlatformPaths = func(def Definition) Definition { return def }
	m.prepare = func(Definition) error {
		return os.MkdirAll(filepath.Join(unitDir, "default.target.wants"), 0o755)
	}
	m.canStartNow = func() bool { return busUp }
	m.startUnavailableError = func() error {
		return errors.New("the systemd user session is not running; log in or run `loginctl enable-linger`, then: hister service start")
	}
	m.start = func() error {
		_, _, err := run(m.runner, "systemctl", "--user", "start", systemdUnitName)
		return err
	}
	m.stop = func() error {
		stdout, stderr, err := run(m.runner, "systemctl", "--user", "stop", systemdUnitName)
		if err != nil && !isMissingExecutable(err) && managerReportsAbsent(stderr, stdout) {
			return nil
		}
		return err
	}
	m.reload = func() error {
		if !busUp {
			return nil
		}
		_, _, err := run(m.runner, "systemctl", "--user", "daemon-reload")
		return err
	}
	m.query = func() (jobQuery, error) {
		if !busUp {
			if runtimeDir == "" {
				runtimeDir = os.Getenv("XDG_RUNTIME_DIR")
			}
			if !systemdUserBusUp(runtimeDir) {
				return jobQuery{}, errors.New("systemd user bus is not available")
			}
		}
		stdout, stderr, err := run(m.runner, "systemctl", "--user", "show", systemdUnitName,
			"--property=LoadState", "--property=ActiveState", "--property=MainPID")
		if err != nil {
			if isMissingExecutable(err) {
				return jobQuery{}, err
			}
			if managerReportsAbsent(stderr, stdout) {
				return jobQuery{}, nil
			}
			return jobQuery{}, err
		}
		return parseSystemdShow(stdout)
	}
	return m, nil
}

func systemdUserBusUp(runtimeDir string) bool {
	if runtimeDir == "" {
		runtimeDir = os.Getenv("XDG_RUNTIME_DIR")
	}
	if runtimeDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(runtimeDir, "systemd", "private"))
	return err == nil
}

func parseSystemdShow(out string) (jobQuery, error) {
	loadState := ""
	activeState := ""
	pid := 0
	for line := range strings.SplitSeq(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			loadState = val
		case "ActiveState":
			activeState = val
		case "MainPID":
			n, convErr := strconv.Atoi(val)
			if convErr == nil {
				pid = n
			}
		}
	}
	if loadState == "" || loadState == "not-found" {
		return jobQuery{}, nil
	}
	failed := activeState == "failed"
	running := !failed && (activeState == "active" || activeState == "activating" || activeState == "reloading")
	if pid > 0 && !failed {
		running = true
	}
	return jobQuery{loaded: true, running: running, failed: failed, pid: pid}, nil
}
