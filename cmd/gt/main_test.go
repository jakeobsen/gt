package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

var update = flag.Bool("update", false, "rewrite docs/configuration.md from the setting declarations")

const docsPath = "../../docs/configuration.md"

// TestConfigDocsUpToDate keeps docs/configuration.md in step with the setting
// declarations. Run `make docs` to regenerate it.
func TestConfigDocsUpToDate(t *testing.T) {
	want := config.Reference(plugins())
	if *update {
		if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(docsPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(docsPath)
	if err != nil || string(got) != want {
		t.Fatal("docs/configuration.md doesn't match the setting declarations; run `make docs`")
	}
}

func TestRegisteredPluginsAreValid(t *testing.T) {
	if err := kit.Validate(plugins()); err != nil {
		t.Fatal(err)
	}
}

func TestFlags(t *testing.T) {
	t.Setenv(config.EnvPath, "/somewhere/gt.toml")
	for _, c := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"--version"}, 0, "gt dev"},
		{[]string{"--help"}, 0, "gt --default-config"},
		{[]string{"--config-path"}, 0, "/somewhere/gt.toml"},
		{[]string{"--default-config"}, 0, "# theme = \"auto\""},
		{[]string{"--nope"}, 2, `unknown argument "--nope"`},
	} {
		var out, errOut bytes.Buffer
		code := run(c.args, &out, &errOut)
		if code != c.code || !strings.Contains(out.String()+errOut.String(), c.contains) {
			t.Errorf("gt %v: exit %d, output:\n%s%s\nwant exit %d containing %q", c.args, code, out.String(), errOut.String(), c.code, c.contains)
		}
	}
}

func TestBadConfigStopsBeforeTheUI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`theme = "blue"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvPath, path)
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), `theme: must be one of`) {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut.String())
	}
}
