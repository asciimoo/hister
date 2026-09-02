// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"fmt"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

// Definition is the Hister process a user-level service should run.
type Definition struct {
	Binary     string
	ConfigPath string
	DataDir    string
	StdoutLog  string
	StderrLog  string
}

func (d Definition) Args() []string {
	args := []string{d.Binary, "listen"}
	if d.ConfigPath != "" {
		args = append(args, "--config", d.ConfigPath)
	}
	return args
}

func (d Definition) Validate() error {
	if d.Binary == "" {
		return errors.New("binary path is empty")
	}
	if d.DataDir == "" {
		return errors.New("data directory is empty")
	}
	if !filepath.IsAbs(d.Binary) {
		return fmt.Errorf("binary path %q is not absolute", d.Binary)
	}
	if !filepath.IsAbs(d.DataDir) {
		return fmt.Errorf("data directory %q is not absolute", d.DataDir)
	}
	if d.ConfigPath != "" && !filepath.IsAbs(d.ConfigPath) {
		return fmt.Errorf("config path %q is not absolute", d.ConfigPath)
	}
	for _, p := range []string{d.Binary, d.ConfigPath, d.DataDir, d.StdoutLog, d.StderrLog} {
		if p == "" {
			continue
		}
		if err := rejectPersistedControlChars(p); err != nil {
			return err
		}
	}
	return nil
}

func rejectPersistedControlChars(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("service path is not valid UTF-8")
	}
	for _, r := range s {
		switch {
		case r == 0:
			return errors.New("service path contains a NUL byte")
		case r == '\n' || r == '\r' || (unicode.IsControl(r) && r != '\t'):
			return errors.New("service path contains a control character")
		}
	}
	return nil
}
