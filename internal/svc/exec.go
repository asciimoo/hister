// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner runs an external command and returns stdout, stderr, and the run error.
type CommandRunner func(name string, args ...string) (stdout, stderr string, err error)

var commandTimeout = 30 * time.Second

func defaultRunner(name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("%s timed out after %s: %w", name, commandTimeout, ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func run(runner CommandRunner, name string, args ...string) (string, string, error) {
	if runner == nil {
		runner = defaultRunner
	}
	stdout, stderr, err := runner(name, args...)
	if err != nil {
		detail := stderr
		if detail == "" {
			detail = stdout
		}
		if detail != "" {
			return stdout, stderr, fmt.Errorf("%s: %w: %s", name, err, trimOneLine(detail))
		}
		return stdout, stderr, fmt.Errorf("%s: %w", name, err)
	}
	return stdout, stderr, nil
}

func trimOneLine(s string) string {
	s = strings.TrimRight(s, "\r\n")
	runes := []rune(s)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return s
}

func isMissingExecutable(err error) bool {
	return err != nil && errors.Is(err, exec.ErrNotFound)
}

// managerReportsAbsent reports whether launchctl/systemctl output says the
// job or unit is absent. It inspects command output only, never the Go error
// string, so "executable file not found in $PATH" is not treated as "not installed".
func managerReportsAbsent(stderr, stdout string) bool {
	msg := strings.ToLower(stderr + "\n" + stdout)
	for _, p := range []string{
		"could not find service",
		"could not find domain",
		"could not be found",
		"no such process",
		"service not found",
		"not loaded",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
