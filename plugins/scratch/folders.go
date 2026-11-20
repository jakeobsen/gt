package scratch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jakeobsen/gt/kit"
)

// markerFile is written into every folder Create Scratch makes. Its format is
// shared with the Raycast extension, so folders made by either are recognised.
const markerFile = ".scratch.json"

// scanEntryCap bounds the entries visited while dating one folder. A
// node_modules tree can hold six figures of files; past the cap the scan
// stops and reports truncated, which callers treat as "not stale" so an
// unfinished scan can never authorise a delete.
var scanEntryCap = 20_000

const day = 24 * time.Hour

type meta struct {
	CreatedAt string `json:"createdAt,omitempty"`
	Label     string `json:"label,omitempty"`
}

type folder struct {
	name string
	path string
	// lastModified is the newest modification time anywhere inside.
	lastModified time.Time
	// stale is set when nothing inside changed within the threshold.
	stale bool
	// truncated is set when the scan hit scanEntryCap, so lastModified is
	// only a lower bound.
	truncated bool
	// meta is set when the folder has a marker written by Create Scratch.
	meta *meta
}

// skippable reports errors that mean "nothing to date here": the entry
// vanished mid-scan or can't be read.
func skippable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR)
}

// scanActivity finds the newest modification time inside dir, stopping as
// soon as anything newer than cutoff turns up — the stale/active decision
// needs no more, and active folders are the expensive ones to walk. A
// directory's own mtime only tracks its direct children, so a full walk is
// the only honest answer to "has anything in here changed?".
func scanActivity(dir string, cutoff time.Time) (newest time.Time, fresh, truncated bool, err error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return time.Time{}, false, false, nil
	}
	newest = info.ModTime()
	if newest.After(cutoff) {
		return newest, true, false, nil
	}

	visited := 0
	queue := []string{dir}
	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		entries, err := os.ReadDir(current)
		if err != nil {
			if skippable(err) {
				continue
			}
			return newest, false, false, err
		}
		for _, e := range entries {
			visited++
			if visited > scanEntryCap {
				return newest, false, true, nil
			}
			info, err := e.Info() // the entry itself; symlinks are not followed
			if err != nil {
				continue
			}
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			if newest.After(cutoff) {
				return newest, true, false, nil
			}
			// Symlinked directories are deliberately not followed: they can
			// leave the scratch tree entirely, and they can loop.
			if e.IsDir() {
				queue = append(queue, filepath.Join(current, e.Name()))
			}
		}
	}
	return newest, false, false, nil
}

// listFolders lists the scratch folders directly under root, dated and
// classified as stale or active, oldest first. A missing root has no folders.
func listFolders(root string, staleDays int, now time.Time) ([]folder, error) {
	cutoff := now.Add(-time.Duration(staleDays) * day)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	// Only real, non-hidden directories one level down. Loose files and
	// dotfolders in the root are never candidates for deletion.
	var candidates []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			candidates = append(candidates, e.Name())
		}
	}

	folders := make([]folder, len(candidates))
	errs := make([]error, len(candidates))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i, name := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			path := filepath.Join(root, name)
			newest, fresh, truncated, err := scanActivity(path, cutoff)
			folders[i] = folder{
				name:         name,
				path:         path,
				lastModified: newest,
				// A truncated scan never counts as stale: an unknown answer
				// must not delete data.
				stale:     !fresh && !truncated,
				truncated: truncated,
				meta:      readMeta(path),
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sort.SliceStable(folders, func(i, j int) bool { return folders[i].lastModified.Before(folders[j].lastModified) })
	return folders, nil
}

func readMeta(path string) *meta {
	data, err := os.ReadFile(filepath.Join(path, markerFile))
	if err != nil {
		return nil
	}
	var m meta
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return &m
}

// createFolder makes a uniquely named folder under root and writes the marker
// into it. Uniqueness comes from mkdir itself (no exists-then-create race):
// when the name is taken, the next candidate is tried.
func createFolder(root, style, label string, now time.Time) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for attempt := range 20 {
		base := generateName(style, label, now)
		name := base
		// The first few attempts re-roll the random name; after that,
		// disambiguate with a counter.
		if attempt >= 5 {
			name = fmt.Sprintf("%s-%d", base, attempt-3)
		}
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		m := meta{CreatedAt: now.UTC().Format("2006-01-02T15:04:05.000Z"), Label: strings.TrimSpace(label)}
		data, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(path, markerFile), append(data, '\n'), 0o644); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("could not find an unused folder name under %s", root)
}

// isDirectChildOf guards deletes: nothing that isn't directly inside the
// scratch root may be removed, whatever a setting or a path says.
func isDirectChildOf(root, path string) bool {
	r, err1 := filepath.Abs(root)
	p, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	return p != r && filepath.Dir(p) == r
}

// removeFolder removes one scratch folder: to the Trash, or permanently.
func removeFolder(root, path, mode string) error {
	if !isDirectChildOf(root, path) {
		return fmt.Errorf("refusing to delete %s: not directly inside %s", path, root)
	}
	if mode == "permanent" {
		return os.RemoveAll(path)
	}
	return trash(path)
}

// System calls, replaced in tests so they never touch the real clipboard,
// Trash or apps.
var (
	trash    = kit.Trash
	copyText = kit.Copy
	openPath = kit.Open
)

// formatAge describes how long ago t was, in days or months.
func formatAge(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	days := int(now.Sub(t) / day)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day ago"
	case days < 30:
		return fmt.Sprintf("%d days ago", days)
	case days < 60:
		return "1 month ago"
	}
	return fmt.Sprintf("%d months ago", days/30)
}

// tildify shows paths under the home directory with ~.
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
