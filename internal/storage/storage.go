// Package storage keeps each plugin's kit.Storage in a JSON file under gt's
// state directory.
package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// EnvDir overrides where state is kept.
const EnvDir = "GT_STATE_DIR"

// Dir returns gt's state directory: $GT_STATE_DIR if set, otherwise
// $XDG_STATE_HOME/gt, otherwise ~/.local/state/gt.
func Dir() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return d, nil
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "gt"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "gt"), nil
}

// File is one plugin's store: a JSON object of strings in
// <dir>/plugins/<plugin>.json, read on first use and rewritten whole (via a
// temporary file and rename) on every change.
type File struct {
	mu     sync.Mutex
	path   string
	data   map[string]string
	loaded bool
}

// Open returns the store for plugin under dir. Nothing is read until used.
func Open(dir, plugin string) *File {
	return &File{path: filepath.Join(dir, "plugins", plugin+".json")}
}

// load reads the file once. A missing or unreadable file is an empty store,
// so a corrupt file can't stop a plugin from working.
func (f *File) load() {
	if f.loaded {
		return
	}
	f.loaded = true
	f.data = map[string]string{}
	if data, err := os.ReadFile(f.path); err == nil {
		_ = json.Unmarshal(data, &f.data)
	}
}

func (f *File) save() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f.data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

func (f *File) Get(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	v, ok := f.data[key]
	return v, ok
}

func (f *File) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	f.data[key] = value
	return f.save()
}

func (f *File) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	if _, ok := f.data[key]; !ok {
		return nil
	}
	delete(f.data, key)
	return f.save()
}

func (f *File) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Stores hands out one File per plugin, so every command of a plugin shares
// the same in-memory copy.
type Stores struct {
	mu    sync.Mutex
	dir   string
	files map[string]*File
}

func NewStores(dir string) *Stores { return &Stores{dir: dir, files: map[string]*File{}} }

func (s *Stores) For(plugin string) *File {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.files[plugin]; ok {
		return f
	}
	f := Open(s.dir, plugin)
	s.files[plugin] = f
	return f
}
