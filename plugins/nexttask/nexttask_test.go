package nexttask

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

// --- fakes ------------------------------------------------------------------

type memStore map[string]string

func (m memStore) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memStore) Set(k, v string) error       { m[k] = v; return nil }
func (m memStore) Delete(k string) error       { delete(m, k); return nil }
func (m memStore) Keys() []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

type fakeCtx struct {
	settings kit.Settings
	store    memStore
	dataDir  string
	answer   bool
	toasts   []string
	// edit transforms the text handed to EditText.
	edit func(string) string
}

func (c *fakeCtx) HUD(string)                        {}
func (c *fakeCtx) Close()                            {}
func (c *fakeCtx) Settings() kit.Settings            { return c.settings }
func (c *fakeCtx) Argument(string) string            { return "" }
func (c *fakeCtx) Push(kit.View)                     {}
func (c *fakeCtx) Refresh()                          {}
func (c *fakeCtx) Storage() kit.Storage              { return c.store }
func (c *fakeCtx) Filter() string                    { return "" }
func (c *fakeCtx) DataDir() (string, error)          { return c.dataDir, os.MkdirAll(c.dataDir, 0o700) }
func (c *fakeCtx) OpenTerminal(string, string) error { return nil }
func (c *fakeCtx) Confirm(kit.Confirmation) bool     { return c.answer }
func (c *fakeCtx) EditText(_, text string) (string, error) {
	if c.edit == nil {
		return text, nil
	}
	return c.edit(text), nil
}
func (c *fakeCtx) Toast(_ kit.ToastStyle, title, message string) kit.Toast {
	c.toasts = append(c.toasts, strings.TrimSpace(title+" "+message))
	return nopToast{}
}

type nopToast struct{}

func (nopToast) Update(kit.ToastStyle, string, string) {}

func newCtx(t *testing.T, toml string) *fakeCtx {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[plugins.next-task]\n"+toml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, []kit.Plugin{Plugin()})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeCtx{settings: cfg.Plugin("next-task"), store: memStore{}, dataDir: filepath.Join(t.TempDir(), "data")}
}

func init() { timezoneName = func() string { return "Europe/Copenhagen" } }

// --- prompt -------------------------------------------------------------------

func TestPluginIsValid(t *testing.T) {
	if err := kit.Validate([]kit.Plugin{Plugin()}); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceholders(t *testing.T) {
	p := prefsFrom(newCtx(t, "resolved-reactions = \":merged:, white_check_mark\"\n").settings)
	now := time.Date(2026, 11, 22, 15, 20, 0, 0, time.Local) // a Sunday
	wf := renderWorkflow("On {{weekday}} {{ date }} at {{time}} ({{timezone}}), week of {{weekStart}}: {{resolvedReactions}} {{dat}} {{dat}}", p, now)
	want := "On Sunday 2026-11-22 at 15:20 (Europe/Copenhagen), week of 2026-11-16: merged, white_check_mark {{dat}} {{dat}}"
	if wf.text != want || wf.source != "saved" || !slices.Equal(wf.unknown, []string{"{{dat}}"}) {
		t.Fatalf("got %q %s %v", wf.text, wf.source, wf.unknown)
	}
	if wf := renderWorkflow("  \n", p, now); wf.source != "example" || !strings.Contains(wf.text, "merged, white_check_mark") {
		t.Fatal("an empty workflow should fall back to the example")
	}
}

func TestBuildArgsIsReadOnlyAndKeepsListsLast(t *testing.T) {
	p := prefsFrom(newCtx(t, "").settings)
	saved := []savedItem{{ID: "ab12", Title: "Line\none", URL: "https://x/1", Why: "secret reason", SavedAt: 0}}
	args := buildArgs(p, time.Now(), workflow{text: "MY WORKFLOW"}, saved)

	i := slices.Index(args, "--tools")
	if i < 0 || args[i+1] != "" {
		t.Fatal(`want --tools ""`)
	}
	if args[slices.Index(args, "--permission-mode")+1] != "dontAsk" || args[slices.Index(args, "--model")+1] != "claude-opus-5-5" {
		t.Fatal("permission mode or model wrong")
	}
	allowed, denied := slices.Index(args, "--allowedTools"), slices.Index(args, "--disallowedTools")
	if allowed < 0 || denied < allowed || len(args)-denied-1 != len(deniedTools) || denied-allowed-1 != len(allowedTools) {
		t.Fatal("the variadic tool lists must come last, allowed then denied")
	}
	if !slices.Contains(args, "mcp__claude_ai_Gmail__send_message") || slices.Contains(args[allowed:denied], "mcp__claude_ai_Gmail__send_message") {
		t.Fatal("send_message must be denied, never allowed")
	}
	prompt := args[1]
	if !strings.Contains(prompt, "MY WORKFLOW") || !strings.Contains(prompt, "- [ab12] Line one — https://x/1 (saved 1970-01-01)") {
		t.Fatalf("prompt is missing the workflow or saved list:\n%s", prompt)
	}
	if strings.Contains(prompt, "secret reason") {
		t.Fatal("the saved reason must never be sent to Claude")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(args[slices.Index(args, "--json-schema")+1]), &schema); err != nil {
		t.Fatal(err)
	}

	p.saveForLater = false
	if args := buildArgs(p, time.Now(), workflow{text: "x"}, saved); strings.Contains(args[1], "ab12") {
		t.Fatal("with save-for-later off, the saved list must not reach Claude")
	}
}

// --- storage --------------------------------------------------------------------

func TestSavedItems(t *testing.T) {
	s := memStore{}
	now := time.Now()
	saveForLater(s, "Fix bug", "https://x/1", "why", now)
	saveForLater(s, "Fix bug again", "https://x/1", "dupe link", now) // same link: no-op
	saveForLater(s, "A note", "", "", now)
	saveForLater(s, "A note", "", "", now) // same link-less title: no-op
	items := loadSaved(s)
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if findSaved(items, "", "Fix bug") != nil {
		t.Fatal("a link-less lookup must not match an item with a link")
	}
	letGo(s, items[0].ID)
	if items := loadSaved(s); len(items) != 1 || items[0].Title != "A note" {
		t.Fatalf("after letGo: %+v", items)
	}
}

func TestEditWorkflow(t *testing.T) {
	ctx := newCtx(t, "")
	p := prefsFrom(ctx.settings)

	// Saving the example unchanged keeps using the example.
	editWorkflow(ctx, p)
	if loadWorkflow(ctx.store) != "" || ctx.toasts[len(ctx.toasts)-1] != "Workflow unchanged" {
		t.Fatalf("toasts = %v", ctx.toasts)
	}

	ctx.edit = func(text string) string {
		if !strings.Contains(text, scissors) || !strings.Contains(text, "{{weekStart}}") {
			t.Fatalf("the editor should get the workflow and the placeholder help:\n%s", text)
		}
		return "Check only Jira.\n" + text[strings.Index(text, scissors):]
	}
	editWorkflow(ctx, p)
	if got := loadWorkflow(ctx.store); got != "Check only Jira.\n" {
		t.Fatalf("saved %q; the help below the scissors must be stripped", got)
	}

	ctx.edit = func(string) string { return scissors + "\n# nothing above" }
	editWorkflow(ctx, p)
	if loadWorkflow(ctx.store) != "" {
		t.Fatal("clearing the editor should go back to the example")
	}
}

// --- running ----------------------------------------------------------------------

const initAllConnected = `{"type":"system","subtype":"init","mcp_servers":[{"name":"claude.ai Google Calendar","status":"connected"},{"name":"claude.ai Atlassian","status":"connected"},{"name":"claude.ai Slack","status":"connected"},{"name":"claude.ai Gmail","status":"connected"}]}`

// fakeClaude writes a script that records its arguments, prints lines as
// its event stream, then runs tail (a shell snippet).
func fakeClaude(t *testing.T, lines []string, tail string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args.txt\"\ncat <<'EOF'\n" + strings.Join(lines, "\n") + "\nEOF\n" + tail + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// waitForOutcome polls until the run has recorded a result or an error.
func waitForOutcome(t *testing.T, r *runner, w *watcher) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w.poll(r)
		if r.loadResult() != nil || r.loadError() != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the run never finished")
}

func TestRunEndToEnd(t *testing.T) {
	claude := fakeClaude(t, []string{
		initAllConnected,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__claude_ai_Slack__slack_read_channel"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"Output exceeds maximum allowed tokens (90000)"}]}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"duration_ms":61000,"total_cost_usd":0.42,"modelUsage":{"claude-haiku":{"costUSD":0.01},"claude-opus-5-5":{"costUSD":0.41}},"permission_denials":[],"structured_output":{"onBau":false,"upcomingMeetings":[],"next":{"title":"Fix the login bug","why":"Blocking QA.","source":"jira","url":"https://acme.atlassian.net/browse/X-1"},"alternatives":[],"flags":["You promised Ana a reply."],"savedStatus":[]}}`,
	}, "")
	ctx := newCtx(t, "claude-path = \""+claude+"\"\n")
	p := prefsFrom(ctx.settings)
	dir, _ := ctx.DataDir()
	r, w := newRunner(dir), &watcher{}

	if err := r.start(p, renderWorkflow("", p, time.Now()), nil); err != nil {
		t.Fatal(err)
	}
	waitForOutcome(t, r, w)

	res := r.loadResult()
	if res == nil {
		t.Fatalf("no result; error = %+v", r.loadError())
	}
	if res.Answer.Next.Title != "Fix the login bug" || res.Model != "claude-opus-5-5" || res.Workflow != "example" ||
		!slices.Equal(res.Unreadable, []string{"a Slack channel"}) {
		t.Fatalf("result = %+v", res)
	}
	args, _ := os.ReadFile(filepath.Join(filepath.Dir(claude), "args.txt"))
	if !strings.Contains(string(args), "Work out the single most useful thing") {
		t.Fatal("claude didn't get the prompt")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "history"))
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "-success") {
		t.Fatalf("history = %v", entries)
	}
	for _, f := range []string{"result.json", "meta.json", "stream.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, "history", entries[0].Name(), f)); err != nil {
			t.Errorf("history is missing %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "run")); !os.IsNotExist(err) {
		t.Fatal("the run folder should have moved to history")
	}
}

func TestMissingConnectorStopsTheRun(t *testing.T) {
	claude := fakeClaude(t, []string{
		`{"type":"system","subtype":"init","mcp_servers":[{"name":"claude.ai Google Calendar","status":"connected"},{"name":"claude.ai Atlassian","status":"connected"},{"name":"claude.ai Slack","status":"connected"},{"name":"claude.ai Gmail","status":"failed"}]}`,
	}, "sleep 30")
	ctx := newCtx(t, "claude-path = \""+claude+"\"\n")
	p := prefsFrom(ctx.settings)
	dir, _ := ctx.DataDir()
	r, w := newRunner(dir), &watcher{}
	if err := r.start(p, renderWorkflow("", p, time.Now()), nil); err != nil {
		t.Fatal(err)
	}
	pid := r.readMeta().Pid
	waitForOutcome(t, r, w)
	if e := r.loadError(); e == nil || !strings.Contains(e.Message, "These connectors didn't load: Gmail") {
		t.Fatalf("error = %+v", e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.alive(pid) {
		t.Fatal("the run was not stopped")
	}
}

func TestExitWithoutAnswerRecordsStderr(t *testing.T) {
	claude := fakeClaude(t, []string{initAllConnected}, "echo 'Not logged in' >&2; exit 1")
	ctx := newCtx(t, "claude-path = \""+claude+"\"\n")
	p := prefsFrom(ctx.settings)
	dir, _ := ctx.DataDir()
	r, w := newRunner(dir), &watcher{}
	if err := r.start(p, renderWorkflow("", p, time.Now()), nil); err != nil {
		t.Fatal(err)
	}
	waitForOutcome(t, r, w)
	if e := r.loadError(); e == nil || e.Message != "Not logged in" {
		t.Fatalf("error = %+v", e)
	}
}

func TestHungRunIsStoppedAfterTenMinutes(t *testing.T) {
	dir := t.TempDir()
	r := newRunner(dir)
	killed := 0
	r.alive = func(int) bool { return true }
	r.kill = func(int) { killed++ }
	os.MkdirAll(r.runDir(), 0o700)
	writeJSON(r.metaPath(), runMeta{Pid: 1, StartedAt: time.Now().Add(-11 * time.Minute).UnixMilli()})
	(&watcher{}).poll(r)
	if e := r.loadError(); e == nil || killed != 1 || !strings.Contains(e.Message, "still going after 10 minutes") {
		t.Fatalf("error=%+v killed=%d", e, killed)
	}
}

func TestStartRequiresTheBinary(t *testing.T) {
	ctx := newCtx(t, "claude-path = \"/nowhere/claude\"\n")
	s := &screen{p: prefsFrom(ctx.settings), r: newRunner(t.TempDir())}
	if _, err := s.load(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.toasts) != 1 || !strings.Contains(ctx.toasts[0], "Could not ask Claude No claude binary at /nowhere/claude") {
		t.Fatalf("toasts = %v", ctx.toasts)
	}
}

func TestPruneHistory(t *testing.T) {
	r := newRunner(t.TempDir())
	old := filepath.Join(r.historyDir(), "2026-01-01_00-00-00-success")
	recent := filepath.Join(r.historyDir(), "2026-11-19_00-00-00-success")
	for _, d := range []string{old, recent} {
		os.MkdirAll(d, 0o700)
	}
	ts := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(old, ts, ts)
	r.prune()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("history older than 7 days should be deleted")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("recent history was deleted")
	}
}

// --- the screen -------------------------------------------------------------------

func TestContentSections(t *testing.T) {
	ctx := newCtx(t, "reuse-minutes = 60\n")
	p := prefsFrom(ctx.settings)
	dir, _ := ctx.DataDir()
	r := newRunner(dir)
	now := time.Now()
	saveForLater(ctx.store, "Fix the login bug", "https://acme.atlassian.net/browse/X-1", "Blocking QA.", now)
	saved := loadSaved(ctx.store)
	writeJSON(r.resultPath(), storedResult{
		Answer: answer{
			UpcomingMeetings: []meeting{{Title: "Standup", Start: now.Add(10 * time.Minute).Format(time.RFC3339), End: now.Add(25 * time.Minute).Format(time.RFC3339), URL: "https://meet.google.com/abc"}},
			Next:             task{Title: "Fix the login bug", Why: "Blocking QA.", Source: "jira", URL: "https://acme.atlassian.net/browse/X-1"},
			Alternatives:     []task{{Title: "Reply to Ana", Why: "Waiting on you.", Source: "slack", URL: "https://acme.slack.com/archives/C1/p1"}},
			Flags:            []string{"You promised a review by Friday."},
			SavedStatus:      []savedStatus{{ID: saved[0].ID, Status: "waiting", Note: "QA is retesting."}},
		},
		Unreadable: []string{"a Slack channel"},
		FinishedAt: now.UnixMilli(),
	})

	s := &screen{p: p, r: r}
	c, err := s.load(ctx) // a fresh answer: opening must not ask again
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, sec := range c.Sections {
		titles = append(titles, sec.Title)
	}
	if want := []string{"Meeting", "Next", "Alternatives", "Notes", "Saved for Later"}; !slices.Equal(titles, want) {
		t.Fatalf("sections = %v, want %v", titles, want)
	}
	next := c.Sections[1].Items[0]
	if next.Accessories[0].Text != "saved" || next.Accessories[1].Text != "Jira" || next.Actions[0].Title != "Open in Browser" {
		t.Fatalf("next = %+v", next)
	}
	if !slices.ContainsFunc(next.Actions, func(a kit.Action) bool { return a.Title == "Let Go" && a.Shortcut == "ctrl+d" }) {
		t.Fatal("an already-saved task should offer Let Go")
	}
	if c.Sections[2].Items[0].Accessories[0].Text != "Slack" {
		t.Fatal("a slack.com link should be labelled Slack")
	}
	if c.Sections[3].Items[0].Accessories[0].Text != "partial" {
		t.Fatal("the unreadable note should come first in Notes")
	}
	savedRow := c.Sections[4].Items[0]
	if savedRow.Accessories[0] != (kit.Accessory{Text: "Waiting", Color: kit.Orange}) || !strings.Contains(savedRow.Detail, "Now: QA is retesting.") {
		t.Fatalf("saved row = %+v", savedRow)
	}
	if c.RefreshAfter != clockEvery {
		t.Fatalf("RefreshAfter = %v between runs, want %v", c.RefreshAfter, clockEvery)
	}
	if len(ctx.toasts) != 0 {
		t.Fatalf("opening with a fresh answer must not ask; toasts = %v", ctx.toasts)
	}
}

func TestSoonMeeting(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	ms := []timedMeeting{
		{meeting: meeting{Title: "over"}, start: now.Add(-time.Hour), end: now.Add(-time.Minute)},
		{meeting: meeting{Title: "later"}, start: now.Add(90 * time.Minute), end: now.Add(2 * time.Hour)},
	}
	if soonMeeting(ms, now, 60) != nil {
		t.Fatal("no meeting is within the window")
	}
	if m := soonMeeting(ms, now, 120); m == nil || m.Title != "later" {
		t.Fatal("want the meeting within 2 hours")
	}
}

func TestFormatting(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for start, want := range map[time.Duration]string{0: "now", 9 * time.Minute: "in 9 min", 80 * time.Minute: "in 1 h 20 min", 2 * time.Hour: "in 2 h", -3 * time.Minute: "started 3 min ago"} {
		if got := formatStartsIn(now.Add(start), now); got != want {
			t.Errorf("formatStartsIn(%v) = %q, want %q", start, got, want)
		}
	}
	for ago, want := range map[time.Duration]string{30 * time.Second: "just now", 12 * time.Minute: "12 min ago", 3 * time.Hour: "3 h ago", 50 * time.Hour: "2 d ago"} {
		if got := formatAge(now.Add(-ago), now); got != want {
			t.Errorf("formatAge(%v) = %q, want %q", ago, got, want)
		}
	}
	if formatElapsed(75*time.Second) != "1:15" {
		t.Error("formatElapsed")
	}
}
