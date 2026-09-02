// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	arg0       = func() string { return os.Args[0] }
	lookPath   = exec.LookPath
	executable = os.Executable
)

// ResolveBinary returns a stable, unresolved path to this hister executable
// for persistence in a service definition. It never EvalSymlinks the result.
func ResolveBinary() (string, error) {
	path, err := resolveBinaryPath()
	if err != nil {
		return "", err
	}
	if reason := unstableBinaryReason(path); reason != "" {
		if alias := pathAliasFor(path); alias != "" && unstableBinaryReason(alias) == "" {
			path = alias
		} else {
			return "", fmt.Errorf("%w: %s", ErrUnstableBinary, reason)
		}
	}
	if err := validateResolvedBinary(path); err != nil {
		return "", err
	}
	return path, nil
}

func resolveBinaryPath() (string, error) {
	invoked := arg0()
	if invoked == "" {
		return absExecutable()
	}
	if filepath.IsAbs(invoked) {
		return invoked, nil
	}
	if strings.ContainsRune(invoked, os.PathSeparator) {
		return filepath.Abs(invoked)
	}

	if found, err := lookPath(invoked); err == nil && found != "" {
		if filepath.IsAbs(found) {
			return found, nil
		}
		return filepath.Abs(found)
	}

	exe, err := absExecutable()
	if err != nil {
		return "", err
	}
	if alias := pathAliasFor(exe); alias != "" {
		return alias, nil
	}
	return exe, nil
}

func absExecutable() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("locate hister binary: %w", err)
	}
	if filepath.IsAbs(exe) {
		return exe, nil
	}
	return filepath.Abs(exe)
}

func pathAliasFor(exe string) string {
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	for dir := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filepath.Base(exe))
		st, err := os.Lstat(candidate)
		if err != nil {
			continue
		}
		cmp := st
		if st.Mode()&os.ModeSymlink != 0 {
			resolved, err := os.Stat(candidate)
			if err != nil {
				continue
			}
			cmp = resolved
		}
		if os.SameFile(exeInfo, cmp) {
			if filepath.IsAbs(candidate) {
				return candidate
			}
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
			return candidate
		}
	}
	return ""
}

func validateResolvedBinary(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("binary %q: %w", path, err)
	}
	if st.IsDir() {
		return fmt.Errorf("binary %q is a directory", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("binary %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("binary %q is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("binary %q is not executable", path)
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("locate running hister executable: %w", err)
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("inspect running hister executable %q: %w", exe, err)
	}
	if !os.SameFile(info, exeInfo) {
		return fmt.Errorf("binary %q is not this hister executable", path)
	}
	return nil
}

func unstableBinaryReason(path string) string {
	cleaned := filepath.ToSlash(path)
	switch {
	case strings.Contains(cleaned, "/Cellar/hister/") || strings.Contains(cleaned, "/Cellar/hister@"):
		return "path is inside the Homebrew Cellar (" + path + ")"
	case strings.HasPrefix(cleaned, "/nix/store/") || strings.Contains(cleaned, "/nix/store/"):
		return "path is inside /nix/store (" + path + ")"
	default:
		return ""
	}
}
