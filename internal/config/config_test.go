package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jakeobsen/gt/kit"
)

func testPlugins() []kit.Plugin {
	return []kit.Plugin{
		{
			Name:        "scratch",
			Title:       "Scratch",
			Description: "Disposable folders.",
			Commands:    []kit.Command{{Name: "create", Title: "Create", Run: func(kit.Context) error { return nil }}},
			Settings: []kit.Setting{
				{Key: "root", Kind: kit.Path, Default: "~/scratch", Description: "Where scratch folders live."},
				{Key: "stale-days", Kind: kit.Int, Default: 7, Description: "Days until a folder counts as stale."},
				{Key: "open-in-editor", Kind: kit.Bool, Default: true, Description: "Open new folders in the editor."},
				{Key: "editor", Kind: kit.String, Default: "code", Description: "Editor command."},
				{Key: "delete-mode", Kind: kit.Choice, Default: "trash", Description: "How to remove folders.", Options: []kit.Option{
					{Value: "trash", Description: "move to the Trash"},
					{Value: "delete", Description: "delete permanently"},
				}},
			},
		},
		{
			Name:     "hello-world",
			Title:    "Hello World",
			Commands: []kit.Command{{Name: "say", Title: "Say", Run: func(kit.Context) error { return nil }}},
		},
	}
}

// load writes content to a temp config file and loads it.
func load(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path, testPlugins())
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var fe *FileError
	if !errors.As(err, &fe) {
		t.Fatalf("error = %v, want a *FileError", err)
	}
	return fe.Problems
}

func TestPathPrecedence(t *testing.T) {
	t.Setenv(EnvPath, "/explicit/gt.toml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p, _ := Path(); p != "/explicit/gt.toml" {
		t.Errorf("with %s set: Path() = %q", EnvPath, p)
	}
	t.Setenv(EnvPath, "")
	if p, _ := Path(); p != "/xdg/gt/config.toml" {
		t.Errorf("with XDG_CONFIG_HOME set: Path() = %q", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if p, _ := Path(); p != filepath.Join(home, ".config", "gt", "config.toml") {
		t.Errorf("default: Path() = %q", p)
	}
}

func TestMissingFileMeansDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), testPlugins())
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Plugin("scratch")
	home, _ := os.UserHomeDir()
	if got := s.String("root"); got != filepath.Join(home, "scratch") {
		t.Errorf("root = %q, want ~ expanded", got)
	}
	if s.Int("stale-days") != 7 || !s.Bool("open-in-editor") || s.String("delete-mode") != "trash" {
		t.Error("defaults not applied")
	}
	if cfg.Theme() != "auto" || !cfg.Enabled("scratch") || !cfg.Enabled("hello-world") {
		t.Error("core defaults not applied")
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	cfg, err := load(t, `
theme = "light"

[plugins.scratch]
root = "/tmp/s"
stale-days = 30
open-in-editor = false
delete-mode = "delete"

[plugins.hello-world]
enabled = false
`)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Plugin("scratch")
	if s.String("root") != "/tmp/s" || s.Int("stale-days") != 30 || s.Bool("open-in-editor") || s.String("delete-mode") != "delete" {
		t.Error("file values not applied")
	}
	if cfg.Theme() != "light" || cfg.Enabled("hello-world") || !cfg.Enabled("scratch") {
		t.Error("core/enabled values not applied")
	}
}

func TestEveryProblemIsReported(t *testing.T) {
	_, err := load(t, `
theme = "blue"
colour = "red"

[plugins.scratch]
stale-dayz = 3
stale-days = "7"
open-in-editor = "yes"
delete-mode = 1

[plugins.scratchy]
enabled = false
`)
	got := strings.Join(problems(t, err), "\n")
	for _, want := range []string{
		`theme: must be one of "auto", "dark", "light" (got "blue")`,
		`colour: unknown setting`,
		`plugins.scratch.stale-dayz: unknown setting (did you mean "stale-days"?)`,
		`plugins.scratch.stale-days: must be a whole number, without quotes (got "7")`,
		`plugins.scratch.open-in-editor: must be true or false (got "yes")`,
		`plugins.scratch.delete-mode: must be one of "trash", "delete" (got 1)`,
		`plugins.scratchy: unknown plugin (did you mean "scratch"?)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, `colour: unknown setting (did you mean`) {
		t.Errorf("suggested a match for a key nothing like any setting:\n%s", got)
	}
}

func TestSyntaxErrorShowsPosition(t *testing.T) {
	_, err := load(t, "theme = \"dark\"\n[plugins.scratch\n")
	got := strings.Join(problems(t, err), "\n")
	if !strings.Contains(got, "At line") || !strings.Contains(got, "[plugins.scratch") {
		t.Fatalf("want the line number in the message; got:\n%s", got)
	}
}

func TestWrongShapeForTables(t *testing.T) {
	_, err := load(t, "plugins = 3\n")
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "plugins: must be a table") {
		t.Fatalf("got:\n%s", got)
	}
	_, err = load(t, "[plugins]\nscratch = true\n")
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "plugins.scratch: must be a table") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestMisusedSettingPanicsWithClearMessage(t *testing.T) {
	s := Defaults(testPlugins()).Plugin("scratch")
	for name, f := range map[string]func(){
		"undeclared": func() { s.Bool("nope") },
		"wrong type": func() { s.Int("root") },
	} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), `plugin "scratch" reads setting`) {
					t.Errorf("%s: recover() = %v", name, r)
				}
			}()
			f()
		}()
	}
}

func TestDefaultFileLoadsAsDefaults(t *testing.T) {
	text := DefaultFile(testPlugins())
	cfg, err := load(t, text)
	if err != nil {
		t.Fatalf("the default file doesn't load: %v\n%s", err, text)
	}
	if cfg.Plugin("scratch").Int("stale-days") != 7 {
		t.Error("default file changed a value")
	}
	// Uncommenting every setting must also load and keep the defaults.
	uncommented := strings.NewReplacer("# theme", "theme", "# enabled", "enabled", "# root", "root",
		"# stale-days", "stale-days", "# open-in-editor", "open-in-editor", "# editor", "editor",
		"# delete-mode", "delete-mode").Replace(text)
	cfg, err = load(t, uncommented)
	if err != nil {
		t.Fatalf("the default file with every setting uncommented doesn't load: %v\n%s", err, uncommented)
	}
	if cfg.Plugin("scratch").String("delete-mode") != "trash" || cfg.Theme() != "auto" {
		t.Error("uncommented default file changed a value")
	}
}

func TestReferenceDocumentsEverySetting(t *testing.T) {
	doc := Reference(testPlugins())
	for _, want := range []string{
		"| `theme` | choice | `\"auto\"` |",
		"`\"dark\"`: colours for a dark background",
		"[plugins.scratch]",
		"| `stale-days` | whole number | `7` | Days until a folder counts as stale. |",
		"| `root` | path | `\"~/scratch\"` |",
		"| `enabled` | true/false | `true` |",
		"### Hello World",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("reference is missing %q", want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"", "", 0}, {"abc", "abc", 0}, {"stale-dayz", "stale-days", 1}, {"kitten", "sitting", 3}} {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
