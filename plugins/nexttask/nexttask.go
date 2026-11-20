// Package nexttask asks Claude what to work on next: it runs Claude Code
// headless and read-only against your calendar, Jira, Slack and Gmail, and
// shows one pick, alternatives and notes. It is ported from the Raycast
// "next-task" extension.
package nexttask

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jakeobsen/gt/kit"
)

const (
	defaultReuseMinutes   = 30
	defaultMeetingMinutes = 60
	// pollEvery is how often a running run's stream is read: cheap, and
	// fine-grained enough for a run that takes a minute.
	pollEvery = time.Second
	// clockEvery keeps meeting countdowns honest between runs.
	clockEvery = 30 * time.Second
)

var defaultResolvedEmojis = []string{"white_check_mark", "heavy_check_mark"}

// System calls, replaced in tests.
var (
	openURL  = kit.Open
	copyText = kit.Copy
)

// Plugin returns the next-task plugin.
func Plugin() kit.Plugin {
	return kit.Plugin{
		Name:        "next-task",
		Title:       "Next Task",
		Description: "Ask Claude what to work on next, based on your calendar, Jira board, Slack and Gmail.",
		Settings: []kit.Setting{
			{Key: "model", Kind: kit.Choice, Default: "claude-opus-5-5",
				Description: "Model that picks the task.",
				Options: []kit.Option{
					{Value: "claude-opus-5-5", Description: "Opus 5.5: picked better tasks in side-by-side runs"},
					{Value: "claude-sonnet-5-5", Description: "Sonnet 5.5: faster and cheaper, but cut corners"},
					{Value: "claude-fable-5-1", Description: "Fable 5.1"},
				}},
			{Key: "effort", Kind: kit.Choice, Default: "medium",
				Description: "Higher effort reads more carefully but takes longer and costs more.",
				Options: []kit.Option{
					{Value: "low"}, {Value: "medium"}, {Value: "high"}, {Value: "xhigh", Description: "extra high"},
				}},
			{Key: "reuse-minutes", Kind: kit.Int, Default: defaultReuseMinutes,
				Description: "Opening Next Task within this many minutes of the last answer shows it instead of asking again. 0 asks every time; ctrl+r always asks."},
			{Key: "meeting-window-minutes", Kind: kit.Int, Default: defaultMeetingMinutes,
				Description: "Your next meeting is shown at the top when it starts within this many minutes, and stays until it ends."},
			{Key: "save-for-later", Kind: kit.Bool, Default: true,
				Description: "Save tasks and notes to come back to; Claude checks where each stands. When false the actions and section are hidden and the list isn't sent to Claude, but saved items are kept."},
			{Key: "resolved-reactions", Kind: kit.String, Default: "white_check_mark, heavy_check_mark",
				Description: "Slack reactions (comma-separated, colons optional) that mark a request as done. Inserted wherever the workflow says {{resolvedReactions}}."},
			{Key: "claude-path", Kind: kit.Path, Default: "~/.local/bin/claude",
				Description: "The claude binary: a path, or a bare name to look up on PATH."},
		},
		Commands: []kit.Command{{
			Name:        "next-task",
			Title:       "Next Task",
			Description: "Runs Claude Code headless to pick the single most useful thing to work on next, with alternatives.",
			Keywords:    []string{"claude", "todo", "focus"},
			Open:        open,
		}},
	}
}

type prefs struct {
	model          string
	effort         string
	reuseMinutes   int
	meetingMinutes int
	saveForLater   bool
	resolvedEmojis []string
	claudePath     string
}

var emojiSep = regexp.MustCompile(`[\s,]+`)

func prefsFrom(s kit.Settings) prefs {
	p := prefs{
		model:          s.String("model"),
		effort:         s.String("effort"),
		reuseMinutes:   s.Int("reuse-minutes"),
		meetingMinutes: s.Int("meeting-window-minutes"),
		saveForLater:   s.Bool("save-for-later"),
		claudePath:     strings.TrimSpace(s.String("claude-path")),
	}
	// Negative makes no sense; 0 is a legitimate answer.
	if p.reuseMinutes < 0 {
		p.reuseMinutes = defaultReuseMinutes
	}
	if p.meetingMinutes < 0 {
		p.meetingMinutes = defaultMeetingMinutes
	}
	for _, name := range emojiSep.Split(s.String("resolved-reactions"), -1) {
		if name = strings.Trim(name, ":"); name != "" {
			p.resolvedEmojis = append(p.resolvedEmojis, name)
		}
	}
	if len(p.resolvedEmojis) == 0 {
		p.resolvedEmojis = defaultResolvedEmojis
	}
	home, _ := os.UserHomeDir()
	switch {
	case p.claudePath == "":
		p.claudePath = filepath.Join(home, ".local", "bin", "claude")
	case !strings.Contains(p.claudePath, "/"):
		if found, err := exec.LookPath(p.claudePath); err == nil {
			p.claudePath = found
		}
	case !filepath.IsAbs(p.claudePath):
		p.claudePath = filepath.Join(home, p.claudePath)
	}
	return p
}

// screen is one open Next Task list: its watcher survives reloads, and the
// first load decides whether opening it asks Claude.
type screen struct {
	mu     sync.Mutex
	p      prefs
	r      *runner
	w      watcher
	opened bool
}

func open(ctx kit.Context) (kit.View, error) {
	dir, err := ctx.DataDir()
	if err != nil {
		return nil, err
	}
	s := &screen{p: prefsFrom(ctx.Settings()), r: newRunner(dir)}
	return &kit.List{
		Title:             "Next Task",
		SearchPlaceholder: "Filter tasks and notes",
		ShowDetail:        true,
		Load:              s.load,
	}, nil
}

func (s *screen) load(ctx kit.Context) (kit.Content, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Poll before reading the stored answer: a run that finished while gt
	// was closed gets recorded by this first poll.
	prog := s.w.poll(s.r)
	if !s.opened {
		s.opened = true
		// Opening the command is the request: ask straight away unless a
		// recent answer is on hand or a run is already going.
		res := s.r.loadResult()
		fresh := res != nil && s.r.now().Sub(time.UnixMilli(res.FinishedAt)) < time.Duration(s.p.reuseMinutes)*time.Minute
		if prog == nil && !fresh {
			ask(ctx, s.p, s.r)
			prog = s.w.poll(s.r)
		}
	}
	return s.content(ctx, prog, s.r.now()), nil
}

// ask starts a run, reporting problems as toasts.
func ask(ctx kit.Context, p prefs, r *runner) {
	store := ctx.Storage()
	wf := renderWorkflow(loadWorkflow(store), p, r.now())
	if len(wf.unknown) > 0 {
		ctx.Toast(kit.Failure, "Unknown placeholders in workflow",
			strings.Join(wf.unknown, ", ")+" sent to Claude as-is. Fix them with ctrl+e.")
	}
	var saved []savedItem
	if p.saveForLater {
		saved = loadSaved(store)
	}
	if err := r.start(p, wf, saved); err != nil {
		ctx.Toast(kit.Failure, "Could not ask Claude", capitalize(err.Error()))
	}
}

func (s *screen) commonActions(running bool) []kit.Action {
	p, r := s.p, s.r
	var acts []kit.Action
	if running {
		acts = append(acts, kit.Action{Title: "Stop Run", Shortcut: "ctrl+x", Destructive: true, Run: func(ctx kit.Context) error {
			if !ctx.Confirm(kit.Confirmation{
				Title: "Stop this run?", Message: "Whatever Claude has read so far is thrown away.",
				Action: "Stop", Destructive: true,
			}) {
				return nil
			}
			s.mu.Lock()
			r.stop()
			s.mu.Unlock()
			ctx.Refresh()
			return nil
		}})
	} else {
		acts = append(acts, kit.Action{Title: "Ask Again", Shortcut: "ctrl+r", Run: func(ctx kit.Context) error {
			s.mu.Lock()
			ask(ctx, p, r)
			s.mu.Unlock()
			ctx.Refresh()
			return nil
		}})
	}
	return append(acts,
		kit.Action{Title: "Edit Workflow", Shortcut: "ctrl+e", Run: func(ctx kit.Context) error { return editWorkflow(ctx, p) }},
		kit.Action{Title: "Copy Workflow", Run: func(ctx kit.Context) error {
			text := loadWorkflow(ctx.Storage())
			if text == "" {
				text = exampleWorkflow
			}
			if err := copyText(text); err != nil {
				return err
			}
			ctx.Toast(kit.Success, "Workflow copied", "")
			return nil
		}},
		kit.Action{Title: "Reset Workflow to Example", Destructive: true, Run: func(ctx kit.Context) error {
			if loadWorkflow(ctx.Storage()) == "" {
				ctx.Toast(kit.Success, "Already using the built-in example", "")
				return nil
			}
			if !ctx.Confirm(kit.Confirmation{
				Title: "Reset your workflow to the example?", Message: "Your saved workflow is deleted. Copy it first if you want to keep it.",
				Action: "Reset", Destructive: true,
			}) {
				return nil
			}
			if err := saveWorkflow(ctx.Storage(), ""); err != nil {
				return err
			}
			ctx.Toast(kit.Success, "Workflow reset to the example", "")
			return nil
		}},
	)
}

// scissors separates the workflow from the help text below it in the
// editor; everything from it down is ignored, as in a git commit message.
const scissors = "# ------------------------ >8 ------------------------"

func editorHelp(p prefs, now time.Time) string {
	values := placeholderValues(p, now)
	var b strings.Builder
	b.WriteString("\n" + scissors + "\n")
	b.WriteString("# Everything from the line above down is ignored. Save and quit to keep your\n")
	b.WriteString("# workflow; delete everything above it to go back to the built-in example.\n#\n")
	b.WriteString("# It's what Claude checks and how it judges it, wrapped in fixed rules you can't\n")
	b.WriteString("# edit out: read-only, treat what is read as data, fetch every page, and the\n")
	b.WriteString("# answer format.\n#\n")
	b.WriteString("# Placeholders, filled in when a run starts:\n")
	for _, ph := range placeholderHelp {
		fmt.Fprintf(&b, "#   %-21s %s (now: %s)\n", "{{"+ph.name+"}}", ph.help, values[ph.name])
	}
	return b.String()
}

// stripHelp drops the scissors line and everything after it.
func stripHelp(text string) string {
	if i := strings.Index(text, scissors); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text) + "\n"
}

// editWorkflow opens the workflow in the user's editor.
func editWorkflow(ctx kit.Context, p prefs) error {
	store := ctx.Storage()
	saved := loadWorkflow(store)
	current := saved
	if current == "" {
		current = exampleWorkflow
	}
	edited, err := ctx.EditText("next-task-workflow.md", strings.TrimRight(current, "\n")+"\n"+editorHelp(p, time.Now()))
	if err != nil {
		return err
	}
	edited = stripHelp(edited)
	switch {
	case strings.TrimSpace(edited) == strings.TrimSpace(current):
		ctx.Toast(kit.Success, "Workflow unchanged", "")
		return nil
	case strings.TrimSpace(edited) == "":
		if err := saveWorkflow(store, ""); err != nil {
			return err
		}
		ctx.Toast(kit.Success, "Workflow cleared", "Using the built-in example.")
		return nil
	}
	if err := saveWorkflow(store, edited); err != nil {
		return err
	}
	ctx.Toast(kit.Success, "Workflow saved", "")
	return nil
}

func (s *screen) saveActions(ctx kit.Context, saved []savedItem, title, url, why string) []kit.Action {
	if !s.p.saveForLater {
		return nil
	}
	if existing := findSaved(saved, url, title); existing != nil {
		return []kit.Action{s.letGoAction(*existing)}
	}
	return []kit.Action{{Title: "Save for Later", Shortcut: "ctrl+s", Run: func(ctx kit.Context) error {
		if err := saveForLater(ctx.Storage(), title, url, why, time.Now()); err != nil {
			return err
		}
		ctx.Toast(kit.Success, "Saved for later", title)
		ctx.Refresh()
		return nil
	}}}
}

func (s *screen) letGoAction(item savedItem) kit.Action {
	return kit.Action{Title: "Let Go", Shortcut: "ctrl+d", Run: func(ctx kit.Context) error {
		if err := letGo(ctx.Storage(), item.ID); err != nil {
			return err
		}
		ctx.Toast(kit.Success, "Let go", item.Title)
		ctx.Refresh()
		return nil
	}}
}

func openURLAction(title, url string) kit.Action {
	return kit.Action{Title: title, Run: func(ctx kit.Context) error {
		if err := openURL(url, ""); err != nil {
			return err
		}
		ctx.Close()
		return nil
	}}
}

func copyAction(title, text, shortcut string) kit.Action {
	return kit.Action{Title: title, Shortcut: shortcut, Run: func(ctx kit.Context) error {
		if err := copyText(text); err != nil {
			return err
		}
		ctx.Toast(kit.Success, "Copied", "")
		return nil
	}}
}

func join(groups ...[]kit.Action) []kit.Action {
	var out []kit.Action
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, kit.Divider)
		}
		out = append(out, g...)
	}
	return out
}

type timedMeeting struct {
	meeting
	start, end time.Time
}

// timedMeetings are the meetings whose times parse, soonest first.
func timedMeetings(ms []meeting) []timedMeeting {
	var out []timedMeeting
	for _, m := range ms {
		start, err1 := time.Parse(time.RFC3339, m.Start)
		end, err2 := time.Parse(time.RFC3339, m.End)
		if err1 == nil && err2 == nil {
			out = append(out, timedMeeting{meeting: m, start: start, end: end})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].start.Before(out[j].start) })
	return out
}

// soonMeeting is the meeting to show at the top: not over yet, and starting
// within the window (or already started).
func soonMeeting(ms []timedMeeting, now time.Time, windowMinutes int) *timedMeeting {
	for i, m := range ms {
		if m.end.After(now) && m.start.Sub(now) <= time.Duration(windowMinutes)*time.Minute {
			return &ms[i]
		}
	}
	return nil
}

func meetingDetails(m timedMeeting) string {
	parts := []string{m.Title, formatTimeRange(m.start, m.end)}
	if m.URL != "" {
		parts = append(parts, m.URL)
	} else if m.Location != "" {
		parts = append(parts, m.Location)
	}
	if len(m.Attendees) > 0 {
		parts = append(parts, "With "+strings.Join(m.Attendees, ", "))
	}
	return strings.Join(parts, "\n")
}

func paragraphs(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *screen) content(ctx kit.Context, prog *progress, now time.Time) kit.Content {
	p := s.p
	running := prog != nil
	common := s.commonActions(running)
	res := s.r.loadResult()
	runErr := s.r.loadError()
	var saved []savedItem
	if p.saveForLater {
		saved = loadSaved(ctx.Storage())
	}

	var sections []kit.Section
	if prog != nil {
		detail := paragraphs(
			"Claude is reading your calendar, Jira, Slack and Gmail with read-only tools.",
			"You can close goto. The run keeps going, and the answer will be here next time you open **Next Task**.",
		)
		if prog.workflow == "example" {
			detail = paragraphs(detail, "No workflow saved yet, so this run uses the built-in example. Press **ctrl+e** to set your own.")
		}
		if len(prog.unknown) > 0 {
			detail = paragraphs(detail, "Your workflow has unknown placeholders, sent to Claude as-is: "+strings.Join(prog.unknown, ", ")+". Fix them with **ctrl+e**.")
		}
		sections = append(sections, kit.Section{Title: "Asking Claude", Items: []kit.Item{{
			Title:    prog.label,
			Subtitle: fmt.Sprintf("%d %s · %s", prog.steps, plural(prog.steps, "step", "steps"), formatElapsed(now.Sub(prog.startedAt))),
			Detail:   detail,
			Actions:  common,
		}}})
	}

	if runErr != nil && !running {
		sections = append(sections, kit.Section{Title: "Last Run Failed", Items: []kit.Item{{
			Title:       firstLine(runErr.Message),
			Subtitle:    formatAge(time.UnixMilli(runErr.At), now),
			Accessories: []kit.Accessory{{Text: "failed", Color: kit.Red}},
			Detail:      "```\n" + runErr.Message + "\n```",
			Actions:     join(common, []kit.Action{copyAction("Copy Error", runErr.Message, "")}),
		}}})
	}

	if res != nil {
		a := res.Answer
		if m := soonMeeting(timedMeetings(a.UpcomingMeetings), now, p.meetingMinutes); m != nil {
			sections = append(sections, kit.Section{Title: "Meeting", Subtitle: formatStartsIn(m.start, now),
				Items: []kit.Item{s.meetingItem(*m, now, common)}})
		}
		sections = append(sections, kit.Section{Title: "Next", Items: []kit.Item{s.taskItem(ctx, a.Next, saved, common)}})
		if len(a.Alternatives) > 0 {
			alt := kit.Section{Title: "Alternatives"}
			for _, t := range a.Alternatives {
				alt.Items = append(alt.Items, s.taskItem(ctx, t, saved, common))
			}
			sections = append(sections, alt)
		}
		notes := kit.Section{Title: "Notes"}
		if n := len(res.Unreadable); n > 0 {
			note := fmt.Sprintf("Claude couldn't read %s because %s too large (%s), so this answer may have missed something. Ask again with ctrl+r.",
				plural(n, "one result", fmt.Sprintf("%d results", n)), plural(n, "it was", "they were"), strings.Join(unique(res.Unreadable), ", "))
			notes.Items = append(notes.Items, kit.Item{Title: note, Accessories: []kit.Accessory{{Text: "partial", Color: kit.Orange}}, Detail: note, Actions: common})
		}
		for _, flag := range a.Flags {
			notes.Items = append(notes.Items, kit.Item{
				Title:   flag,
				Detail:  flag,
				Actions: join([]kit.Action{copyAction("Copy Note", flag, "")}, s.saveActions(ctx, saved, flag, "", ""), common),
			})
		}
		if len(notes.Items) > 0 {
			sections = append(sections, notes)
		}
	}

	// Saved items are yours, not part of an answer, so they show even before
	// the first answer.
	if p.saveForLater && len(saved) > 0 {
		sec := kit.Section{Title: "Saved for Later"}
		for _, it := range saved {
			sec.Items = append(sec.Items, s.savedItem(it, res, now, common))
		}
		sections = append(sections, sec)
	}

	refresh := clockEvery
	if running {
		refresh = pollEvery
	}
	return kit.Content{
		Sections: sections,
		Empty: kit.Empty{
			Title:       "No answer yet",
			Description: "Press ctrl+r to ask Claude what to work on next.",
			Actions:     common,
		},
		RefreshAfter: refresh,
	}
}

func (s *screen) taskItem(ctx kit.Context, t task, saved []savedItem, common []kit.Action) kit.Item {
	accs := []kit.Accessory{{Text: taskApp(t)}}
	link := ""
	if isLink(t.URL) {
		link = t.URL
	}
	if s.p.saveForLater && findSaved(saved, link, t.Title) != nil {
		accs = append([]kit.Accessory{{Text: "saved", Color: kit.Green}}, accs...)
	}
	var first []kit.Action
	if link != "" {
		first = append(first, openURLAction("Open in Browser", link))
	}
	first = append(first,
		copyAction("Copy Link", t.URL, "ctrl+y"),
		copyAction("Copy Task", t.Title+"\n"+t.Why+"\n"+t.URL, ""),
	)
	return kit.Item{
		Title:       t.Title,
		Accessories: accs,
		Detail:      "## " + t.Title + "\n\n" + t.Why,
		Actions:     join(first, s.saveActions(ctx, saved, t.Title, link, t.Why), common),
	}
}

func (s *screen) meetingItem(m timedMeeting, now time.Time, common []kit.Action) kit.Item {
	virtual := isLink(m.URL)
	when := formatTimeRange(m.start, m.end) + " · " + formatStartsIn(m.start, now)
	where := ""
	switch {
	case m.Location != "":
		where = "At " + m.Location + "."
	case !virtual:
		where = "No location given."
	}
	with := ""
	if len(m.Attendees) > 0 {
		with = "With " + strings.Join(m.Attendees, ", ") + "."
	}
	call := ""
	var acts []kit.Action
	if virtual {
		call = "Video call. Press enter to join."
		acts = append(acts, openURLAction("Join Meeting", m.URL), copyAction("Copy Meeting Link", m.URL, "ctrl+y"))
	}
	acts = append(acts, copyAction("Copy Meeting Details", meetingDetails(m), ""))
	return kit.Item{
		Title:       m.Title,
		Subtitle:    when,
		Accessories: []kit.Accessory{{Text: "Calendar"}},
		Detail:      paragraphs("## "+m.Title, "**"+when+"**", call, where, with, m.Notes),
		Actions:     join(acts, common),
	}
}

var statusTags = map[string]kit.Accessory{
	"done":    {Text: "Looks done", Color: kit.Green},
	"waiting": {Text: "Waiting", Color: kit.Orange},
}

func (s *screen) savedItem(it savedItem, res *storedResult, now time.Time, common []kit.Action) kit.Item {
	var status *savedStatus
	if res != nil {
		for i, st := range res.Answer.SavedStatus {
			if st.ID == it.ID {
				status = &res.Answer.SavedStatus[i]
			}
		}
	}
	var accs []kit.Accessory
	note := ""
	if status != nil {
		if tag, ok := statusTags[status.Status]; ok {
			accs = append(accs, tag)
		}
		if status.Note != "" {
			note = "Now: " + status.Note
		}
	}
	if app := appFor(it.URL); app != "" {
		accs = append(accs, kit.Accessory{Text: app})
	}
	var acts []kit.Action
	if isLink(it.URL) {
		acts = append(acts, openURLAction("Open in Browser", it.URL))
	}
	acts = append(acts, s.letGoAction(it))
	return kit.Item{
		Title:       it.Title,
		Accessories: accs,
		Detail:      paragraphs("## "+it.Title, it.Why, note, "Saved "+formatAge(time.UnixMilli(it.SavedAt), now)+"."),
		Actions:     join(acts, common),
	}
}

func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
