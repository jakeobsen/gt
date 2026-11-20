# goto

A keyboard launcher for the terminal. Run `gt`, type to search every command from every plugin, and press Enter to run one — like Raycast, but in a terminal window.

```
 goto  scr
────────────────────────────────────────────────────────
› Scratch Folders         Scratch
  Create Scratch          Scratch
  Search Project Manager  Better Visual Studio Code
────────────────────────────────────────────────────────
                                ↵ Run Command   esc Clear
```

goto's plugins are ports of Raycast extensions.

## Install

With [Homebrew](https://brew.sh) on macOS:

```bash
brew install jakeobsen/tap/gt
```

The binary runs natively on Apple silicon and Intel, and is signed and notarized by Apple.

From source (needs Go 1.26+):

```bash
make install                       # builds gt and copies it to $(go env GOPATH)/bin
make install BINDIR=~/.local/bin   # or anywhere else on your PATH
```

## Use

| Key | Does |
|-----|------|
| type | Filter the list (fuzzy: `scr` finds "Scratch Folders") |
| `↑` `↓` / `ctrl+p` `ctrl+n` | Move the selection |
| `pgup` `pgdown` | Move a page at a time |
| `enter` | Run the selected item's main action |
| `ctrl+k` | All actions for the selected item, with their shortcuts |
| `tab` | Open the list's filter (where there is one) |
| `esc` | Clear the search; with an empty search, go back (or quit) |
| `ctrl+c` | Quit |

## Configuration

gt reads its settings from `~/.config/gt/config.toml` (or `$GT_CONFIG`). The file is optional; every setting has a default. To start one with every setting listed at its default:

```bash
mkdir -p ~/.config/gt
gt --default-config > ~/.config/gt/config.toml
```

```toml
theme = "dark"            # or "light", or "auto" to follow the terminal

[plugins.hello-world]
enabled = false           # hide this plugin's commands
```

gt checks the file each time it starts and lists every misspelled key or wrong value before opening. The full reference — every setting of gt and of each plugin — is in [docs/configuration.md](docs/configuration.md).

## Make targets

| Target | What it does |
|--------|--------------|
| `make help` | List targets (default). |
| `make build` | Build `bin/gt`. |
| `make run` | Build and open goto. |
| `make install` | Build and copy `gt` to `BINDIR`. |
| `make test` | Run all tests with the race detector. |
| `make lint` | `gofmt` check and `go vet`. |
| `make docs` | Regenerate `docs/configuration.md` from the setting declarations. |
| `make clean` | Remove build output. |

Releases are built, signed, notarized and published by GitHub Actions when a `v*` tag is pushed; see [docs/releasing.md](docs/releasing.md).

## Writing a plugin

A plugin is a Go package under `plugins/` that returns a `kit.Plugin`. The smallest one is [`plugins/hello`](plugins/hello/hello.go):

```go
func Plugin() kit.Plugin {
	return kit.Plugin{
		Name:  "hello-world",
		Title: "Hello World",
		Commands: []kit.Command{{
			Name:  "say-hello",
			Title: "Say Hello",
			Run: func(ctx kit.Context) error {
				ctx.HUD("Hello from goto!") // close goto, print this
				return nil
			},
		}},
	}
}
```

Then add it to [`cmd/gt/plugins.go`](cmd/gt/plugins.go) and `make run`. gt checks every plugin when it starts (`kit.Validate`) and refuses to open if one is misconfigured, with a message saying what to fix.

A plugin with settings declares them in `Plugin.Settings`, and its commands read them with `ctx.Settings()`:

```go
Settings: []kit.Setting{
	{Key: "stale-days", Kind: kit.Int, Default: 7, Description: "Days without changes before a folder counts as stale."},
},
// in a command:
days := ctx.Settings().Int("stale-days")
```

Run `make docs` afterwards: the settings reference is generated from these declarations, and `make test` fails while it is out of date.

The kit ([`kit/`](kit/)) plays the part `@raycast/api` played: it is what a plugin uses to talk to the UI.

## Plugins

| Plugin | Commands | Ported from |
|---|---|---|
| [`scratch`](plugins/scratch/) | Create Scratch, Scratch Folders | Raycast `scratch` |
| [`vscode-projects`](plugins/vscode/) | Search Project Manager | Raycast `better-visual-studio-code-project-manager` |
| [`next-task`](plugins/nexttask/) | Next Task | Raycast `next-task` |
| [`hello-world`](plugins/hello/) | Say Hello | Raycast `hello-world` |

Plugin actions are listed in each plugin's README. Everywhere, `ctrl+k` opens the actions menu for the selected item, and `tab` opens a list's filter when it has one. Actions that open a terminal follow the `terminal` setting: a new tmux window inside tmux, otherwise a new window of the terminal gt runs in.

## Layout

```
.
├── cmd/gt/          the gt binary; plugins.go lists the compiled-in plugins
├── .github/         CI and release workflows
├── docs/            settings reference (generated) and release guide
├── kit/             the plugin kit (what plugins import)
├── internal/app/    the terminal UI (Bubble Tea)
├── internal/config/ the settings file: loading, checking, docs
└── plugins/         one package per plugin
```

## License

Copyright (C) 2026 Morten Jakobsen.

Licensed under the GNU Affero General Public License, version 3 or (at your option) any later version (`AGPL-3.0-or-later`). See [LICENSE](LICENSE).
