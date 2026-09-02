// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MarkerSystemd = "# Managed by hister service"
	MarkerPlist   = "<!-- Managed by hister service -->"
)

// Ownership of a service definition file on disk.
type Ownership int

const (
	OwnershipMissing Ownership = iota
	OwnershipOurs
	OwnershipForeign
)

func (o Ownership) String() string {
	switch o {
	case OwnershipMissing:
		return "missing"
	case OwnershipOurs:
		return "ours"
	case OwnershipForeign:
		return "foreign"
	default:
		return "unknown"
	}
}

// Classify reports whether path is missing, owned by `hister service`, or
// managed elsewhere. Symlinks are always foreign (Home Manager / Nix).
func Classify(path string) (Ownership, error) {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OwnershipMissing, nil
		}
		return 0, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return OwnershipForeign, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	if ownedByHister(data) {
		return OwnershipOurs, nil
	}
	return OwnershipForeign, nil
}

func ownedByHister(data []byte) bool {
	s := strings.TrimPrefix(string(data), "\ufeff")
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return false
	}
	if lines[0] == MarkerSystemd {
		return true
	}
	return strings.HasPrefix(lines[0], "<?xml") && len(lines) > 1 && lines[1] == MarkerPlist
}

func classifyEnableLink(path, wantTarget string) (Ownership, error) {
	if path == "" {
		return OwnershipMissing, nil
	}
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OwnershipMissing, nil
		}
		return 0, err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return OwnershipForeign, nil
	}
	got, err := os.Readlink(path)
	if err != nil {
		return 0, err
	}
	if enableLinkPointsAt(path, got, wantTarget) {
		return OwnershipOurs, nil
	}
	return OwnershipForeign, nil
}

func enableLinkPointsAt(linkPath, got, want string) bool {
	if got == want {
		return true
	}
	if !filepath.IsAbs(got) {
		got = filepath.Join(filepath.Dir(linkPath), got)
	}
	gotAbs, err1 := filepath.Abs(got)
	wantAbs, err2 := filepath.Abs(want)
	return err1 == nil && err2 == nil && gotAbs == wantAbs
}
