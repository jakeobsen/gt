package storage

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := Open(dir, "demo")
	if _, ok := f.Get("missing"); ok {
		t.Fatal("empty store returned a value")
	}
	if err := f.Set("b", "2"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("b"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("never-there"); err != nil {
		t.Fatal(err)
	}

	again := Open(dir, "demo") // a new process reading the same file
	if v, ok := again.Get("a"); !ok || v != "1" {
		t.Fatalf("Get(a) = %q, %v after reopening", v, ok)
	}
	if keys := again.Keys(); !slices.Equal(keys, []string{"a"}) {
		t.Fatalf("Keys() = %v", keys)
	}
	info, err := os.Stat(filepath.Join(dir, "plugins"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode = %v, %v; want private (0700)", info.Mode().Perm(), err)
	}
}

func TestCorruptFileIsAnEmptyStore(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o700)
	os.WriteFile(filepath.Join(dir, "plugins", "demo.json"), []byte("{not json"), 0o600)
	f := Open(dir, "demo")
	if keys := f.Keys(); len(keys) != 0 {
		t.Fatalf("Keys() = %v", keys)
	}
	if err := f.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
}

func TestDirPrecedence(t *testing.T) {
	t.Setenv(EnvDir, "/explicit")
	if d, _ := Dir(); d != "/explicit" {
		t.Errorf("Dir() = %s", d)
	}
	t.Setenv(EnvDir, "")
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if d, _ := Dir(); d != "/xdg/gt" {
		t.Errorf("Dir() = %s", d)
	}
}

func TestStoresShareOneFilePerPlugin(t *testing.T) {
	s := NewStores(t.TempDir())
	if s.For("a") != s.For("a") || s.For("a") == s.For("b") {
		t.Fatal("want one File per plugin")
	}
}
