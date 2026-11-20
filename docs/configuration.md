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
