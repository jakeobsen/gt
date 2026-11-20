# Visual Studio Code projects

**Search Project Manager** lists the projects saved by the [Project Manager](https://marketplace.visualstudio.com/items?itemName=alefragnani.project-manager) VS Code extension and opens them. It reads `projects.json` — from Project Manager's own storage folder, or any folder you point at — and merges `projects_cache_{git,any,vscode}.json` when present.

Ported from the Raycast `better-visual-studio-code-project-manager` extension, a fork of Raycast's `visual-studio-code-project-manager`; see [LICENSE-upstream](LICENSE-upstream).

Projects are ranked by how often and how recently you open them; ones you have never opened follow, by name. With `group-by-tag` on, each project is listed under every tag it has (untagged ones under `[no tags]`). **Tab** picks a tag to show only its projects.

| Key | Action |
|---|---|
| `enter` | Open in VS Code (or, for `vscode-remote://` projects, open the remote) |
| `ctrl+t` | Open in a terminal |
| `ctrl+g` | Open your git client (`lazygit` by default) in a terminal |
| `ctrl+w` | Open the project's git remote in the browser |
| `ctrl+f` | Show in Finder |
| `ctrl+o` | Open With… |
| `ctrl+y` | Copy the project's path |
| `ctrl+x` | Move the project folder to the Trash (asks first) |
| `tab` | Filter by tag |
| `ctrl+k` | All actions, including Copy Name and Reset Project Ranking |

Shortcuts follow the scratch plugin's mapping of Raycast's ⌘ shortcuts. Unlike the Raycast version, Move to Trash asks for confirmation.

## Settings

In `[plugins.vscode-projects]`; see [docs/configuration.md](../../docs/configuration.md#visual-studio-code).

| Setting | Default | What it does |
|---|---|---|
| `vscode-app` | `/Applications/Visual Studio Code.app` | App projects open in; also where Project Manager's files are looked for. |
| `projects-location` | *(empty)* | Folder with `projects.json`. Empty means Project Manager's own storage for `vscode-app`. |
| `group-by-tag` | `true` | Group projects under their tags. |
| `hide-untagged` | `false` | Hide projects without tags. |
| `hide-disabled` | `false` | Hide projects with `"enabled": false`. |
| `git-client` | `lazygit` | Command the git client action runs. |

Rankings are kept in gt's state folder (`~/.local/state/gt`).
