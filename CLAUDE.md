# CLAUDE.md

Guidance for AI coding agents working in this repository. User-facing docs are in README.md.

## What this is

goto is a keyboard launcher for the terminal, written in Go. `gt` opens a full-screen UI that searches every command from every compiled-in plugin. There are no subcommands — the UI is the product.

## Architecture

- **Plugins are compiled in.** Each plugin is a Go package under `plugins/` exporting `Plugin() kit.Plugin`, registered with one line in `cmd/gt/plugins.go`. There is no runtime plugin loading.
- **`kit/` mirrors `@raycast/api`.** Plugins are ports of Raycast extensions; translate an extension onto the kit rather than redesigning it. When a plugin needs something the kit lacks, add it to the kit in the same shape as the Raycast API, then use it.
- **Plugin code never runs on the UI loop.** `Command.Run` runs inside a `tea.Cmd`; `kit.Context` methods turn calls into messages via `Program.Send` (the `bridge` in `internal/app`). Plugin panics are recovered into errors (`safely`).
- **Settings are declared, never ad hoc.** gt's own settings are `config.Core`; a plugin's are `kit.Plugin.Settings` (plus the automatic `enabled`). `internal/config` checks the user's TOML against the declarations and renders them as `docs/configuration.md` and `gt --default-config`. After changing a declaration run `make docs`; `TestConfigDocsUpToDate` fails until you do.
- **`tea.Sequence(busy, run)`** makes the "Running…" status land before the result. Tests unwrap Bubble Tea's unexported `sequenceMsg` by reflection (`asCmds` in `app_test.go`).

## Gotchas

- **No package may be named `goto`** — it is a Go keyword. Packages are `kit`, `app`, and so on.
- **Charm v2 lives at `charm.land/...`** (bubbletea, bubbles, lipgloss), not `github.com/charmbracelet/...`. `View()` returns `tea.View`; the alternate screen is `View.AltScreen`, not a program option.
- **The fuzzy matcher (`sahilm/fuzzy`) returns byte offsets.** Convert them to rune positions before `lipgloss.StyleRunes` (`titleRunes`).
- **Rendering must fill the window exactly** (tests check width and height). Measure with `ansi.StringWidth`, never `len` — `›`, `↵` and `…` are multi-byte.

## Releases

A pushed `v*` tag runs `.github/workflows/release.yml`: it loads the certificate into a temporary keychain, then GoReleaser (`.goreleaser.yaml`) builds a universal macOS binary, runs `scripts/sign-and-notarize.sh` on it as a post-hook (`codesign` + `notarytool` via a keychain profile), publishes the GitHub release and pushes the `gt` cask to `jakeobsen/homebrew-tap` over SSH with a deploy key. `docs/releasing.md` lists the secrets. The script must sign only with the identity found in `KEYCHAIN_PATH` (by SHA-1) — `codesign --keychain` alone falls back to other keychains. Secret values must never reach logs or third-party code: only the keychain step gets the `APPLE_*` secrets, nothing echoes them, and the logs are public. Validate config changes with `goreleaser check`; dry-run with `HOMEBREW_TAP_DEPLOY_KEY=unused goreleaser release --snapshot --clean`. Third-party actions are pinned to commit SHAs (version in a trailing comment) because the release job holds signing secrets — keep them pinned when updating.

## Commands

`make build` · `make run` · `make test` (race detector) · `make lint` (gofmt + vet) · `make docs` · `make install`

## Conventions

- Go only: a launcher must start instantly and ship as one binary.
- Docs describe what exists — no roadmaps, status tables or TODO lists in committed files.
- Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`), scoped with the plugin name for plugin changes, e.g. `feat(scratch): ...`.
- `AGPL-3.0-or-later` everywhere. Code derived from MIT-licensed upstream keeps the upstream notice in a `LICENSE-upstream` next to it. New dependencies must be AGPL-compatible.
- Plugin and command names are kebab-case (enforced by `kit.Validate`).
