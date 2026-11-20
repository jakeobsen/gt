package vscode

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

type memStore map[string]string

func (m memStore) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memStore) Set(k, v string) error       { m[k] = v; return nil }
func (m memStore) Delete(k string) error       { delete(m, k); return nil }
func (m memStore) Keys() []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

type fakeCtx struct {
	settings kit.Settings
	store    memStore
	filter   string
	answer   bool
	toasts   []string
	closed   bool
}

func (c *fakeCtx) HUD(string)                              {}
func (c *fakeCtx) Close()                                  { c.closed = true }
func (c *fakeCtx) Settings() kit.Settings                  { return c.settings }
func (c *fakeCtx) Argument(string) string                  { return "" }
func (c *fakeCtx) Push(kit.View)                           {}
func (c *fakeCtx) Refresh()                                {}
func (c *fakeCtx) Storage() kit.Storage                    { return c.store }
func (c *fakeCtx) Filter() string                          { return c.filter }
func (c *fakeCtx) DataDir() (string, error)                { return "", nil }
func (c *fakeCtx) EditText(_, text string) (string, error) { return text, nil }
func (c *fakeCtx) OpenTerminal(string, string) error       { return nil }
func (c *fakeCtx) Confirm(kit.Confirmation) bool           { return c.answer }
func (c *fakeCtx) Toast(_ kit.ToastStyle, title, _ string) kit.Toast {
	c.toasts = append(c.toasts, title)
	return nopToast{}
}

type nopToast struct{}

func (nopToast) Update(kit.ToastStyle, string, string) {}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup makes a fake VS Code app and a projects folder, and returns a context
// whose settings point at them.
func setup(t *testing.T, extra string) (*fakeCtx, string) {
	t.Helper()
	base := t.TempDir()
	app := filepath.Join(base, "Visual Studio Code.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "pm")
	write(t, filepath.Join(dir, "projects.json"), `[
		{"name": "gt", "rootPath": "/src/gt", "tags": ["go", "work"], "enabled": true},
		{"name": "blog", "rootPath": "/src/blog", "tags": [], "enabled": false},
		{"name": "notes", "rootPath": "/src/notes", "tags": ["personal"]},
		{"name": 42, "rootPath": "/broken"}
	]`)
	write(t, filepath.Join(dir, "projects_cache_git.json"), `[
		{"name": "gt-dupe", "fullPath": "/src/gt"},
		{"name": "cached", "fullPath": "/src/cached"}
	]`)
	cfgPath := filepath.Join(base, "config.toml")
	write(t, cfgPath, "[plugins.vscode-projects]\nvscode-app = \""+app+"\"\nprojects-location = \""+dir+"\"\n"+extra)
	cfg, err := config.Load(cfgPath, []kit.Plugin{Plugin()})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeCtx{settings: cfg.Plugin("vscode-projects"), store: memStore{}}, dir
}

func titles(s kit.Section) []string {
	var out []string
	for _, it := range s.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestPluginIsValid(t *testing.T) {
	if err := kit.Validate([]kit.Plugin{Plugin()}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectsMergesCachesAndSkipsBadEntries(t *testing.T) {
	_, dir := setup(t, "")
	projects, err := loadProjects(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range projects {
		names = append(names, p.name)
	}
	if !slices.Equal(names, []string{"gt", "blog", "notes", "cached"}) {
		t.Fatalf("names = %v (bad entry and duplicate cache entry should be skipped)", names)
	}
	if projects[1].enabled || !projects[2].enabled {
		t.Fatal("enabled: false must be kept; a missing enabled means true")
	}

	projects, _ = loadProjects(dir, true, true)
	if len(projects) != 2 || projects[0].name != "gt" || projects[1].name != "notes" {
		t.Fatalf("with both hide settings: %+v", projects)
	}
}

func TestProjectsLocation(t *testing.T) {
	home, _ := os.UserHomeDir()
	got, err := projectsLocation("", "/Applications/Visual Studio Code - Insiders.app")
	want := filepath.Join(home, "Library", "Application Support", "Code - Insiders", "User", "globalStorage", "alefragnani.project-manager")
	if err != nil || got != want {
		t.Fatalf("default: %s, %v", got, err)
	}
	_, dir := setup(t, "")
	if got, _ := projectsLocation(filepath.Join(dir, "projects.json"), ""); got != dir {
		t.Fatalf("a file should resolve to its folder; got %s", got)
	}
	if _, err := projectsLocation(filepath.Join(dir, "nope"), ""); err == nil {
		t.Fatal("a missing location should be an error")
	}
}

func TestContentGroupsByTagAndRanksByFrecency(t *testing.T) {
	ctx, _ := setup(t, "")
	c, err := content(ctx, prefsFrom(ctx.settings))
	if err != nil {
		t.Fatal(err)
	}
	var sections []string
	for _, s := range c.Sections {
		sections = append(sections, s.Title+"="+strings.Join(titles(s), ","))
	}
	if want := "[no tags]=blog,cached go=gt personal=notes work=gt"; strings.Join(sections, " ") != want {
		t.Fatalf("sections = %v, want %s", sections, want)
	}
	gt := c.Sections[1].Items[0]
	if gt.Subtitle != "/src" || gt.Accessories[0].Text != "go, work" || gt.Actions[0].Title != "Open in Code" {
		t.Fatalf("gt item: %+v", gt)
	}

	// Opening "cached" ranks it above "blog" in the untagged section.
	opened := stubOpen(t)
	for _, a := range c.Sections[0].Items[1].Actions {
		if a.Title == "Open in Code" {
			if err := a.Run(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(*opened) != 1 || !ctx.closed {
		t.Fatalf("opened=%v closed=%v", *opened, ctx.closed)
	}
	c, _ = content(ctx, prefsFrom(ctx.settings))
	if got := titles(c.Sections[0]); !slices.Equal(got, []string{"cached", "blog"}) {
		t.Fatalf("after a visit: %v", got)
	}
}

func TestTagFilterShowsAFlatList(t *testing.T) {
	ctx, _ := setup(t, "")
	ctx.filter = "work"
	c, _ := content(ctx, prefsFrom(ctx.settings))
	if len(c.Sections) != 1 || !slices.Equal(titles(c.Sections[0]), []string{"gt"}) {
		t.Fatalf("filtered: %+v", c.Sections)
	}

	v, _ := projectsList(ctx)
	f := v.(*kit.List).Filter
	if f == nil || f.Options[0].Title != "All Tags" || len(f.Options) != 4 {
		t.Fatalf("filter = %+v", f)
	}
}

func TestEmptyStates(t *testing.T) {
	ctx, dir := setup(t, "")
	os.Remove(filepath.Join(dir, "projects.json"))
	os.Remove(filepath.Join(dir, "projects_cache_git.json"))
	c, err := content(ctx, prefsFrom(ctx.settings))
	if err != nil || c.Empty.Title != "No projects found" {
		t.Fatalf("no projects: %+v, %v", c.Empty, err)
	}

	p := prefsFrom(ctx.settings)
	p.app = "/nowhere/Visual Studio Code.app"
	if c, _ := content(ctx, p); c.Empty.Title != "Visual Studio Code not found" {
		t.Fatalf("missing app: %+v", c.Empty)
	}
}

func TestRemoteTarget(t *testing.T) {
	a, p := remoteTarget("vscode-remote://ssh-remote+devbox/home/me/src")
	if a != "ssh-remote+devbox" || p != "/home/me/src/" {
		t.Fatalf("got %q %q", a, p)
	}
}

func TestTrashAsksFirst(t *testing.T) {
	ctx, _ := setup(t, "")
	var trashed []string
	old := trashPath
	trashPath = func(p string) error { trashed = append(trashed, p); return nil }
	t.Cleanup(func() { trashPath = old })

	c, _ := content(ctx, prefsFrom(ctx.settings))
	run := func() {
		for _, a := range c.Sections[1].Items[0].Actions {
			if a.Title == "Move to Trash" {
				_ = a.Run(ctx)
			}
		}
	}
	run()
	if len(trashed) != 0 {
		t.Fatal("trashed without confirmation")
	}
	ctx.answer = true
	run()
	if !slices.Equal(trashed, []string{"/src/gt"}) {
		t.Fatalf("trashed = %v", trashed)
	}
}

func stubOpen(t *testing.T) *[]string {
	t.Helper()
	opened := &[]string{}
	old := openPath
	openPath = func(target, app string) error { *opened = append(*opened, target); return nil }
	t.Cleanup(func() { openPath = old })
	return opened
}
