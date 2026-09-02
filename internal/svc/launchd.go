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

func newLaunchdManager(runner CommandRunner, home string) (*userManager, error) {
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
	plist := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	stdoutLog := filepath.Join(home, "Library", "Logs", "hister.log")
	stderrLog := filepath.Join(home, "Library", "Logs", "hister-error.log")
	uid := os.Getuid()
	domain := "gui/" + strconv.Itoa(uid)
	target := domain + "/" + launchdLabel

	m := &userManager{
		platform:       "launchd",
		definitionPath: plist,
		logs:           []string{stdoutLog, stderrLog},
		loginNote:      "Hister will start at login.",
		runner:         runner,
	}
	m.render = RenderLaunchd
	m.withPlatformPaths = func(def Definition) Definition {
		def.StdoutLog = stdoutLog
		def.StderrLog = stderrLog
		return def
	}
	m.prepare = func(def Definition) error {
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return err
		}
		if def.StdoutLog != "" {
			if err := os.MkdirAll(filepath.Dir(def.StdoutLog), 0o755); err != nil {
				return err
			}
		}
		return nil
	}
	m.canStartNow = func() bool { return true }
	m.startUnavailableError = func() error {
		return errors.New("launchd is unavailable")
	}
	m.start = func() error {
		q, err := m.query()
		if err != nil {
			return err
		}
		if q.loaded {
			_, _, err := run(m.runner, "launchctl", "kickstart", target)
			return err
		}
		_, _, err = run(m.runner, "launchctl", "bootstrap", domain, plist)
		return err
	}
	m.stop = func() error {
		stdout, stderr, err := run(m.runner, "launchctl", "bootout", target)
		if err != nil && !isMissingExecutable(err) && managerReportsAbsent(stderr, stdout) {
			return nil
		}
		return err
	}
	m.reload = func() error { return nil }
	m.query = func() (jobQuery, error) {
		stdout, stderr, err := run(m.runner, "launchctl", "print", target)
		if err != nil {
			if isMissingExecutable(err) {
				return jobQuery{}, err
			}
			if managerReportsAbsent(stderr, stdout) {
				return jobQuery{}, nil
			}
			return jobQuery{}, err
		}
		running, pid := parseLaunchdPrint(stdout)
		return jobQuery{loaded: true, running: running, pid: pid}, nil
	}
	return m, nil
}

func parseLaunchdPrint(out string) (bool, int) {
	running := false
	pid := 0
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "state = "):
			state := strings.TrimPrefix(line, "state = ")
			if state == "running" {
				running = true
			}
		case strings.HasPrefix(line, "pid = "):
			n, convErr := strconv.Atoi(strings.TrimPrefix(line, "pid = "))
			if convErr == nil {
				pid = n
			}
		}
	}
	if pid > 0 {
		running = true
	}
	return running, pid
}
