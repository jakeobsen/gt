# Scratch

Disposable working folders, on demand. **Create Scratch** makes a randomly named folder under `~/scratch` and opens it in your editor; **Scratch Folders** browses what you have and cleans up whatever nothing has touched for a week.

Ported from the Raycast `scratch` extension. Both write the same `.scratch.json` marker, so folders made by either are recognised.

## Commands

### Create Scratch

Creates `~/scratch/<random-name>` (e.g. `curious-walrus`), writes a small `.scratch.json` marker into it, and opens the folder in your editor. goto closes and prints the folder's name.

It first asks for an optional **label**, which is slugified and prefixed to the random name — `redis repro` gives `redis-repro-brave-otter`.

If the editor can't be opened, the folder is still created and its path is copied to the clipboard.

### Scratch Folders

Lists every folder directly under the scratch root, in two sections:

- **Active** — changed within the threshold, newest first.
- **Stale** — nothing inside changed for at least `stale-days` (default 7), oldest first.

Staleness is decided by the newest modification time found **anywhere inside** the folder, not the folder's own — editing a file three levels down keeps the whole folder alive.

Tags on the right: the folder's age (red when stale), `large tree` on folders too big to scan, and `no marker` on folders that weren't made by Create Scratch.

| Key | Action |
|---|---|
| `enter` | Open in your editor |
| `ctrl+t` | Open in a terminal |
| `ctrl+g` | Open your git client (`lazygit` by default) in a terminal |
| `ctrl+w` | Open the folder's git remote in the browser |
| `ctrl+f` | Show in Finder |
| `ctrl+o` | Open With… |
| `ctrl+y` | Copy the folder's path |
| `ctrl+r` | Refresh |
| `ctrl+x` | Remove this folder |
| `ctrl+s` | Clean up all stale folders |
| `ctrl+k` | All actions, including Copy Name and Create Scratch Folder |

Terminals can't send ⌘, so the Raycast shortcuts changed: ⌘T, ⌘G, ⌘F and ⌘O became ctrl+t, ctrl+g, ctrl+f and ctrl+o; ⌘⇧G (remote) became ctrl+w for *web*; ⌘⇧. (copy path) became ctrl+y for *yank*; ⌘R became ctrl+r; ⌃X stayed; ⌘⇧X (clean up) became ctrl+s for *stale*. ⌘N and ⌘. have no shortcut — ctrl+n moves down the list, as in Raycast — and are in the ctrl+k menu.

Where terminals open follows gt's `terminal` setting: a new tmux window inside tmux, otherwise a new window of the terminal gt runs in.

## Settings

In `[plugins.scratch]`; see [docs/configuration.md](../../docs/configuration.md#scratch).

| Setting | Default | What it does |
|---|---|---|
| `root` | `~/scratch` | Folder that holds the scratch folders. Created on first use. |
| `editor` | `Visual Studio Code` | App that opens folders: a name or a path to a `.app`. |
| `open-in-editor` | `true` | When false, Create Scratch copies the path instead of opening the folder. |
| `stale-days` | `7` | Age threshold for the Stale section. Values below 1 mean 7. |
| `name-style` | `words` | Also `words-suffix`, `date-words`, `hex`. |
| `git-client` | `lazygit` | Command the git client action runs, e.g. `gitui` or `tig`. |
| `delete-mode` | `trash` | Or `permanent` (not recoverable). |

## Safety rails

Cleanup is deliberately conservative, since the failure mode is deleted work:

- Only **direct child directories** of the scratch root are ever candidates — loose files and dotfolders are ignored, and every delete re-checks that the path is a direct child of the root.
- Symlinked directories are never followed while scanning, so a symlink can't drag the scan (or a delete) outside the scratch tree.
- Folders too large to scan fully (more than 20,000 entries) are tagged `large tree` and never counted as stale.
- A `stale-days` below 1 means 7, so a bad setting can't mark everything stale.
- Nothing is deleted without a confirmation, and the default is the Trash.
