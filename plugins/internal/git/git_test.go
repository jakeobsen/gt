package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRemoteURLPrefersOrigin(t *testing.T) {
	config := `[core]
	bare = false
[remote "upstream"]
	url = git@github.com:up/stream.git
[remote "origin"]
	url = https://github.com/me/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	if got := ParseRemoteURL(config); got != "https://github.com/me/repo.git" {
		t.Fatalf("got %q", got)
	}
	if got := ParseRemoteURL("[remote \"upstream\"]\n\turl = a@b.c:d/e\n"); got != "a@b.c:d/e" {
		t.Fatalf("without origin, want the first remote; got %q", got)
	}
	if got := ParseRemoteURL("[core]\n\turl = nope\n"); got != "" {
		t.Fatalf("url outside a remote section: got %q", got)
	}
}

func TestWebURL(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:me/repo.git":                "https://github.com/me/repo",
		"git@gitlab.example.com:group/sub/repo":     "https://gitlab.example.com/group/sub/repo",
		"ssh://git@github.com/me/repo.git":          "https://github.com/me/repo",
		"ssh://git@host.example.com:2222/me/repo":   "https://host.example.com/me/repo",
		"https://github.com/me/repo.git":            "https://github.com/me/repo",
		"http://git.internal:8080/team/project.git": "http://git.internal:8080/team/project",
	} {
		if got, ok := WebURL(remote); !ok || got != want {
			t.Errorf("WebURL(%q) = %q, %v; want %q", remote, got, ok, want)
		}
	}
	for _, remote := range []string{"https://user:token@github.com/me/repo.git", "/local/path/repo", "file:///x"} {
		if got, ok := WebURL(remote); ok {
			t.Errorf("WebURL(%q) = %q; want no URL", remote, got)
		}
	}
}

func TestRemoteWebURLFollowsWorktreePointers(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	common := filepath.Join(main, ".git")
	wtGit := filepath.Join(common, "worktrees", "feature")
	for _, d := range []string{wtGit, filepath.Join(base, "feature")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(common, "config"), "[remote \"origin\"]\n\turl = git@github.com:me/repo.git\n")
	write(filepath.Join(wtGit, "commondir"), "../..\n")
	write(filepath.Join(base, "feature", ".git"), "gitdir: "+wtGit+"\n")

	for _, root := range []string{main, filepath.Join(base, "feature")} {
		if !IsRepo(root) {
			t.Fatalf("IsRepo(%s) = false", root)
		}
		got, err := RemoteWebURL(root)
		if err != nil || got != "https://github.com/me/repo" {
			t.Fatalf("RemoteWebURL(%s) = %q, %v", root, got, err)
		}
	}
}

func TestRemoteWebURLErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := RemoteWebURL(dir); err == nil || err.Error() != "not a git repository" {
		t.Fatalf("plain folder: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoteWebURL(dir); err == nil || err.Error() != "no git remote found" {
		t.Fatalf("no remote: %v", err)
	}
}
