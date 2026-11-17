// Package logging owns the process log output independently of server generations.
package logging

import (
	"errors"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog"

	"github.com/asciimoo/nogon/config"
)

var Default = New(os.Stderr)

// Manager is a stable writer shared by the global logger and its derived
// loggers. Switching destinations never replaces those logger values.
type Manager struct {
	mu     sync.Mutex
	stderr io.Writer
	out    io.Writer
	file   *os.File
}

func New(stderr io.Writer) *Manager {
	return &Manager{stderr: stderr, out: newConsoleWriter(stderr, false)}
}

func (m *Manager) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.out.Write(p)
}

// Change owns a prepared destination until Apply transfers ownership to its
// manager. Its caller must Close it, including when a reload is abandoned.
// Apply and Close must be called sequentially by that caller.
type Change struct {
	manager *Manager
	out     io.Writer
	file    *os.File
	level   zerolog.Level
}

// Prepare opens even an unchanged file path so reload supports log rotation.
// It leaves the active output and log level untouched.
func (m *Manager) Prepare(c config.App) (*Change, error) {
	levelName := c.LogLevel
	switch levelName {
	case "":
		levelName = "debug"
	case "warning":
		levelName = "warn"
	case "err":
		levelName = "error"
	}
	level, err := zerolog.ParseLevel(levelName)
	if err != nil {
		return nil, errors.New("invalid app.log_level")
	}
	if c.LogFormat != "" && c.LogFormat != "text" && c.LogFormat != "json" {
		return nil, errors.New("invalid app.log_format")
	}
	change := &Change{manager: m, out: m.stderr, level: level}
	if c.LogFile != "" {
		f, err := os.OpenFile(c.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			return nil, err
		}
		change.out, change.file = f, f
	}
	if c.LogFormat != "json" {
		change.out = newConsoleWriter(change.out, change.file != nil)
	}
	return change, nil
}

// Apply waits for active writes, publishes the prepared output and level, and
// closes the previous file. A returned close error does not undo the switch.
func (c *Change) Apply() error {
	if c.out == nil {
		return errors.New("logging change already applied or closed")
	}
	m := c.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.file
	m.out, m.file = c.out, c.file
	zerolog.SetGlobalLevel(c.level)
	c.out, c.file = nil, nil
	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (c *Change) Close() error {
	file := c.file
	c.out, c.file = nil, nil
	if file != nil {
		return file.Close()
	}
	return nil
}

// Close releases the owned log file and returns output to stderr. The process
// stderr is borrowed and is never closed.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	file := m.file
	m.out, m.file = newConsoleWriter(m.stderr, false), nil
	if file != nil {
		return file.Close()
	}
	return nil
}
