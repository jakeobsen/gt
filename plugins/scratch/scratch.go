// Package scratch makes disposable working folders and cleans up the ones
// nobody has touched for a while. It is ported from the Raycast "scratch"
// extension.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jakeobsen/gt/kit"
	"github.com/jakeobsen/gt/plugins/internal/git"
)

const defaultStaleDays = 7

// Plugin returns the scratch plugin.
func Plugin() kit.Plugin {
	return kit.Plugin{
		Name:        "scratch",
		Title:       "Scratch",
		Description: "Create disposable scratch folders and clean up the ones you have stopped touching.",
		Settings: []kit.Setting{
			{Key: "root", Kind: kit.Path, Default: "~/scratch",
				Description: "Folder that holds the scratch folders. Created on first use; a relative path is taken from your home folder."},
			{Key: "editor", Kind: kit.String, Default: "Visual Studio Code",
				Description: "App that opens scratch folders: an app name or a path to a .app."},
			{Key: "open-in-editor", Kind: kit.Bool, Default: true,
				Description: "Open a new scratch folder in the editor. When false, its path is copied to the clipboard instead."},
			{Key: "stale-days", Kind: kit.Int, Default: defaultStaleDays,
				Description: "A folder counts as stale when nothing anywhere inside it has changed for this many days. Values below 1 mean 7."},
			{Key: "name-style", Kind: kit.Choice, Default: "words",
				Description: "Shape of generated folder names.",
				Options: []kit.Option{
					{Value: "words", Description: "brave-otter"},
					{Value: "words-suffix", Description: "brave-otter-a1b2"},
					{Value: "date-words", Description: "2026-08-11-brave-otter"},
					{Value: "hex", Description: "scratch-8f3a2c4d"},
				}},
			{Key: "git-client", Kind: kit.String, Default: "lazygit",
				Description: "Command the git client action runs in a terminal at the folder, e.g. lazygit, gitui or tig."},
			{Key: "delete-mode", Kind: kit.Choice, Default: "trash",
				Description: "How folders are removed.",
				Options: []kit.Option{
					{Value: "trash", Description: "move to the Trash, recoverable from Finder"},
					{Value: "permanent", Description: "delete permanently, not recoverable"},
				}},
		},
		Commands: []kit.Command{
			{
				Name:        "create-scratch",
				Title:       "Create Scratch",
				Description: "Creates a randomly named folder under the scratch root and opens it in your editor.",
				Keywords:    []string{"new", "folder", "tmp"},
				Arguments:   []kit.Argument{{Name: "label", Placeholder: "label (optional)"}},
				Run:         createScratch,
			},
			{
				Name:        "scratch-folders",
				Title:       "Scratch Folders",
				Description: "Browse scratch folders, open them, and clean up the stale ones.",
				Keywords:    []string{"clean", "stale", "folders"},
				Open:        func(ctx kit.Context) (kit.View, error) { return foldersList(ctx), nil },
			},
		},
	}
}

type prefs struct {
	root         string
	editor       string
	openInEditor bool
	staleDays    int
	nameStyle    string
	gitClient    string
	deleteMode   string
}

func prefsFrom(s kit.Settings) prefs {
	p := prefs{
		root:         s.String("root"),
		editor:       strings.TrimSpace(s.String("editor")),
		openInEditor: s.Bool("open-in-editor"),
		staleDays:    s.Int("stale-days"),
		nameStyle:    s.String("name-style"),
		gitClient:    strings.TrimSpace(s.String("git-client")),
		deleteMode:   s.String("delete-mode"),
	}
	home, _ := os.UserHomeDir()
	switch {
	case strings.TrimSpace(p.root) == "":
		p.root = filepath.Join(home, "scratch")
	case !filepath.IsAbs(p.root):
		p.root = filepath.Join(home, p.root)
	}
	p.root = filepath.Clean(p.root)
	// A 0-day threshold would make every folder stale — exactly the mistake
	// you cannot undo — so anything below 1 means the default.
	if p.staleDays < 1 {
		p.staleDays = defaultStaleDays
	}
	if p.editor == "" {
		p.editor = "Visual Studio Code"
	}
	if p.gitClient == "" {
		p.gitClient = "lazygit"
	}
	return p
}

// editorName is the editor's short name for action titles: "Visual Studio
// Code" (or its .app path) becomes "Code".
func (p prefs) editorName() string {
	name := strings.TrimSuffix(filepath.Base(p.editor), ".app")
	return strings.TrimPrefix(name, "Visual Studio ")
}

func (p prefs) deleteVerb() string {
	if p.deleteMode == "permanent" {
		return "Delete Permanently"
	}
	return "Move to Trash"
}

// createScratch is the Create Scratch command.
func createScratch(ctx kit.Context) error {
	p := prefsFrom(ctx.Settings())
	path, err := createFolder(p.root, p.nameStyle, ctx.Argument("label"), time.Now())
	if err != nil {
		return fmt.Errorf("could not create scratch folder: %w", err)
	}
	name := filepath.Base(path)
	if !p.openInEditor {
		if err := copyText(path); err != nil {
			return fmt.Errorf("created %s, but could not copy its path: %w", name, err)
		}
		ctx.HUD("Created " + name + " — path copied")
		return nil
	}
	if err := openPath(path, p.editor); err != nil {
		_ = copyText(path)
		ctx.Toast(kit.Failure, "Created "+name+", but could not open "+p.editorName(), "Path copied to clipboard. "+err.Error())
		return nil
	}
	ctx.HUD("Created " + name)
	return nil
}

// foldersList is the Scratch Folders screen.
func foldersList(ctx kit.Context) *kit.List {
	p := prefsFrom(ctx.Settings())
	return &kit.List{
		SearchPlaceholder: "Search scratch folders in " + tildify(p.root),
		Load: func(ctx kit.Context) (kit.Content, error) {
			return foldersContent(p, time.Now())
		},
	}
}

func foldersContent(p prefs, now time.Time) (kit.Content, error) {
	folders, err := listFolders(p.root, p.staleDays, now)
	if err != nil {
		return kit.Content{}, fmt.Errorf("could not read scratch folders: %w", err)
	}
	// Browsing wants the freshest first; cleanup wants the mustiest first.
	var active, stale []folder
	for _, f := range folders {
		if f.stale {
			stale = append(stale, f)
		} else {
			active = append([]folder{f}, active...)
		}
	}

	item := func(f folder) kit.Item {
		var accs []kit.Accessory
		if f.meta == nil {
			accs = append(accs, kit.Accessory{Text: "no marker"})
		}
		if f.truncated {
			accs = append(accs, kit.Accessory{Text: "large tree", Color: kit.Orange})
		}
		ageColor := kit.Dim
		if f.stale {
			ageColor = kit.Red
		}
		accs = append(accs, kit.Accessory{Text: formatAge(f.lastModified, now), Color: ageColor})

		it := kit.Item{Title: f.name, Accessories: accs, Actions: folderActions(p, f, stale, now)}
		if f.meta != nil && f.meta.Label != "" {
			it.Subtitle = f.meta.Label
			it.Keywords = strings.Fields(f.meta.Label)
		}
		return it
	}
	section := func(title string, fs []folder) kit.Section {
		s := kit.Section{Title: title}
		if len(fs) > 0 {
			s.Subtitle = fmt.Sprint(len(fs))
		}
		for _, f := range fs {
			s.Items = append(s.Items, item(f))
		}
		return s
	}

	return kit.Content{
		Sections: []kit.Section{
			section("Active", active),
			section(fmt.Sprintf("Stale — untouched %d+ days", p.staleDays), stale),
		},
		Empty: kit.Empty{
			Title:       "No scratch folders",
			Description: "Nothing in " + tildify(p.root) + " yet. Press enter to make one.",
			Actions: []kit.Action{
				{Title: "Create Scratch Folder", Run: func(ctx kit.Context) error { return createAndOpen(ctx, p) }},
				{Title: "Open Scratch Root", Run: func(ctx kit.Context) error {
					if err := os.MkdirAll(p.root, 0o755); err != nil {
						return err
					}
					if err := openPath(p.root, ""); err != nil {
						return err
					}
					ctx.Close()
					return nil
				}},
				refreshAction(),
			},
		},
	}, nil
}

func refreshAction() kit.Action {
	return kit.Action{Title: "Refresh", Shortcut: "ctrl+r", Run: func(ctx kit.Context) error {
		ctx.Refresh()
		return nil
	}}
}

// folderActions mirror the Raycast extension's action panel. Terminals can't
// send ⌘, so ⌘-shortcuts became ctrl ones; see the plugin's README.
func folderActions(p prefs, f folder, stale []folder, now time.Time) []kit.Action {
	return []kit.Action{
		kit.OpenAction("Open in "+p.editorName(), f.path, p.editor, ""),
		{Title: "Open in Terminal", Shortcut: "ctrl+t", Run: func(ctx kit.Context) error {
			if err := ctx.OpenTerminal(f.path, ""); err != nil {
				return err
			}
			ctx.Close()
			return nil
		}},
		{Title: "Open in " + p.gitClient, Shortcut: "ctrl+g", Run: func(ctx kit.Context) error {
			if !git.IsRepo(f.path) {
				ctx.Toast(kit.Failure, "Not a git repository", "")
				return nil
			}
			if err := ctx.OpenTerminal(f.path, p.gitClient); err != nil {
				return fmt.Errorf("failed to launch %s: %w", p.gitClient, err)
			}
			ctx.Close()
			return nil
		}},
		{Title: "Open Git Remote in Browser", Shortcut: "ctrl+w", Run: func(ctx kit.Context) error {
			url, err := git.RemoteWebURL(f.path)
			if err != nil {
				ctx.Toast(kit.Failure, capitalize(err.Error()), "")
				return nil
			}
			if err := openPath(url, ""); err != nil {
				return err
			}
			ctx.Close()
			return nil
		}},
		kit.RevealAction(f.path, "ctrl+f"),
		kit.OpenWithAction(f.path, "ctrl+o"),
		kit.Divider,
		kit.CopyAction("Copy Name", f.name, ""),
		kit.CopyAction("Copy Path", f.path, "ctrl+y"),
		kit.Divider,
		{Title: "Create Scratch Folder", Run: func(ctx kit.Context) error { return createAndOpen(ctx, p) }},
		refreshAction(),
		kit.Divider,
		{Title: p.deleteVerb(), Shortcut: "ctrl+x", Destructive: true, Run: func(ctx kit.Context) error {
			return removeOne(ctx, p, f, now)
		}},
		{Title: "Clean up All Stale Folders", Shortcut: "ctrl+s", Destructive: true, Run: func(ctx kit.Context) error {
			return removeStale(ctx, p, stale, now)
		}},
	}
}

func createAndOpen(ctx kit.Context, p prefs) error {
	path, err := createFolder(p.root, p.nameStyle, "", time.Now())
	if err != nil {
		return fmt.Errorf("could not create scratch folder: %w", err)
	}
	if p.openInEditor {
		if err := openPath(path, p.editor); err != nil {
			return err
		}
		ctx.Close()
		return nil
	}
	ctx.Toast(kit.Success, "Created "+tildify(path), "")
	ctx.Refresh()
	return nil
}

func removeOne(ctx kit.Context, p prefs, f folder, now time.Time) error {
	if !ctx.Confirm(kit.Confirmation{
		Title:       p.deleteVerb() + "?",
		Message:     tildify(f.path) + "\nLast modified " + formatAge(f.lastModified, now) + ".",
		Action:      p.deleteVerb(),
		Destructive: true,
	}) {
		return nil
	}
	if err := removeFolder(p.root, f.path, p.deleteMode); err != nil {
		ctx.Toast(kit.Failure, "Could not remove "+f.name, err.Error())
	} else {
		ctx.Toast(kit.Success, "Removed "+f.name, "")
	}
	ctx.Refresh()
	return nil
}

func removeStale(ctx kit.Context, p prefs, stale []folder, now time.Time) error {
	if len(stale) == 0 {
		ctx.Toast(kit.Success, "Nothing to clean up", "")
		return nil
	}
	var preview []string
	for i, f := range stale {
		if i == 10 {
			preview = append(preview, fmt.Sprintf("• …and %d more", len(stale)-10))
			break
		}
		preview = append(preview, fmt.Sprintf("• %s (%s)", f.name, formatAge(f.lastModified, now)))
	}
	if !ctx.Confirm(kit.Confirmation{
		Title:       fmt.Sprintf("%s: %d scratch %s?", p.deleteVerb(), len(stale), plural(len(stale), "folder")),
		Message:     fmt.Sprintf("Untouched for at least %d %s:\n%s", p.staleDays, plural(p.staleDays, "day"), strings.Join(preview, "\n")),
		Action:      fmt.Sprintf("%s (%d)", p.deleteVerb(), len(stale)),
		Destructive: true,
	}) {
		return nil
	}

	toast := ctx.Toast(kit.Loading, fmt.Sprintf("Cleaning up %d %s…", len(stale), plural(len(stale), "folder")), "")
	removed := 0
	var failures []string
	for _, f := range stale {
		if err := removeFolder(p.root, f.path, p.deleteMode); err != nil {
			failures = append(failures, f.name+": "+err.Error())
			continue
		}
		removed++
	}
	style := kit.Success
	if len(failures) > 0 {
		style = kit.Failure
	}
	toast.Update(style, fmt.Sprintf("Removed %d of %d %s", removed, len(stale), plural(len(stale), "folder")), strings.Join(failures, "\n"))
	ctx.Refresh()
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
