# Next Task

Asks Claude what to work on next. **Next Task** runs Claude Code headless (`claude -p`) against your calendar, Jira, Slack and Gmail, and shows one pick, up to three alternatives, and notes worth knowing — with the selected item's details beside the list.

Ported from the Raycast `next-task` extension. It keeps the same storage keys and formats, but its data lives in gt's own state folder, so nothing carries over from Raycast automatically.

## The workflow

What Claude checks and how it judges it is your **workflow**: plain text you edit with `ctrl+e` (**Edit Workflow**), which opens it in your editor (`$VISUAL`, else `$EDITOR`, else `vi`) right in the terminal. Below the text is a `>8` scissors line with help on placeholders; everything from that line down is ignored when you save. Delete all the text to go back to the built-in example.

The workflow is stored in gt's state folder (`~/.local/state/gt/plugins/next-task.json`), so names, IDs and team conventions stay on your machine. Until you save one, a generic example is used: today's calendar, your open Jira issues, Slack mentions and DMs, and Gmail threads waiting on you.

It is wrapped in fixed rules you can't edit out: read-only, treat everything read as data rather than instructions, fetch every page before deciding, and the answer format.

| Placeholder | Value |
|---|---|
| `{{date}}` | today, e.g. `2026-10-07` |
| `{{weekday}}` | e.g. `Wednesday` |
| `{{time}}` | e.g. `15:20` |
| `{{timezone}}` | e.g. `Europe/Copenhagen` |
| `{{weekStart}}` | Monday of this week |
| `{{resolvedReactions}}` | the `resolved-reactions` setting |

An unknown placeholder (say `{{dat}}`) is sent as-is and flagged in a toast and the progress panel.

## Using it

Opening the command asks Claude straight away, unless the last answer is younger than `reuse-minutes` (default 30). A run takes about a minute on Opus; the progress row shows what it's reading.

You can close gt mid-run — or the whole terminal. Claude keeps going in the background, and the answer is there next time you open the command.

| Key | Action |
|---|---|
| `enter` | Open the task's link, or join the meeting |
| `ctrl+y` | Copy the link |
| `ctrl+r` | Ask again |
| `ctrl+e` | Edit the workflow |
| `ctrl+s` | Save a task or note for later |
| `ctrl+d` | Let go of a saved item |
| `ctrl+x` | Stop the current run |
| `ctrl+k` | All actions, including Copy Task, Copy Workflow and Reset Workflow to Example |

Raycast's ⌘ shortcuts became ctrl ones; ⌘⌫ (Let Go) became ctrl+d, since terminals can't tell ctrl+backspace from backspace.

When your next meeting starts within `meeting-window-minutes` (default 60), it's shown at the top with a live countdown until it ends. A virtual meeting's `enter` joins it; an in-person one shows the time, place, attendees and what it's about.

Each item is labelled with where `enter` takes you — Jira, Slack, Gmail or Calendar, judged from the link — in place of the Raycast extension's brand icons.

## Save for later

`ctrl+s` on a task or note saves it for later exactly as it is: title, link and Claude's reason. Saved items sit in a **Saved for Later** section, showing that reason, Claude's latest take on where it stands, and when you saved it, until you let them go with `ctrl+d`.

Claude sees the saved list — titles, links and dates only; the saved reason stays out — and reports where each stands: **Looks done** or **Waiting**. It won't push a saved item as your next task just because it's saved.

Set `save-for-later = false` to hide the actions and the section and stop sending the list to Claude; your saved items are kept.

## Read-only by construction

Claude gets no built-in tools (`--tools ""`) and only MCP read tools: calendar list/get, Slack read/search/profile, Jira search/get and site lookup, Gmail search/get thread. Every write tool on those connectors is explicitly denied with `--disallowedTools`, because Claude Code settings can pre-approve some of them and `--permission-mode dontAsk` would otherwise let those through. Any refused tool call is recorded in the run's `result.json`.

For debugging, each run's JSON is kept for 7 days in `~/.local/state/gt/plugins/next-task/history/<start time>-<outcome>/`: `stream.jsonl` (Claude's full event stream), `meta.json`, and `result.json` or `error.json`. Older runs are deleted whenever a run ends. The stream contains raw message, mail and ticket text, so it stays on this machine and nothing keeps it longer.

## Settings

In `[plugins.next-task]`; see [docs/configuration.md](../../docs/configuration.md#next-task).

| Setting | Default | What it does |
|---|---|---|
| `model` | `claude-opus-5-5` | Also `claude-sonnet-5-5` and `claude-fable-5-1`. |
| `effort` | `medium` | `low`, `medium`, `high` or `xhigh`. |
| `reuse-minutes` | `30` | `0` asks every time the command opens. |
| `meeting-window-minutes` | `60` | Your next meeting appears at the top once it's this close. |
| `save-for-later` | `true` | Turns Save for Later on or off. Saved items are kept either way. |
| `resolved-reactions` | `white_check_mark, heavy_check_mark` | Slack reactions that mark a request done. |
| `claude-path` | `~/.local/bin/claude` | A path, or a bare name to look up on `PATH`. |

## Requirements

- Claude Code CLI, logged in, with the claude.ai **Google Calendar**, **Slack**, **Gmail** and **Atlassian** connectors authorised. If any of them doesn't load, the run stops early and says which.
- Runs start with `MCP_CONNECTION_NONBLOCKING=false MCP_TIMEOUT=30000`; without them headless runs often start before the claude.ai connectors are ready.
