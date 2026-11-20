<!-- Generated from the setting declarations by "make docs". Edit the declarations, not this file. -->

# Configuration

gt reads its settings from a [TOML](https://toml.io) file. Every setting is optional: leave it out and gt uses the default.

## Where the file lives

gt uses the first of these that applies:

1. `$GT_CONFIG`, if it is set.
2. `$XDG_CONFIG_HOME/gt/config.toml`, if `XDG_CONFIG_HOME` is set.
3. `~/.config/gt/config.toml`.

`gt --config-path` prints the path gt is using. `gt --default-config` prints a complete file with every setting at its default, commented out — a good place to start:

```sh
mkdir -p ~/.config/gt
gt --default-config > ~/.config/gt/config.toml
```

## When something is wrong

gt checks the file every time it starts. If a key is misspelled, a value has the wrong type, or a choice isn't one of the allowed values, gt lists every problem by its setting name and doesn't open until they are fixed.

## Value types

| Type | How to write it | Example |
|---|---|---|
| true/false | `true` or `false` | `enabled = false` |
| whole number | digits, no quotes | `days = 7` |
| text | in double quotes | `command = "lazygit"` |
| path | in double quotes; a leading `~/` means your home directory | `folder = "~/scratch"` |
| choice | one of the listed values, in double quotes | `theme = "dark"` |

## General settings

These go at the top of the file, before any `[section]`.

| Setting | Type | Default | Description |
|---|---|---|---|
| `theme` | choice | `"auto"` | Colour scheme. One of `"auto"`: follow the terminal's background colour; `"dark"`: colours for a dark background; `"light"`: colours for a light background. |
| `terminal` | choice | `"auto"` | Where actions like Open in Terminal open a terminal. One of `"auto"`: a new tmux window inside tmux, otherwise a new window of the terminal gt runs in; `"tmux"`: a new tmux window; `"terminal"`: a new Terminal.app window; `"iterm2"`: a new iTerm2 window; `"kitty"`: a new kitty window; `"ghostty"`: a new Ghostty window; `"wezterm"`: a new WezTerm window. |

## Plugin settings

Each plugin's settings go in its own `[plugins.<name>]` section. Every plugin has `enabled`.

### Hello World

Minimal seed plugin, used as a template for new plugins.

```toml
[plugins.hello-world]
```

| Setting | Type | Default | Description |
|---|---|---|---|
| `enabled` | true/false | `true` | Show this plugin's commands in the search. |

### Next Task

Ask Claude what to work on next, based on your calendar, Jira board, Slack and Gmail.

```toml
[plugins.next-task]
```

| Setting | Type | Default | Description |
|---|---|---|---|
| `enabled` | true/false | `true` | Show this plugin's commands in the search. |
| `model` | choice | `"claude-opus-5-5"` | Model that picks the task. One of `"claude-opus-5-5"`: Opus 5.5: picked better tasks in side-by-side runs; `"claude-sonnet-5-5"`: Sonnet 5.5: faster and cheaper, but cut corners; `"claude-fable-5-1"`: Fable 5.1. |
| `effort` | choice | `"medium"` | Higher effort reads more carefully but takes longer and costs more. One of `"low"`; `"medium"`; `"high"`; `"xhigh"`: extra high. |
| `reuse-minutes` | whole number | `30` | Opening Next Task within this many minutes of the last answer shows it instead of asking again. 0 asks every time; ctrl+r always asks. |
| `meeting-window-minutes` | whole number | `60` | Your next meeting is shown at the top when it starts within this many minutes, and stays until it ends. |
| `save-for-later` | true/false | `true` | Save tasks and notes to come back to; Claude checks where each stands. When false the actions and section are hidden and the list isn't sent to Claude, but saved items are kept. |
| `resolved-reactions` | text | `"white_check_mark, heavy_check_mark"` | Slack reactions (comma-separated, colons optional) that mark a request as done. Inserted wherever the workflow says {{resolvedReactions}}. |
| `claude-path` | path | `"~/.local/bin/claude"` | The claude binary: a path, or a bare name to look up on PATH. |

### Scratch

Create disposable scratch folders and clean up the ones you have stopped touching.

```toml
[plugins.scratch]
```

| Setting | Type | Default | Description |
|---|---|---|---|
| `enabled` | true/false | `true` | Show this plugin's commands in the search. |
| `root` | path | `"~/scratch"` | Folder that holds the scratch folders. Created on first use; a relative path is taken from your home folder. |
| `editor` | text | `"Visual Studio Code"` | App that opens scratch folders: an app name or a path to a .app. |
| `open-in-editor` | true/false | `true` | Open a new scratch folder in the editor. When false, its path is copied to the clipboard instead. |
| `stale-days` | whole number | `7` | A folder counts as stale when nothing anywhere inside it has changed for this many days. Values below 1 mean 7. |
| `name-style` | choice | `"words"` | Shape of generated folder names. One of `"words"`: brave-otter; `"words-suffix"`: brave-otter-a1b2; `"date-words"`: 2026-08-11-brave-otter; `"hex"`: scratch-8f3a2c4d. |
| `git-client` | text | `"lazygit"` | Command the git client action runs in a terminal at the folder, e.g. lazygit, gitui or tig. |
| `delete-mode` | choice | `"trash"` | How folders are removed. One of `"trash"`: move to the Trash, recoverable from Finder; `"permanent"`: delete permanently, not recoverable. |

### Visual Studio Code

Search and open projects from the VS Code Project Manager extension.

```toml
[plugins.vscode-projects]
```

| Setting | Type | Default | Description |
|---|---|---|---|
| `enabled` | true/false | `true` | Show this plugin's commands in the search. |
| `vscode-app` | path | `"/Applications/Visual Studio Code.app"` | The VS Code app projects open in. It also decides where Project Manager's projects.json is looked for. |
| `projects-location` | path | `""` | Folder containing projects.json (and any projects_cache_*.json). Empty means Project Manager's own storage folder for vscode-app. |
| `group-by-tag` | true/false | `true` | Group projects under their tags instead of one list. |
| `hide-untagged` | true/false | `false` | Hide projects that have no tags. |
| `hide-disabled` | true/false | `false` | Hide projects whose enabled is false in projects.json. |
| `git-client` | text | `"lazygit"` | Command the git client action runs in a terminal at the project, e.g. lazygit, gitui or tig. |
