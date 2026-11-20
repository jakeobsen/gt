package vscode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// project is one entry from Project Manager's projects.json or its caches.
type project struct {
	name     string
	rootPath string
	tags     []string
	enabled  bool
}

// noTags is the section untagged projects are grouped under.
const noTags = "[no tags]"

// appShortName is the name VS Code uses for its support folder: "Visual
// Studio Code.app" → "Code", "Visual Studio Code - Insiders.app" → "Code -
// Insiders".
func appShortName(appPath string) string {
	base := strings.TrimSuffix(filepath.Base(appPath), ".app")
	return strings.TrimPrefix(base, "Visual Studio ")
}

// defaultStoragePath is where the Project Manager extension keeps its files
// for the given VS Code app.
func defaultStoragePath(appPath string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", appShortName(appPath),
		"User", "globalStorage", "alefragnani.project-manager")
}

// projectsLocation resolves the projects-location setting: empty means the
// default storage path; a file means the folder it is in.
func projectsLocation(configured, appPath string) (string, error) {
	if configured == "" {
		return defaultStoragePath(appPath), nil
	}
	info, err := os.Stat(configured)
	if err != nil {
		return configured, fmt.Errorf("projects location does not exist: %s", configured)
	}
	switch {
	case info.IsDir():
		return configured, nil
	case info.Mode().IsRegular():
		return filepath.Dir(configured), nil
	}
	return configured, fmt.Errorf("projects location is not a folder: %s", configured)
}

// loadProjects reads projects.json and merges in the projects_cache_*.json
// files the Project Manager extension writes, skipping entries that are
// malformed or already listed.
func loadProjects(dir string, hideUntagged, hideDisabled bool) ([]project, error) {
	var projects []project
	seen := map[string]bool{}

	var saved []map[string]any
	if err := readJSON(filepath.Join(dir, "projects.json"), &saved); err != nil {
		return nil, err
	}
	for _, e := range saved {
		name, ok1 := e["name"].(string)
		root, ok2 := e["rootPath"].(string)
		if !ok1 || !ok2 {
			continue
		}
		enabled, isBool := e["enabled"].(bool)
		projects = append(projects, project{name: name, rootPath: root, tags: strings_(e["tags"]), enabled: !isBool || enabled})
		seen[root] = true
	}

	for _, cache := range []string{"projects_cache_git.json", "projects_cache_any.json", "projects_cache_vscode.json"} {
		var cached []map[string]any
		if err := readJSON(filepath.Join(dir, cache), &cached); err != nil {
			return nil, err
		}
		for _, e := range cached {
			name, ok1 := e["name"].(string)
			full, ok2 := e["fullPath"].(string)
			if !ok1 || !ok2 || seen[full] {
				continue
			}
			projects = append(projects, project{name: name, rootPath: full, enabled: true})
			seen[full] = true
		}
	}

	return slices.DeleteFunc(projects, func(p project) bool {
		return (hideUntagged && len(p.tags) == 0) || (hideDisabled && !p.enabled)
	}), nil
}

// readJSON decodes path into v; a missing file leaves v alone.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	return nil
}

// strings_ keeps the string elements of a JSON array.
func strings_(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// projectTags lists every tag, sorted ignoring case.
func projectTags(projects []project) []string {
	set := map[string]bool{}
	for _, p := range projects {
		for _, t := range p.tags {
			set[t] = true
		}
	}
	tags := make([]string, 0, len(set))
	for t := range set {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool { return strings.ToLower(tags[i]) < strings.ToLower(tags[j]) })
	return tags
}

// groupByTag puts each project under every tag it has (untagged ones under
// noTags), keeping their order, with the tags sorted.
func groupByTag(projects []project) (tags []string, groups map[string][]project) {
	groups = map[string][]project{}
	for _, p := range projects {
		ts := p.tags
		if len(ts) == 0 {
			ts = []string{noTags}
		}
		for _, t := range ts {
			if _, ok := groups[t]; !ok {
				tags = append(tags, t)
			}
			groups[t] = append(groups[t], p)
		}
	}
	sort.Strings(tags)
	return tags, groups
}

func filterByTag(projects []project, tag string) []project {
	if tag == "" {
		return projects
	}
	return slices.DeleteFunc(slices.Clone(projects), func(p project) bool { return !slices.Contains(p.tags, tag) })
}
