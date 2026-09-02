// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"fmt"
	"os"
	"path/filepath"
)

type userManager struct {
	platform              string
	definitionPath        string
	wantsPath             string
	wantsTarget           string
	logs                  []string
	loginNote             string
	runner                CommandRunner
	render                func(Definition) (string, error)
	prepare               func(Definition) error
	withPlatformPaths     func(Definition) Definition
	start                 func() error
	stop                  func() error
	query                 func() (jobQuery, error)
	reload                func() error
	canStartNow           func() bool
	startUnavailableError func() error
}

func (m *userManager) Platform() string { return m.platform }
func (m *userManager) DefinitionPath() string {
	return m.definitionPath
}
func (m *userManager) Logs() []string    { return m.logs }
func (m *userManager) LoginNote() string { return m.loginNote }

type jobQuery struct {
	loaded  bool
	running bool
	failed  bool
	pid     int
}

func (m *userManager) Install(def Definition, opts InstallOptions) error {
	def = m.withPlatformPaths(def)
	if err := def.Validate(); err != nil {
		return err
	}

	own, err := Classify(m.definitionPath)
	if err != nil {
		return err
	}
	switch own {
	case OwnershipForeign:
		return ErrForeign
	case OwnershipOurs:
		if !opts.Force {
			return ErrAlreadyInstalled
		}
	}
	if err := m.requireEnableLinkWritable(); err != nil {
		return err
	}

	content, err := m.render(def)
	if err != nil {
		return err
	}

	q, qerr := m.query()
	if qerr != nil && own != OwnershipMissing {
		return fmt.Errorf("query service manager: %w", qerr)
	}
	if isOrphanJob(own, q, qerr) {
		return ErrForeign
	}
	if err := m.prepare(def); err != nil {
		return err
	}

	hooks := replaceHooks{
		stopFirst:   own == OwnershipOurs && (q.loaded || q.running || opts.Force),
		restoreRun:  own == OwnershipOurs && q.running,
		stop:        m.stop,
		reload:      m.reload,
		start:       m.start,
		startWanted: !opts.NoStart,
	}
	if !m.canStartNow() {
		hooks.start = func() error {
			return m.startUnavailableError()
		}
		if opts.NoStart {
			hooks.startWanted = false
		}
	}

	return replaceDefinition(m.definitionPath, []byte(content), m.extras(), hooks)
}

func (m *userManager) Uninstall() error {
	own, err := Classify(m.definitionPath)
	if err != nil {
		return err
	}
	if own == OwnershipForeign {
		return ErrForeign
	}
	if err := m.requireEnableLinkWritable(); err != nil {
		return err
	}

	q, qerr := m.query()
	if qerr != nil {
		return fmt.Errorf("query service manager: %w", qerr)
	}
	if own == OwnershipMissing {
		if q.loaded || q.running {
			return ErrForeign
		}
		removed := false
		for _, extra := range m.extras() {
			if err := os.Remove(extra.path); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			removed = true
		}
		if removed && m.reload != nil {
			if err := m.reload(); err != nil {
				return fmt.Errorf("removed service definition, but reload failed: %w", err)
			}
		}
		return nil
	}

	if err := m.stop(); err != nil {
		return err
	}
	for _, extra := range m.extras() {
		if err := os.Remove(extra.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(m.definitionPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if m.reload != nil {
		if err := m.reload(); err != nil {
			return fmt.Errorf("removed service definition, but reload failed: %w", err)
		}
	}
	return nil
}

func (m *userManager) Start() error {
	if err := m.requireOurs(); err != nil {
		return err
	}
	if !m.canStartNow() {
		return m.startUnavailableError()
	}
	return m.start()
}

func (m *userManager) Stop() error {
	if err := m.requireOurs(); err != nil {
		return err
	}
	if !m.canStartNow() {
		return m.startUnavailableError()
	}
	return m.stop()
}

func (m *userManager) Restart() error {
	if err := m.requireOurs(); err != nil {
		return err
	}
	if !m.canStartNow() {
		return m.startUnavailableError()
	}
	if err := m.stop(); err != nil {
		return err
	}
	return m.start()
}

func (m *userManager) Status() (Status, error) {
	st := Status{
		DefinitionPath: m.definitionPath,
		Platform:       m.platform,
	}
	own, err := Classify(m.definitionPath)
	if err != nil {
		return st, err
	}

	q, qerr := m.query()
	if qerr != nil {
		return st, fmt.Errorf("query service manager: %w", qerr)
	}

	st.ExternallyManaged = own == OwnershipForeign || isOrphanJob(own, q, qerr)
	if own == OwnershipMissing && !q.loaded && !q.running {
		st.State = StateNotInstalled
		return st, nil
	}
	if q.running {
		st.State = StateRunning
		st.PID = q.pid
		return st, nil
	}
	st.State = StateStopped
	st.Failed = q.failed
	return st, nil
}

func (m *userManager) requireOurs() error {
	own, err := Classify(m.definitionPath)
	if err != nil {
		return err
	}
	switch own {
	case OwnershipForeign:
		return ErrForeign
	case OwnershipMissing:
		q, qerr := m.query()
		if qerr != nil {
			return fmt.Errorf("query service manager: %w", qerr)
		}
		if q.loaded || q.running {
			return ErrForeign
		}
		return ErrNotInstalled
	}
	return nil
}

func isOrphanJob(own Ownership, q jobQuery, qerr error) bool {
	return own == OwnershipMissing && qerr == nil && (q.loaded || q.running)
}

func (m *userManager) requireEnableLinkWritable() error {
	own, err := classifyEnableLink(m.wantsPath, m.wantsTarget)
	if err != nil {
		return err
	}
	if own == OwnershipForeign {
		return ErrForeign
	}
	return nil
}

func (m *userManager) extras() []extraArtifact {
	if m.wantsPath == "" {
		return nil
	}
	return []extraArtifact{{path: m.wantsPath, symlink: m.wantsTarget}}
}

func userConfigDir(home string) (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", fmt.Errorf("XDG_CONFIG_HOME %q is not an absolute path", xdg)
		}
		return xdg, nil
	}
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, ".config"), nil
}
