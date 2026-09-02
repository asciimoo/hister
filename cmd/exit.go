// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import "errors"

// ExitCoder is an error that carries a process exit code. Cobra RunE
// functions return these instead of calling os.Exit.
type ExitCoder interface {
	error
	ExitCode() int
}

type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	return e.msg
}

func (e *exitCodeError) ExitCode() int {
	return e.code
}

func exitError(code int, msg string) error {
	return &exitCodeError{code: code, msg: msg}
}

// ProcessExitCode maps an Execute error to a process status.
// 0 is success; errors that do not implement ExitCoder are 1.
func ProcessExitCode(err error) int {
	if err == nil {
		return 0
	}
	if ec, ok := errors.AsType[ExitCoder](err); ok {
		return ec.ExitCode()
	}
	return 1
}
