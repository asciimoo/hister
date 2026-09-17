package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
)

func startTestWatcher(t *testing.T, dirs []*config.Directory, paths []string) (<-chan string, <-chan string, context.CancelFunc) {
	t.Helper()
	w, err := NewWatcher(dirs, paths)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	changed, removed := make(chan string, 32), make(chan string, 32)
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx, func(path string) { changed <- path }, func(path string) { removed <- path })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("watcher stopped: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("watcher did not stop")
		}
		_ = w.Close()
	})
	return changed, removed, cancel
}

func waitForFileEvent(t *testing.T, events <-chan string, path string) {
	t.Helper()
	select {
	case got := <-events:
		if got != path {
			t.Fatalf("event = %s, want %s", got, path)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no event for %s", path)
	}
}

func TestWatcherExplicitFileSurvivesReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, removed, cancel := startTestWatcher(t, nil, []string{path})
	for _, contents := range []string{"first save", "second save"} {
		temporary := filepath.Join(root, "save.tmp")
		if err := os.WriteFile(temporary, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary, path); err != nil {
			t.Fatal(err)
		}
		waitForFileEvent(t, changed, path)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case path := <-removed:
		t.Fatalf("explicit file unexpectedly deleted: %s", path)
	default:
	}
}

func TestWatcherDiscoversMovedTreeAndAppliesFilters(t *testing.T) {
	root := t.TempDir()
	changed, _, _ := startTestWatcher(t, []*config.Directory{{Path: root, Filetypes: []string{"txt"}, Excludes: []string{"excluded*"}}}, nil)
	staging := t.TempDir()
	for _, dir := range []string{"tree/deep", "tree/.hidden", "tree/excluded"} {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tree/deep/note.txt", "tree/deep/code.go", "tree/.hidden/note.txt", "tree/excluded/note.txt"} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte("initial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(filepath.Join(staging, "tree"), filepath.Join(root, "tree")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tree/deep/note.txt")
	waitForFileEvent(t, changed, path)
	if err := os.WriteFile(path, []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, path)
	select {
	case path := <-changed:
		t.Fatalf("unexpected event for excluded file: %s", path)
	case <-time.After(2 * debounceTime):
	}
}

func TestWatcherRemovalRequiresDirectoryOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain", true: "delete"}[enabled], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "note.txt")
			if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, removed, _ := startTestWatcher(t, []*config.Directory{{Path: root, DeleteOnRemove: enabled}}, nil)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if enabled {
				waitForFileEvent(t, removed, path)
			} else {
				select {
				case path := <-removed:
					t.Fatalf("unexpected removal: %s", path)
				case <-time.After(2 * debounceTime):
				}
			}
		})
	}
}

func TestWatcherRemovalUsesFirstDirectory(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		firstDeletes, secondDeletes bool
		firstExcludes, parentFirst  bool
		wantRemoval                 bool
	}{
		{name: "retain", secondDeletes: true},
		{name: "delete", firstDeletes: true, wantRemoval: true},
		{name: "excluded", firstDeletes: true, secondDeletes: true, firstExcludes: true},
		{name: "parent first", secondDeletes: true, parentFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			nested := filepath.Join(root, "private")
			if err := os.Mkdir(nested, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(nested, "note.txt")
			if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
				t.Fatal(err)
			}
			first, second := nested, root
			if tc.parentFirst {
				first, second = root, nested
			}
			dirs := []*config.Directory{
				{Path: first, DeleteOnRemove: tc.firstDeletes},
				{Path: second, DeleteOnRemove: tc.secondDeletes},
			}
			if tc.firstExcludes {
				dirs[0].Excludes = []string{"note.txt"}
			}
			_, removed, _ := startTestWatcher(t, dirs, nil)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if tc.wantRemoval {
				waitForFileEvent(t, removed, path)
			} else {
				select {
				case path := <-removed:
					t.Fatalf("later directory overrode removal policy: %s", path)
				case <-time.After(2 * debounceTime):
				}
			}
		})
	}
}

func TestWatcherSymlinkDirectoryRoot(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("cannot create symbolic link: %v", err)
	}
	path := filepath.Join(target, "note.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _, _ := startTestWatcher(t, []*config.Directory{{Path: root}}, nil)
	if err := os.WriteFile(path, []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, filepath.Join(root, "note.txt"))
	if err := os.WriteFile(filepath.Join(target, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, filepath.Join(root, "new.txt"))
}

func TestWatcherReattachesReplacedDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "notes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	changed, _, _ := startTestWatcher(t, []*config.Directory{{Path: root, Filetypes: []string{"txt"}}}, nil)
	if err := os.Rename(root, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, path)
	if err := os.WriteFile(path, []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, path)
}

func TestWatcherSetupFailurePolicy(t *testing.T) {
	root := t.TempDir()
	dirs := []*config.Directory{nil, {Path: filepath.Join(root, "missing")}, {Path: root}}
	if w, err := NewWatcher(dirs, nil); err == nil {
		_ = w.Close()
		t.Fatal("CLI watcher accepted a missing directory")
	}
	w, err := newWatcher(dirs, nil, false)
	if err != nil {
		t.Fatalf("server watcher rejected an accessible directory: %v", err)
	}
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx, func(path string) { cancel() }, nil)
	}()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server watcher did not observe its accessible directory")
	}
}

// TestWatcherSymlinkDirectoryRootSubdirectory covers a nested directory under a
// symbolically linked root. WalkDir does not descend a symbolic link, so the
// scan that registers watches stopped at the link and nothing below the root
// was ever observed.
func TestWatcherSymlinkDirectoryRootSubdirectory(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("cannot create symbolic link: %v", err)
	}
	nested := filepath.Join(target, "chapters")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "note.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, _, _ := startTestWatcher(t, []*config.Directory{{Path: root}}, nil)
	if err := os.WriteFile(path, []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileEvent(t, changed, filepath.Join(root, "chapters", "note.txt"))
}
