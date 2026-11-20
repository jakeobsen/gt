// Package git answers the questions plugins ask about a folder's git
// repository — is it one, and where does its remote live on the web —
// by reading .git directly instead of running git.
package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IsRepo reports whether path is the root of a git repository. .git is a
// directory in a normal repository and a file in a worktree.
func IsRepo(path string) bool {
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

var gitdirLine = regexp.MustCompile(`(?m)^gitdir:\s*(.+)$`)

// gitDir finds the real git directory, following the "gitdir:" pointer a
// worktree leaves in its .git file.
func gitDir(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", errors.New("not a git repository")
	}
	if info.IsDir() {
		return dotGit, nil
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", errors.New("not a git repository")
	}
	m := gitdirLine.FindSubmatch(data)
	if m == nil {
		return "", errors.New("not a git repository")
	}
	return resolve(root, strings.TrimSpace(string(m[1]))), nil
}

func resolve(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// configText reads the repository's config. Worktrees keep remotes in the
// shared config that their "commondir" file points at.
func configText(dir string) (string, error) {
	if data, err := os.ReadFile(filepath.Join(dir, "config")); err == nil {
		return string(data), nil
	}
	common, err := os.ReadFile(filepath.Join(dir, "commondir"))
	if err != nil {
		return "", errors.New("could not read git config")
	}
	data, err := os.ReadFile(filepath.Join(resolve(dir, strings.TrimSpace(string(common))), "config"))
	if err != nil {
		return "", errors.New("could not read git config")
	}
	return string(data), nil
}

var (
	sectionHeader = regexp.MustCompile(`^\[(.+)\]$`)
	remoteSection = regexp.MustCompile(`^remote\s+"`)
	originSection = regexp.MustCompile(`^remote\s+"origin"$`)
	urlLine       = regexp.MustCompile(`^url\s*=\s*(.+)$`)
)

// ParseRemoteURL returns a remote URL from git config text: origin's when it
// has one, otherwise the first remote's, otherwise "".
func ParseRemoteURL(config string) string {
	section, first := "", ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		if !remoteSection.MatchString(section) {
			continue
		}
		m := urlLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		url := strings.TrimSpace(m[1])
		if originSection.MatchString(section) {
			return url
		}
		if first == "" {
			first = url
		}
	}
	return first
}

var (
	scpLike  = regexp.MustCompile(`^[\w.-]+@([A-Za-z0-9.-]+):(.+?)(\.git)?$`)
	sshURL   = regexp.MustCompile(`^ssh://[\w.-]+@([A-Za-z0-9.-]+)(?::\d+)?/(.+?)(\.git)?$`)
	httpURL  = regexp.MustCompile(`^(https?)://([^/]+)/(.+?)(\.git)?$`)
	safeHost = regexp.MustCompile(`^[A-Za-z0-9.\-:]+$`)
)

// WebURL turns a git remote (scp-style, ssh:// or http(s)://) into the
// repository's web page, or reports false when it can't. Credentials in an
// http(s) remote are never passed on.
func WebURL(remote string) (string, bool) {
	if m := scpLike.FindStringSubmatch(remote); m != nil {
		return "https://" + m[1] + "/" + strings.TrimSuffix(m[2], ".git"), true
	}
	if m := sshURL.FindStringSubmatch(remote); m != nil {
		return "https://" + m[1] + "/" + strings.TrimSuffix(m[2], ".git"), true
	}
	if m := httpURL.FindStringSubmatch(remote); m != nil {
		if !safeHost.MatchString(m[2]) {
			return "", false
		}
		return m[1] + "://" + m[2] + "/" + strings.TrimSuffix(m[3], ".git"), true
	}
	return "", false
}

// RemoteWebURL returns the web page of the repository at root's remote.
func RemoteWebURL(root string) (string, error) {
	dir, err := gitDir(root)
	if err != nil {
		return "", err
	}
	config, err := configText(dir)
	if err != nil {
		return "", err
	}
	remote := ParseRemoteURL(config)
	if remote == "" {
		return "", errors.New("no git remote found")
	}
	web, ok := WebURL(remote)
	if !ok {
		return "", fmt.Errorf("unrecognized remote URL: %s", remote)
	}
	return web, nil
}
