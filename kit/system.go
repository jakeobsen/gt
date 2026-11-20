package kit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Open opens target (a path or URL) with the default app, or with app when it
// isn't empty: an app name like "Visual Studio Code" or a path to a .app.
func Open(target, app string) error {
	args := []string{target}
	if app != "" {
		args = []string{"-a", app, target}
	}
	return run("/usr/bin/open", args...)
}

// Reveal shows path selected in a Finder window.
func Reveal(path string) error { return run("/usr/bin/open", "-R", path) }

// Copy puts text on the clipboard.
func Copy(text string) error {
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return wrap(cmd)
}

// Trash moves path to the Trash, where it can be restored from Finder.
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat("/usr/bin/trash"); err == nil {
		return run("/usr/bin/trash", abs)
	}
	// macOS before 15 has no trash command; ask Finder instead.
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file %s`, AppleScriptQuote(abs))
	return run("/usr/bin/osascript", "-e", script)
}

// App is an installed application.
type App struct {
	Name string
	Path string
}

// Applications lists the apps in the usual application folders, sorted by name.
func Applications() []App {
	dirs := []string{"/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	seen := map[string]bool{}
	var apps []App
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".app")
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			apps = append(apps, App{Name: name, Path: filepath.Join(dir, e.Name())})
		}
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return apps
}

// OpenAction opens target with app (or the default app) and closes goto,
// like Raycast's Action.Open.
func OpenAction(title, target, app, shortcut string) Action {
	return Action{Title: title, Shortcut: shortcut, Run: func(ctx Context) error {
		if err := Open(target, app); err != nil {
			return err
		}
		ctx.Close()
		return nil
	}}
}

// OpenURLAction opens url in the default browser and closes goto, like
// Raycast's Action.OpenInBrowser.
func OpenURLAction(title, url, shortcut string) Action {
	return OpenAction(title, url, "", shortcut)
}

// RevealAction shows path in Finder and closes goto, like Raycast's
// Action.ShowInFinder.
func RevealAction(path, shortcut string) Action {
	return Action{Title: "Show in Finder", Shortcut: shortcut, Run: func(ctx Context) error {
		if err := Reveal(path); err != nil {
			return err
		}
		ctx.Close()
		return nil
	}}
}

// CopyAction copies text to the clipboard and closes goto with a message,
// like Raycast's Action.CopyToClipboard.
func CopyAction(title, text, shortcut string) Action {
	return Action{Title: title, Shortcut: shortcut, Run: func(ctx Context) error {
		if err := Copy(text); err != nil {
			return err
		}
		ctx.HUD("Copied to clipboard: " + text)
		return nil
	}}
}

// OpenWithAction lists the installed apps and opens path with the one picked,
// like Raycast's Action.OpenWith.
func OpenWithAction(path, shortcut string) Action {
	return Action{Title: "Open With…", Shortcut: shortcut, Run: func(ctx Context) error {
		ctx.Push(&List{
			Title:             "Open With",
			SearchPlaceholder: "Open " + filepath.Base(path) + " with…",
			Load: func(Context) (Content, error) {
				var items []Item
				for _, app := range Applications() {
					items = append(items, Item{
						Title:    app.Name,
						Subtitle: filepath.Dir(app.Path),
						Actions:  []Action{OpenAction("Open With "+app.Name, path, app.Path, "")},
					})
				}
				return Content{
					Sections: []Section{{Items: items}},
					Empty:    Empty{Title: "No applications found"},
				}, nil
			},
		})
		return nil
	}}
}

// ShellQuote wraps s in single quotes for a POSIX shell command line.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AppleScriptQuote makes s a double-quoted AppleScript string literal.
func AppleScriptQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func run(name string, args ...string) error { return wrap(exec.Command(name, args...)) }

// wrap runs cmd and, on failure, includes what it printed to stderr.
func wrap(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %s", filepath.Base(cmd.Path), msg)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("%s failed: %w", filepath.Base(cmd.Path), err)
		}
		return err
	}
	return nil
}
