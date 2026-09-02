// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"testing"
)

func TestProcessExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: 0},
		{name: "plain", err: errors.New("boom"), want: 1},
		{name: "stopped", err: exitError(3, "stopped"), want: 3},
		{name: "not installed", err: exitError(4, "missing"), want: 4},
		{name: "wrapped", err: errors.Join(exitError(3, "stopped"), errors.New("extra")), want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProcessExitCode(tt.err); got != tt.want {
				t.Fatalf("ProcessExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
