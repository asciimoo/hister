// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type snapshot struct {
	path       string
	existed    bool
	isSymlink  bool
	data       []byte
	mode       os.FileMode
	linkTarget string
}

type extraArtifact struct {
	path    string
	symlink string // if set, write a symlink to this target
}

type replaceHooks struct {
	stopFirst   bool
	restoreRun  bool
	stop        func() error
	reload      func() error
	start       func() error
	startWanted bool
}

func snapshotPath(path string) (snapshot, error) {
	s := snapshot{path: path}
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	s.existed = true
	s.mode = st.Mode()
	if st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return s, err
		}
		s.isSymlink = true
		s.linkTarget = target
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	s.data = data
	return s, nil
}

func restoreSnapshot(s snapshot) error {
	if !s.existed {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if s.isSymlink {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return err
		}
		return os.Symlink(s.linkTarget, s.path)
	}
	return atomicWrite(s.path, s.data, s.mode.Perm())
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	removeTmp = false
	syncDir(dir)
	return nil
}

func writeSymlink(path, target string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, path)
}

func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

func replaceDefinition(path string, content []byte, extras []extraArtifact, hooks replaceHooks) error {
	defSnap, err := snapshotPath(path)
	if err != nil {
		return err
	}
	extraSnaps := make([]snapshot, 0, len(extras))
	for _, extra := range extras {
		s, err := snapshotPath(extra.path)
		if err != nil {
			return err
		}
		extraSnaps = append(extraSnaps, s)
	}

	if hooks.stopFirst {
		if hooks.stop == nil {
			return errors.New("stop of existing service is required but no stop function was provided")
		}
		if err := hooks.stop(); err != nil {
			return fmt.Errorf("stop existing service: %w", err)
		}
	}

	rollback := func(primary error) error {
		errs := []error{primary}
		restored := true
		if err := restoreSnapshot(defSnap); err != nil {
			restored = false
			errs = append(errs, fmt.Errorf("restore service definition: %w", err))
		}
		for _, s := range extraSnaps {
			if err := restoreSnapshot(s); err != nil {
				restored = false
				errs = append(errs, fmt.Errorf("restore %s: %w", s.path, err))
			}
		}
		if restored && hooks.reload != nil {
			if err := hooks.reload(); err != nil {
				restored = false
				errs = append(errs, fmt.Errorf("reload restored service definition: %w", err))
			}
		}
		if restored && hooks.restoreRun && hooks.start != nil {
			if err := hooks.start(); err != nil {
				errs = append(errs, fmt.Errorf("restore running state: %w", err))
			}
		}
		return errors.Join(errs...)
	}

	if err := atomicWrite(path, content, 0o644); err != nil {
		return rollback(err)
	}
	for _, extra := range extras {
		if extra.symlink != "" {
			if err := writeSymlink(extra.path, extra.symlink); err != nil {
				return rollback(err)
			}
		}
	}
	if hooks.reload != nil {
		if err := hooks.reload(); err != nil {
			return rollback(err)
		}
	}
	if hooks.startWanted {
		if hooks.start == nil {
			return fmt.Errorf("installed service definition at %s, but no start function was provided", path)
		}
		if err := hooks.start(); err != nil {
			return fmt.Errorf("installed service definition at %s, but failed to start: %w", path, err)
		}
	}
	return nil
}
