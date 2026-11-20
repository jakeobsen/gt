// Package vscode searches and opens the projects saved by the VS Code
// Project Manager extension (alefragnani.project-manager). It is ported from
// the Raycast "better-visual-studio-code-project-manager" extension, itself a
// fork of Raycast's MIT-licensed "visual-studio-code-project-manager"; see
// LICENSE-upstream.
package vscode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jakeobsen/gt/kit"
	"github.com/jakeobsen/gt/plugins/internal/git"
)

const remotePrefix = "vscode-remote://"

// System calls, replaced in tests so they never touch real apps or the Trash.
var (
	openPath  = kit.Open
	trashPath = kit.Trash
	runCode   = func(cli string, args ...string) error { return exec.Command(cli, args...).Start() }
)

// Plugin returns the VS Code projects plugin.
func Plugin() kit.Plugin {
	return kit.Plugin{
		Name:        "vscode-projects",
		Title:       "Visual Studio Code",
		Description: "Search and open projects from the VS Code Project Manager extension.",
		Settings: []kit.Setting{
			{Key: "vscode-app", Kind: kit.Path, Default: "/Applications/Visual Studio Code.app",
				Description: "The VS Code app projects open in. It also decides where Project Manager's projects.json is looked for."},
			{Key: "projects-location", Kind: kit.Path, Default: "",
				Description: "Folder containing projects.json (and any projects_cache_*.json). Empty means Project Manager's own storage folder for vscode-app."},
			{Key: "group-by-tag", Kind: kit.Bool, Default: true,
				Description: "Group projects under their tags instead of one list."},
			{Key: "hide-untagged", Kind: kit.Bool, Default: false,
				Description: "Hide projects that have no tags."},
			{Key: "hide-disabled", Kind: kit.Bool, Default: false,
				Description: "Hide projects whose enabled is false in projects.json."},
			{Key: "git-client", Kind: kit.String, Default: "lazygit",
				Description: "Command the git client action runs in a terminal at the project, e.g. lazygit, gitui or tig."},
		},
		Commands: []kit.Command{{
			Name:        "search-projects",
			Title:       "Search Project Manager",
			Description: "Search projects from projects.json and open them.",
			Keywords:    []string{"vscode", "code", "projects"},
			Open:        projectsList,
		}},
	}
}

type prefs struct {
	app          string
	location     string
	groupByTag   bool
	hideUntagged bool
	hideDisabled bool
	gitClient    string
}

func prefsFrom(s kit.Settings) prefs {
	p := prefs{
		app:          s.String("vscode-app"),
		location:     strings.TrimSpace(s.String("projects-location")),
		groupByTag:   s.Bool("group-by-tag"),
		hideUntagged: s.Bool("hide-untagged"),
		hideDisabled: s.Bool("hide-disabled"),
		gitClient:    strings.TrimSpace(s.String("git-client")),
	}
	if p.gitClient == "" {
		p.gitClient = "lazygit"
	}
	return p
}

// appName is the full app name, e.g. "Visual Studio Code".
func (p prefs) appName() string { return strings.TrimSuffix(filepath.Base(p.app), ".app") }

func projectsList(ctx kit.Context) (kit.View, error) {
	p := prefsFrom(ctx.Settings())
	list := &kit.List{
		SearchPlaceholder: "Search projects…",
		Load:              func(ctx kit.Context) (kit.Content, error) { return content(ctx, p) },
	}
	// The tag filter needs the tags up front; if projects can't be read,
	// Load reports why.
	if dir, err := projectsLocation(p.location, p.app); err == nil {
		if projects, err := loadProjects(dir, p.hideUntagged, p.hideDisabled); err == nil {
			if tags := projectTags(projects); len(tags) > 0 {
				f := &kit.Filter{Title: "Tag", Options: []kit.FilterOption{{Title: "All Tags", Value: ""}}}
				for _, t := range tags {
					f.Options = append(f.Options, kit.FilterOption{Title: t, Value: t})
				}
				list.Filter = f
			}
		}
	}
	return list, nil
}

func refreshAction() kit.Action {
	return kit.Action{Title: "Refresh", Shortcut: "ctrl+r", Run: func(ctx kit.Context) error {
		ctx.Refresh()
		return nil
	}}
}

func content(ctx kit.Context, p prefs) (kit.Content, error) {
	if _, err := os.Stat(p.app); err != nil {
		return kit.Content{Empty: kit.Empty{
			Title:       "Visual Studio Code not found",
			Description: "No app at " + tildify(p.app) + ". Set vscode-app in [plugins.vscode-projects] (gt --config-path).",
			Actions:     []kit.Action{refreshAction()},
		}}, nil
	}
	dir, err := projectsLocation(p.location, p.app)
	if err != nil {
		return kit.Content{Empty: kit.Empty{
			Title:       "Invalid projects location",
			Description: capitalize(err.Error()) + ". Check projects-location in [plugins.vscode-projects].",
			Actions:     []kit.Action{refreshAction()},
		}}, nil
	}
	projects, err := loadProjects(dir, p.hideUntagged, p.hideDisabled)
	if err != nil {
		return kit.Content{}, err
	}
	if len(projects) == 0 {
		return kit.Content{Empty: kit.Empty{
			Title:       "No projects found",
			Description: "Make sure projects.json exists in " + tildify(dir) + ".",
			Actions:     []kit.Action{kit.RevealAction(dir, ""), refreshAction()},
		}}, nil
	}

	rank := kit.NewFrecency(ctx.Storage(), "projects")
	kit.SortByFrecency(rank, projects, func(pr project) string { return pr.rootPath }, func(a, b project) int {
		return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})

	tag := ctx.Filter()
	var sections []kit.Section
	if p.groupByTag && tag == "" {
		tags, groups := groupByTag(projects)
		for _, t := range tags {
			s := kit.Section{Title: t}
			for _, pr := range groups[t] {
				s.Items = append(s.Items, item(p, pr, rank))
			}
			sections = append(sections, s)
		}
	} else {
		s := kit.Section{}
		for _, pr := range filterByTag(projects, tag) {
			s.Items = append(s.Items, item(p, pr, rank))
		}
		sections = append(sections, s)
	}
	return kit.Content{Sections: sections, Empty: kit.Empty{Title: "No projects with this tag"}}, nil
}

// item is one project row. Its actions mirror the Raycast extension; ⌘
// shortcuts became ctrl ones, as in the scratch plugin.
func item(p prefs, pr project, rank *kit.Frecency) kit.Item {
	pretty := tildify(pr.rootPath)
	it := kit.Item{Title: pr.name, Subtitle: filepath.Dir(pretty), Keywords: pr.tags}
	if len(pr.tags) > 0 {
		it.Accessories = []kit.Accessory{{Text: strings.Join(pr.tags, ", ")}}
	}
	visit := func() { _ = rank.Visit(pr.rootPath) }
	root := pr.rootPath
	remote := strings.HasPrefix(root, remotePrefix)

	var acts []kit.Action
	if remote {
		acts = append(acts, kit.Action{Title: "Open in " + p.appName() + " (Remote)", Run: func(ctx kit.Context) error {
			visit()
			authority, path := remoteTarget(root)
			cli := filepath.Join(p.app, "Contents", "Resources", "app", "bin", "code")
			if err := runCode(cli, "--remote", authority, path); err != nil {
				return err
			}
			ctx.Close()
			return nil
		}})
	} else {
		acts = append(acts,
			kit.Action{Title: "Open in " + appShortName(p.app), Run: func(ctx kit.Context) error {
				visit()
				if err := openPath(root, p.app); err != nil {
					return err
				}
				ctx.Close()
				return nil
			}},
			kit.Action{Title: "Open in Terminal", Shortcut: "ctrl+t", Run: func(ctx kit.Context) error {
				visit()
				if err := ctx.OpenTerminal(root, ""); err != nil {
					return err
				}
				ctx.Close()
				return nil
			}},
			kit.Action{Title: "Open in " + p.gitClient, Shortcut: "ctrl+g", Run: func(ctx kit.Context) error {
				if !git.IsRepo(root) {
					ctx.Toast(kit.Failure, "Not a git repository", "")
					return nil
				}
				visit()
				if err := ctx.OpenTerminal(root, p.gitClient); err != nil {
					return fmt.Errorf("failed to launch %s: %w", p.gitClient, err)
				}
				ctx.Close()
				return nil
			}},
			kit.Action{Title: "Open Git Remote in Browser", Shortcut: "ctrl+w", Run: func(ctx kit.Context) error {
				url, err := git.RemoteWebURL(root)
				if err != nil {
					ctx.Toast(kit.Failure, capitalize(err.Error()), "")
					return nil
				}
				visit()
				if err := openPath(url, ""); err != nil {
					return err
				}
				ctx.Close()
				return nil
			}},
			kit.RevealAction(root, "ctrl+f"),
			kit.Action{Title: "Open With…", Shortcut: "ctrl+o", Run: func(ctx kit.Context) error {
				visit()
				return kit.OpenWithAction(root, "").Run(ctx)
			}},
		)
	}
	acts = append(acts,
		kit.Divider,
		kit.CopyAction("Copy Name", pr.name, ""),
		kit.CopyAction("Copy Path", pretty, "ctrl+y"),
		kit.Divider,
		kit.Action{Title: "Reset Project Ranking", Run: func(ctx kit.Context) error {
			if err := rank.Reset(root); err != nil {
				return err
			}
			ctx.Refresh()
			return nil
		}},
	)
	if !remote {
		acts = append(acts, kit.Action{Title: "Move to Trash", Shortcut: "ctrl+x", Destructive: true, Run: func(ctx kit.Context) error {
			if !ctx.Confirm(kit.Confirmation{
				Title: "Move to Trash?", Message: pretty, Action: "Move to Trash", Destructive: true,
			}) {
				return nil
			}
			if err := trashPath(root); err != nil {
				return err
			}
			ctx.Toast(kit.Success, "Moved "+pr.name+" to the Trash", "")
			ctx.Refresh()
			return nil
		}})
	}
	it.Actions = acts
	return it
}

// remoteTarget splits "vscode-remote://ssh-remote+host/path" into the
// authority and folder that `code --remote` takes.
func remoteTarget(root string) (authority, path string) {
	rest := strings.TrimPrefix(root, remotePrefix)
	i := strings.Index(rest, "/")
	if i < 0 {
		return rest, "/"
	}
	return rest[:i], rest[i:] + "/"
}

func tildify(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
