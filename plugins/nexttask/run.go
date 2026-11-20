package nexttask

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// A run is a detached `claude -p` process writing stream-json to a file in
// the plugin's data folder, so it keeps going when gt closes and the screen
// can pick it up again next time. When a run ends, its JSON (stream, meta and
// the outcome) moves to history/ for debugging and is deleted after
// historyDays. The stream holds raw Slack, mail and Jira content, so nothing
// keeps it longer than that.

type storedResult struct {
	Answer answer `json:"answer"`
	// Model did the work, read from the run's usage data.
	Model      string  `json:"model"`
	DurationMs int64   `json:"durationMs"`
	CostUsd    float64 `json:"costUsd"`
	// Denied lists tool calls the permission layer refused — non-empty means
	// the prompt reached for a write.
	Denied []string `json:"denied"`
	// Unreadable lists tool results too large to reach the model, so the
	// answer may have missed something.
	Unreadable []string `json:"unreadable,omitempty"`
	Workflow   string   `json:"workflow"`
	FinishedAt int64    `json:"finishedAt"` // Unix milliseconds
}

type storedError struct {
	Message string `json:"message"`
	At      int64  `json:"at"`
}

type runMeta struct {
	Pid                 int      `json:"pid"`
	StartedAt           int64    `json:"startedAt"`
	Workflow            string   `json:"workflow"`
	UnknownPlaceholders []string `json:"unknownPlaceholders"`
}

type progress struct {
	startedAt time.Time
	label     string
	steps     int
	workflow  string
	unknown   []string
}

const (
	// maxRun is how long a run may go on before it counts as hung.
	maxRun = 10 * time.Minute
	// historyDays is how long finished runs are kept.
	historyDays = 7
)

// runner owns the run files in dir. Its process hooks are replaced in tests.
type runner struct {
	dir   string
	now   func() time.Time
	alive func(pid int) bool
	kill  func(pid int)
}

func newRunner(dir string) *runner {
	return &runner{
		dir: dir,
		now: time.Now,
		alive: func(pid int) bool {
			err := syscall.Kill(pid, 0)
			// EPERM: the pid exists but belongs to someone else — still alive.
			return err == nil || errors.Is(err, syscall.EPERM)
		},
		// The run has its own process group (Setsid), so this stops claude
		// and any MCP helpers it spawned.
		kill: func(pid int) { _ = syscall.Kill(-pid, syscall.SIGTERM) },
	}
}

func (r *runner) runDir() string     { return filepath.Join(r.dir, "run") }
func (r *runner) historyDir() string { return filepath.Join(r.dir, "history") }
func (r *runner) metaPath() string   { return filepath.Join(r.runDir(), "meta.json") }
func (r *runner) streamPath() string { return filepath.Join(r.runDir(), "stream.jsonl") }
func (r *runner) stderrPath() string { return filepath.Join(r.runDir(), "stderr.log") }
func (r *runner) resultPath() string { return filepath.Join(r.dir, "last-result.json") }
func (r *runner) errorPath() string  { return filepath.Join(r.dir, "last-error.json") }

func readJSON[T any](path string) *T {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v T
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	return &v
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (r *runner) loadResult() *storedResult { return readJSON[storedResult](r.resultPath()) }
func (r *runner) loadError() *storedError   { return readJSON[storedError](r.errorPath()) }
func (r *runner) readMeta() *runMeta        { return readJSON[runMeta](r.metaPath()) }

// running reports whether a run's process is still going.
func (r *runner) running() bool {
	m := r.readMeta()
	return m != nil && r.alive(m.Pid)
}

// claudeEnv makes sure the run finds its login and its connectors, whatever
// gt was started from (a terminal, or a hotkey with a bare environment).
func claudeEnv() []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	home, _ := os.UserHomeDir()
	if env["HOME"] == "" {
		env["HOME"] = home
	}
	if u, err := user.Current(); err == nil {
		// claude looks up its keychain login by $USER.
		if env["USER"] == "" {
			env["USER"] = u.Username
		}
		if env["LOGNAME"] == "" {
			env["LOGNAME"] = u.Username
		}
	}
	if env["LANG"] == "" {
		env["LANG"] = "en_US.UTF-8"
	}
	env["PATH"] = strings.Join([]string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", env["PATH"], "/usr/bin:/bin:/usr/sbin:/sbin"}, ":")
	// Without these, -p mode only waits briefly for MCP servers and the
	// claude.ai connectors are often missing from the run.
	env["MCP_CONNECTION_NONBLOCKING"] = "false"
	env["MCP_TIMEOUT"] = "30000"
	// Past this, Claude Code saves a tool result to a file instead of handing
	// it to the model, which can't read it back without file tools.
	env["MAX_MCP_OUTPUT_TOKENS"] = "80000"

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// start begins a run unless one is already going.
func (r *runner) start(p prefs, wf workflow, saved []savedItem) error {
	if m := r.readMeta(); m != nil && r.alive(m.Pid) {
		return nil
	}
	if info, err := os.Stat(p.claudePath); err != nil || info.IsDir() {
		return fmt.Errorf("no claude binary at %s; set claude-path in [plugins.next-task]", p.claudePath)
	}

	// A run folder with no live process is one nobody watched to the end;
	// keep it for debugging.
	r.archive("abandoned", "", nil)
	_ = os.Remove(r.errorPath())
	if err := os.MkdirAll(r.runDir(), 0o700); err != nil {
		return err
	}
	now := r.now()
	out, err := os.OpenFile(r.streamPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	errf, err := os.OpenFile(r.stderrPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer errf.Close()

	home, _ := os.UserHomeDir()
	cmd := exec.Command(p.claudePath, buildArgs(p, now, wf, saved)...)
	cmd.Dir = home
	cmd.Env = claudeEnv()
	cmd.Stdout, cmd.Stderr = out, errf
	// Its own session: closing the terminal or quitting gt doesn't take the
	// run down, and stop can signal the whole process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start %s: %w", p.claudePath, err)
	}
	// Reap it if it ends while gt is still open; otherwise launchd does.
	go func() { _ = cmd.Wait() }()

	return writeJSON(r.metaPath(), runMeta{
		Pid: cmd.Process.Pid, StartedAt: now.UnixMilli(), Workflow: wf.source, UnknownPlaceholders: wf.unknown,
	})
}

// stop ends the current run without recording an error.
func (r *runner) stop() {
	if m := r.readMeta(); m != nil {
		r.kill(m.Pid)
	}
	r.archive("stopped", "", nil)
}

func (r *runner) fail(message string) {
	e := storedError{Message: message, At: r.now().UnixMilli()}
	_ = writeJSON(r.errorPath(), e)
	r.archive("error", "error.json", e)
}

// stamp is "2026-10-07_15-39-12" in local time: sortable and safe as a
// folder name.
func stamp(t time.Time) string { return t.Local().Format("2006-01-02_15-04-05") }

// archive moves the run folder into history as <start time>-<outcome>, then
// deletes history older than historyDays.
func (r *runner) archive(outcome, extraName string, extra any) {
	if _, err := os.Stat(r.runDir()); err != nil {
		return
	}
	if extraName != "" {
		_ = writeJSON(filepath.Join(r.runDir(), extraName), extra)
	}
	// History keeps the JSON only; an error's stderr lives on in its message.
	_ = os.Remove(r.stderrPath())
	started := r.now()
	if m := r.readMeta(); m != nil {
		started = time.UnixMilli(m.StartedAt)
	}
	if err := os.MkdirAll(r.historyDir(), 0o700); err != nil {
		return
	}
	dest := filepath.Join(r.historyDir(), stamp(started)+"-"+outcome)
	if _, err := os.Stat(dest); err == nil {
		dest += fmt.Sprintf("-%d", r.now().UnixMilli())
	}
	// A rename keeps any still-open file handle valid, so a run being
	// stopped can't fail the move.
	_ = os.Rename(r.runDir(), dest)
	r.prune()
}

func (r *runner) prune() {
	cutoff := r.now().Add(-historyDays * 24 * time.Hour)
	entries, err := os.ReadDir(r.historyDir())
	if err != nil {
		return
	}
	// Only direct child folders of history/ are ever candidates.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(r.historyDir(), e.Name())
		if info, err := os.Stat(path); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(path)
		}
	}
}

func lastChars(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	runes := []rune(strings.TrimSpace(string(data)))
	if len(runes) > n {
		runes = runes[len(runes)-n:]
	}
	return string(runes)
}

// --- following the stream -------------------------------------------------------

type resultEvent struct {
	Subtype          string  `json:"subtype"`
	IsError          bool    `json:"is_error"`
	Result           string  `json:"result"`
	StructuredOutput *answer `json:"structured_output"`
	DurationMs       int64   `json:"duration_ms"`
	TotalCostUsd     float64 `json:"total_cost_usd"`
	ModelUsage       map[string]struct {
		CostUSD float64 `json:"costUSD"`
	} `json:"modelUsage"`
	PermissionDenials []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
}

type streamEvent struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	McpServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// mainModel is the model that cost the most: Claude Code also bills a small
// background model.
func mainModel(usage map[string]struct {
	CostUSD float64 `json:"costUSD"`
}) string {
	names := make([]string, 0, len(usage))
	for n := range usage {
		names = append(names, n)
	}
	sort.Strings(names)
	best, bestCost := "unknown", -1.0
	for _, n := range names {
		if usage[n].CostUSD > bestCost {
			best, bestCost = n, usage[n].CostUSD
		}
	}
	return best
}

func toolName(name string) string {
	parts := strings.Split(name, "__")
	return parts[len(parts)-1]
}

// describeTool is what the run is doing, for the progress row.
func describeTool(name string) string {
	switch t := toolName(name); {
	case t == "list_events" || t == "get_event":
		return "Checking your calendar"
	case t == "searchJiraIssuesUsingJql" || t == "getJiraIssue":
		return "Reading Jira"
	case t == "slack_read_canvas":
		return "Reading a Slack canvas"
	case t == "slack_read_channel":
		return "Reading a Slack channel"
	case t == "slack_read_thread":
		return "Reading a Slack thread"
	case strings.HasPrefix(t, "slack_search"):
		return "Searching Slack"
	case t == "search_threads" || t == "get_thread":
		return "Checking Gmail"
	}
	return "Working"
}

// describeSource is what a tool reads, for the "couldn't read" note.
func describeSource(name string) string {
	switch t := toolName(name); {
	case t == "list_events" || t == "get_event":
		return "your calendar"
	case t == "searchJiraIssuesUsingJql":
		return "a Jira search"
	case t == "getJiraIssue":
		return "a Jira ticket"
	case t == "slack_read_canvas":
		return "a Slack canvas"
	case t == "slack_read_channel":
		return "a Slack channel"
	case t == "slack_read_thread":
		return "a Slack thread"
	case strings.HasPrefix(t, "slack_search"):
		return "a Slack search"
	case t == "search_threads":
		return "a Gmail search"
	case t == "get_thread":
		return "an email thread"
	case t != "":
		return t
	}
	return "a tool result"
}

// oversized is Claude Code's wording when a tool result is too big and gets
// saved to a file instead of reaching the model.
var oversized = regexp.MustCompile(`(?i)exceeds maximum allowed tokens`)

func toolResultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// watcher follows the current run's stream file. Each poll reads only what
// was appended since the last; when the run ends it records the answer (or
// the error) and poll returns nil.
type watcher struct {
	startedAt  int64
	offset     int64
	partial    []byte
	steps      int
	label      string
	final      *resultEvent
	missing    []string
	checked    bool // the init event has been seen
	toolNames  map[string]string
	unreadable []string
}

func (w *watcher) reset(startedAt int64) {
	*w = watcher{startedAt: startedAt, label: "Waiting for connectors", toolNames: map[string]string{}}
}

func (w *watcher) poll(r *runner) *progress {
	m := r.readMeta()
	if m == nil {
		return nil
	}
	if m.StartedAt != w.startedAt || w.toolNames == nil {
		w.reset(m.StartedAt)
	}
	w.readNew(r)
	if w.final != nil {
		r.finish(*w.final, *m, w.unreadable)
		return nil
	}
	if len(w.missing) > 0 {
		// Without these the answer would quietly skip a source, so stop now
		// rather than pay for it.
		r.kill(m.Pid)
		r.fail(fmt.Sprintf("These connectors didn't load: %s. Ask again with ctrl+r.", strings.Join(w.missing, ", ")))
		return nil
	}
	if !r.alive(m.Pid) {
		// It may have written the result line between our read and its exit.
		w.readNew(r)
		if w.final != nil {
			r.finish(*w.final, *m, w.unreadable)
		} else if msg := lastChars(r.stderrPath(), 600); msg != "" {
			r.fail(msg)
		} else {
			r.fail("Claude exited without an answer.")
		}
		return nil
	}
	if r.now().Sub(time.UnixMilli(m.StartedAt)) > maxRun {
		r.kill(m.Pid)
		r.fail(fmt.Sprintf("Claude was still going after %d minutes, so the run was stopped.", int(maxRun.Minutes())))
		return nil
	}
	return &progress{
		startedAt: time.UnixMilli(m.StartedAt),
		label:     w.label,
		steps:     w.steps,
		workflow:  m.Workflow,
		unknown:   m.UnknownPlaceholders,
	}
}

func (w *watcher) readNew(r *runner) {
	f, err := os.Open(r.streamPath())
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return
	}
	w.offset += int64(len(data))
	// Keep a line split across two reads (bytes, so a multi-byte character
	// split in two stays intact) until its newline arrives.
	data = append(w.partial, data...)
	lines := bytes.Split(data, []byte("\n"))
	w.partial = append([]byte(nil), lines[len(lines)-1]...)
	for _, line := range lines[:len(lines)-1] {
		w.handle(line)
	}
}

func (w *watcher) handle(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	var ev streamEvent
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	var blocks []block
	if ev.Message != nil {
		_ = json.Unmarshal(ev.Message.Content, &blocks)
	}
	switch {
	case ev.Type == "system" && ev.Subtype == "init":
		w.missing = nil
		for _, name := range requiredServers {
			connected := false
			for _, s := range ev.McpServers {
				if s.Name == name && s.Status == "connected" {
					connected = true
				}
			}
			if !connected {
				w.missing = append(w.missing, strings.TrimPrefix(name, "claude.ai "))
			}
		}
		w.label = "Thinking"
	case ev.Type == "assistant":
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			w.steps++
			w.label = describeTool(b.Name)
			if b.ID != "" {
				w.toolNames[b.ID] = b.Name
			}
		}
	case ev.Type == "user":
		// Tool results come back as user messages. One that was too big
		// never reached the model.
		for _, b := range blocks {
			if b.Type == "tool_result" && oversized.MatchString(toolResultText(b.Content)) {
				w.unreadable = append(w.unreadable, describeSource(w.toolNames[b.ToolUseID]))
			}
		}
	case ev.Type == "result":
		var res resultEvent
		if json.Unmarshal(line, &res) == nil {
			w.final = &res
		}
	}
}

func (r *runner) finish(ev resultEvent, m runMeta, unreadable []string) {
	if ev.IsError || ev.Subtype != "success" || ev.StructuredOutput == nil {
		msg := strings.TrimSpace(ev.Result)
		if msg == "" {
			sub := ev.Subtype
			if sub == "" {
				sub = "unknown"
			}
			msg = fmt.Sprintf("Claude stopped without an answer (%s).", sub)
		}
		r.fail(msg)
		return
	}
	denied := []string{}
	for _, d := range ev.PermissionDenials {
		name := d.ToolName
		if name == "" {
			name = "unknown"
		}
		denied = append(denied, name)
	}
	res := storedResult{
		Answer:     *ev.StructuredOutput,
		Model:      mainModel(ev.ModelUsage),
		DurationMs: ev.DurationMs,
		CostUsd:    ev.TotalCostUsd,
		Denied:     denied,
		Unreadable: unreadable,
		Workflow:   m.Workflow,
		FinishedAt: r.now().UnixMilli(),
	}
	_ = writeJSON(r.resultPath(), res)
	r.archive("success", "result.json", res)
}
