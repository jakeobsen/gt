package scratch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

// --- fakes ------------------------------------------------------------------

type fakeCtx struct {
	settings  kit.Settings
	args      map[string]string
	answer    bool
	confirms  []kit.Confirmation
	toasts    []string
	hud       string
	closed    bool
	refreshed int
}

func (c *fakeCtx) HUD(text string)                         { c.hud = text }
func (c *fakeCtx) Close()                                  { c.closed = true }
func (c *fakeCtx) Settings() kit.Settings                  { return c.settings }
func (c *fakeCtx) Argument(name string) string             { return c.args[name] }
func (c *fakeCtx) Push(kit.View)                           {}
func (c *fakeCtx) Refresh()                                { c.refreshed++ }
func (c *fakeCtx) Storage() kit.Storage                    { return nil }
func (c *fakeCtx) Filter() string                          { return "" }
func (c *fakeCtx) DataDir() (string, error)                { return "", nil }
func (c *fakeCtx) EditText(_, text string) (string, error) { return text, nil }
func (c *fakeCtx) OpenTerminal(string, string) error       { return nil }
func (c *fakeCtx) Confirm(cf kit.Confirmation) bool {
	c.confirms = append(c.confirms, cf)
	return c.answer
}
func (c *fakeCtx) Toast(style kit.ToastStyle, title, message string) kit.Toast {
	t := &fakeToast{c: c}
	t.Update(style, title, message)
	return t
}

type fakeToast struct{ c *fakeCtx }

func (t *fakeToast) Update(_ kit.ToastStyle, title, message string) {
	t.c.toasts = append(t.c.toasts, strings.TrimSpace(title+" "+message))
}

// settingsFor loads the scratch plugin's settings from a config snippet.
func settingsFor(t *testing.T, toml string) kit.Settings {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[plugins.scratch]\n"+toml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, []kit.Plugin{Plugin()})
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Plugin("scratch")
}

// stubSystem replaces clipboard, Trash and open for one test.
func stubSystem(t *testing.T) (copied, opened, trashed *[]string) {
	t.Helper()
	copied, opened, trashed = &[]string{}, &[]string{}, &[]string{}
	oldCopy, oldOpen, oldTrash := copyText, openPath, trash
	copyText = func(s string) error { *copied = append(*copied, s); return nil }
	openPath = func(target, app string) error { *opened = append(*opened, target+" with "+app); return nil }
	trash = func(path string) error { *trashed = append(*trashed, path); return os.RemoveAll(path) }
	t.Cleanup(func() { copyText, openPath, trash = oldCopy, oldOpen, oldTrash })
	return
}

// mkdirAged makes dir (and parents) and sets its times to age ago.
func mkdirAged(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-age)
	if err := os.Chtimes(dir, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestPluginIsValid(t *testing.T) {
	if err := kit.Validate([]kit.Plugin{Plugin()}); err != nil {
		t.Fatal(err)
	}
}

// --- names ------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"redis repro":         "redis-repro",
		"  Ünïcode — Test!! ": "unicode-test",
		"CamelCase_and.dots":  "camelcase-and-dots",
		"!!!":                 "",
		"":                    "",
		// Cut at 40 characters, then trailing dashes trimmed, as in the Raycast version.
		strings.Repeat("ab ", 30): "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-a",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateNameStyles(t *testing.T) {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.Local)
	for style, pattern := range map[string]string{
		"words":        `^[a-z]+-[a-z]+$`,
		"words-suffix": `^[a-z]+-[a-z]+-[0-9a-f]{4}$`,
		"date-words":   `^2026-08-11-[a-z]+-[a-z]+$`,
		"hex":          `^scratch-[0-9a-f]{8}$`,
	} {
		if got := generateName(style, "", now); !regexp.MustCompile(pattern).MatchString(got) {
			t.Errorf("%s: %q doesn't match %s", style, got, pattern)
		}
	}
	if got := generateName("words", "Redis repro", now); !strings.HasPrefix(got, "redis-repro-") {
		t.Errorf("label prefix missing: %q", got)
	}
}

// --- scanning -----------------------------------------------------------------

func TestScanActivityLooksInsideTheTree(t *testing.T) {
	cutoff := time.Now().Add(-7 * day)
	root := t.TempDir()

	old := filepath.Join(root, "old")
	mkdirAged(t, filepath.Join(old, "a", "b"), 30*day)
	writeAged(t, filepath.Join(old, "a", "b", "f.txt"), 30*day)
	mkdirAged(t, filepath.Join(old, "a", "b"), 30*day) // writing the file touched it
	mkdirAged(t, filepath.Join(old, "a"), 30*day)
	mkdirAged(t, old, 30*day)
	if _, fresh, _, err := scanActivity(old, cutoff); err != nil || fresh {
		t.Fatalf("old tree: fresh=%v err=%v", fresh, err)
	}

	// A recent file three levels down keeps the whole folder alive, even
	// though the folder's own mtime is old.
	deep := filepath.Join(root, "deep")
	mkdirAged(t, filepath.Join(deep, "a", "b"), 30*day)
	writeAged(t, filepath.Join(deep, "a", "b", "new.txt"), time.Hour)
	mkdirAged(t, filepath.Join(deep, "a", "b"), 30*day)
	mkdirAged(t, filepath.Join(deep, "a"), 30*day)
	mkdirAged(t, deep, 30*day)
	if _, fresh, _, _ := scanActivity(deep, cutoff); !fresh {
		t.Fatal("a recent file deep inside should make the folder fresh")
	}
}

func TestScanActivityDoesNotFollowSymlinks(t *testing.T) {
	cutoff := time.Now().Add(-7 * day)
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	mkdirAged(t, outside, 0)
	writeAged(t, filepath.Join(outside, "fresh.txt"), 0)

	dir := filepath.Join(root, "dir")
	mkdirAged(t, dir, 30*day)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-30 * day)
	_ = os.Chtimes(dir, ts, ts)
	// Lchtimes isn't portable; the link's own mtime is "now", so date the
	// test by whether the scan entered the target, not by the link.
	newest, _, _, _ := scanActivity(dir, cutoff)
	info, _ := os.Lstat(link)
	if newest.After(info.ModTime().Add(time.Second)) {
		t.Fatalf("the scan followed the symlink (newest %v)", newest)
	}
}

func TestTruncatedScanIsNeverStale(t *testing.T) {
	old := scanEntryCap
	scanEntryCap = 5
	t.Cleanup(func() { scanEntryCap = old })

	root := t.TempDir()
	big := filepath.Join(root, "big")
	mkdirAged(t, big, 30*day)
	for i := range 10 {
		writeAged(t, filepath.Join(big, "f"+string(rune('a'+i))), 30*day)
	}
	mkdirAged(t, big, 30*day)

	folders, err := listFolders(root, 7, time.Now())
	if err != nil || len(folders) != 1 {
		t.Fatalf("folders=%v err=%v", folders, err)
	}
	if !folders[0].truncated || folders[0].stale {
		t.Fatalf("truncated=%v stale=%v; a cut-short scan must not count as stale", folders[0].truncated, folders[0].stale)
	}
}

func TestListFoldersOnlyTakesVisibleDirectories(t *testing.T) {
	root := t.TempDir()
	mkdirAged(t, filepath.Join(root, "active"), time.Hour)
	mkdirAged(t, filepath.Join(root, "stale"), 30*day)
	mkdirAged(t, filepath.Join(root, ".hidden"), 30*day)
	writeAged(t, filepath.Join(root, "loose.txt"), 30*day)

	folders, err := listFolders(root, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range folders {
		got[f.name] = f.stale
	}
	if len(got) != 2 || got["active"] || !got["stale"] {
		t.Fatalf("got %v, want active (fresh) and stale only", got)
	}
	if folders, err := listFolders(filepath.Join(root, "missing"), 7, time.Now()); err != nil || folders != nil {
		t.Fatalf("missing root: %v, %v", folders, err)
	}
}

// --- creating and removing ------------------------------------------------------

func TestCreateFolderWritesMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scratch") // created on first use
	path, err := createFolder(root, "words", "  redis repro ", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "redis-repro-") || filepath.Dir(path) != root {
		t.Fatalf("path = %s", path)
	}
	data, err := os.ReadFile(filepath.Join(path, markerFile))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil || m["label"] != "redis repro" || m["createdAt"] == "" {
		t.Fatalf("marker = %s (%v)", data, err)
	}
	if readMeta(path) == nil {
		t.Fatal("readMeta can't read the marker createFolder wrote")
	}
}

func TestCreateFolderNamesAreUnique(t *testing.T) {
	root := t.TempDir()
	seen := map[string]bool{}
	for range 40 {
		path, err := createFolder(root, "words", "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if seen[path] {
			t.Fatalf("created %s twice", path)
		}
		seen[path] = true
	}
}

func TestRemoveRefusesAnythingButADirectChild(t *testing.T) {
	_, _, trashed := stubSystem(t)
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	mkdirAged(t, nested, 0)
	for _, path := range []string{root, nested, filepath.Dir(root), filepath.Join(root, "..", "elsewhere")} {
		if err := removeFolder(root, path, "permanent"); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("removeFolder(%s) = %v, want a refusal", path, err)
		}
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatal("a refused delete removed something")
	}
	if err := removeFolder(root, filepath.Join(root, "a"), "trash"); err != nil || len(*trashed) != 1 {
		t.Fatalf("trash mode: err=%v trashed=%v", err, *trashed)
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for age, want := range map[time.Duration]string{
		time.Hour: "today",
		day:       "1 day ago",
		3 * day:   "3 days ago",
		29 * day:  "29 days ago",
		45 * day:  "1 month ago",
		95 * day:  "3 months ago",
	} {
		if got := formatAge(now.Add(-age), now); got != want {
			t.Errorf("formatAge(-%v) = %q, want %q", age, got, want)
		}
	}
	if got := formatAge(time.Time{}, now); got != "unknown" {
		t.Errorf("zero time: %q", got)
	}
}

// --- settings and commands --------------------------------------------------------

func TestPrefsFallbacks(t *testing.T) {
	home, _ := os.UserHomeDir()
	p := prefsFrom(settingsFor(t, "stale-days = 0\nroot = \"work/tmp\"\neditor = \"/Applications/Visual Studio Code.app\"\n"))
	if p.staleDays != 7 {
		t.Errorf("stale-days 0 should fall back to 7, got %d", p.staleDays)
	}
	if p.root != filepath.Join(home, "work", "tmp") {
		t.Errorf("relative root should be taken from home, got %s", p.root)
	}
	if p.editorName() != "Code" {
		t.Errorf("editorName() = %q, want Code", p.editorName())
	}
}

func TestCreateScratchCommand(t *testing.T) {
	copied, opened, _ := stubSystem(t)
	root := t.TempDir()

	ctx := &fakeCtx{settings: settingsFor(t, "root = \""+root+"\"\n"), args: map[string]string{"label": "bug 42"}}
	if err := createScratch(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ctx.hud, "Created bug-42-") || len(*opened) != 1 || !strings.HasSuffix((*opened)[0], "with Visual Studio Code") {
		t.Fatalf("hud=%q opened=%v", ctx.hud, *opened)
	}

	ctx = &fakeCtx{settings: settingsFor(t, "root = \""+root+"\"\nopen-in-editor = false\n")}
	if err := createScratch(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ctx.hud, "— path copied") || len(*copied) != 1 || !strings.HasPrefix((*copied)[0], root) {
		t.Fatalf("hud=%q copied=%v", ctx.hud, *copied)
	}
}

func TestFoldersContent(t *testing.T) {
	root := t.TempDir()
	mkdirAged(t, filepath.Join(root, "fresh-one"), time.Hour)
	path, _ := createFolder(root, "words", "labelled", time.Now())
	mkdirAged(t, filepath.Join(root, "old-one"), 40*day)

	p := prefsFrom(settingsFor(t, "root = \""+root+"\"\n"))
	content, err := foldersContent(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	active, stale := content.Sections[0], content.Sections[1]
	if active.Title != "Active" || active.Subtitle != "2" || stale.Title != "Stale — untouched 7+ days" || stale.Subtitle != "1" {
		t.Fatalf("sections: %q %q / %q %q", active.Title, active.Subtitle, stale.Title, stale.Subtitle)
	}
	old := stale.Items[0]
	if old.Title != "old-one" || old.Accessories[0].Text != "no marker" || old.Accessories[1] != (kit.Accessory{Text: "1 month ago", Color: kit.Red}) {
		t.Fatalf("stale item: %+v", old)
	}
	for _, it := range active.Items {
		if it.Title == filepath.Base(path) && (it.Subtitle != "labelled" || it.Accessories[0].Text == "no marker") {
			t.Fatalf("labelled item: %+v", it)
		}
	}
	if old.Actions[0].Title != "Open in Code" {
		t.Fatalf("primary action = %q", old.Actions[0].Title)
	}
	shortcuts := map[string]string{}
	for _, a := range old.Actions {
		if a.Shortcut != "" {
			shortcuts[a.Shortcut] = a.Title
		}
	}
	for key, title := range map[string]string{
		"ctrl+t": "Open in Terminal", "ctrl+g": "Open in lazygit", "ctrl+w": "Open Git Remote in Browser",
		"ctrl+f": "Show in Finder", "ctrl+o": "Open With…", "ctrl+y": "Copy Path", "ctrl+r": "Refresh",
		"ctrl+x": "Move to Trash", "ctrl+s": "Clean up All Stale Folders",
	} {
		if shortcuts[key] != title {
			t.Errorf("%s = %q, want %q", key, shortcuts[key], title)
		}
	}
}

func TestCleanUpStaleRemovesOnlyStaleAfterConfirming(t *testing.T) {
	stubSystem(t)
	root := t.TempDir()
	mkdirAged(t, filepath.Join(root, "keep"), time.Hour)
	mkdirAged(t, filepath.Join(root, "old-a"), 30*day)
	mkdirAged(t, filepath.Join(root, "old-b"), 60*day)
	settings := settingsFor(t, "root = \""+root+"\"\ndelete-mode = \"permanent\"\n")
	p := prefsFrom(settings)

	run := func(answer bool) *fakeCtx {
		content, err := foldersContent(p, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ctx := &fakeCtx{settings: settings, answer: answer}
		for _, a := range content.Sections[0].Items[0].Actions {
			if a.Title == "Clean up All Stale Folders" {
				if err := a.Run(ctx); err != nil {
					t.Fatal(err)
				}
			}
		}
		return ctx
	}

	ctx := run(false)
	if len(ctx.confirms) != 1 || !strings.Contains(ctx.confirms[0].Message, "• old-b (2 months ago)") {
		t.Fatalf("confirms = %+v", ctx.confirms)
	}
	if _, err := os.Stat(filepath.Join(root, "old-a")); err != nil {
		t.Fatal("cancelling removed a folder")
	}

	ctx = run(true)
	for _, name := range []string{"old-a", "old-b"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Fatal("removed an active folder")
	}
	if last := ctx.toasts[len(ctx.toasts)-1]; last != "Removed 2 of 2 folders" || ctx.refreshed != 1 {
		t.Fatalf("toasts=%v refreshed=%d", ctx.toasts, ctx.refreshed)
	}
}
