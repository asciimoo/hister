// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestManagerReportsAbsentDoesNotMatchPATHMiss(t *testing.T) {
	if managerReportsAbsent("executable file not found in $PATH", "") {
		t.Fatal("PATH miss must not be treated as a missing job")
	}
	if !managerReportsAbsent("Could not find service", "") {
		t.Fatal("launchd missing job should be absent")
	}
	if !managerReportsAbsent("Unit hister.service could not be found.", "") {
		t.Fatal("systemd missing unit should be absent")
	}
}

func TestDefaultRunnerTimeout(t *testing.T) {
	if os.Getenv("HISTER_TEST_HELPER_SLEEP") == "1" {
		time.Sleep(time.Hour)
		return
	}
	orig := commandTimeout
	commandTimeout = 80 * time.Millisecond
	t.Cleanup(func() { commandTimeout = orig })
	t.Setenv("HISTER_TEST_HELPER_SLEEP", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = defaultRunner(exe, "-test.run=^TestDefaultRunnerTimeout$")
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestIsMissingExecutableUnwraps(t *testing.T) {
	if isMissingExecutable(errors.New("not found")) {
		t.Fatal("generic not found is not ErrNotFound")
	}
	wrapped := errors.Join(exec.ErrNotFound)
	if !isMissingExecutable(wrapped) {
		t.Fatal("wrapped ErrNotFound should match")
	}
}
