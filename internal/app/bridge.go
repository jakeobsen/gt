package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jakeobsen/gt/kit"
)

// Messages plugin code and screens send to the Model.
type (
	// popMsg closes the top screen (quitting when it is the root).
	popMsg struct{}
	// busyMsg marks plugin code as running; keys are ignored until doneMsg.
	busyMsg struct{ text string }
	// doneMsg reports that plugin code finished, with its error if it failed.
	doneMsg struct{ err error }
	// hudMsg closes goto and prints text afterwards.
	hudMsg struct{ text string }
	// closeMsg closes goto.
	closeMsg struct{}
	// toastMsg shows or updates a toast in the status line.
	toastMsg struct {
		id      int64
		style   kit.ToastStyle
		title   string
		message string
	}
	// confirmMsg asks a question; the answer goes back on reply.
	confirmMsg struct {
		c     kit.Confirmation
		reply chan bool
	}
	// pushMsg opens a plugin's view on top of the stack.
	pushMsg struct {
		view kit.View
		ctx  *commandContext
	}
	// refreshMsg reloads the list on top of the stack.
	refreshMsg struct{}
	// contentMsg delivers a plugin list's contents to the screen with that id.
	contentMsg struct {
		screen int
		items  []listItem
		empty  kit.Empty
		emptyA []action
		err    error
		// refreshAfter schedules a quiet reload.
		refreshAfter time.Duration
	}
	// openMenuMsg opens the ctrl+k actions menu.
	openMenuMsg struct {
		title   string
		actions []action
	}
	// openArgsMsg asks for a command's arguments.
	openArgsMsg struct {
		cmd kit.Command
		ctx *commandContext
	}
	// openFilterMsg opens the filter picker for the list with that id.
	openFilterMsg struct {
		screen  int
		filter  *kit.Filter
		current string
	}
	// filterChosenMsg sets the filter of the list with that id.
	filterChosenMsg struct {
		screen int
		value  string
	}
	// tickMsg is a list's scheduled quiet refresh; gen must still match.
	tickMsg struct {
		screen int
		gen    int
	}
	// execMsg runs an interactive program in the terminal with goto paused,
	// then reports how it ended on done.
	execMsg struct {
		cmd  *exec.Cmd
		done chan error
	}
)

// bridge carries messages from plugin goroutines to the UI loop.
type bridge struct {
	send func(tea.Msg)
	// openTerminal opens a terminal as the "terminal" setting says.
	openTerminal func(dir, command string) error
	// storage returns a plugin's persistent store.
	storage func(plugin string) kit.Storage
	// dataDir returns the folder for a plugin's own files.
	dataDir func(plugin string) string
	// exec runs an interactive program with goto paused. Set by Run to go
	// through the UI loop; tests replace it.
	exec   func(cmd *exec.Cmd) error
	toasts atomic.Int64
}

// execViaUI sends cmd to the UI loop, which pauses goto while it runs, and
// waits for it to finish.
func (b *bridge) execViaUI(cmd *exec.Cmd) error {
	done := make(chan error, 1)
	b.send(execMsg{cmd: cmd, done: done})
	return <-done
}

// commandContext is the kit.Context plugin code runs with: the bridge, its
// plugin's settings and the arguments its command was given.
type commandContext struct {
	b        *bridge
	plugin   string
	title    string // the command's title, for screens it opens
	settings kit.Settings
	store    kit.Storage
	args     map[string]string
	filter   string
}

func (c *commandContext) HUD(text string)             { c.b.send(hudMsg{text: text}) }
func (c *commandContext) Close()                      { c.b.send(closeMsg{}) }
func (c *commandContext) Settings() kit.Settings      { return c.settings }
func (c *commandContext) Argument(name string) string { return c.args[name] }
func (c *commandContext) Push(v kit.View)             { c.b.send(pushMsg{view: v, ctx: c}) }
func (c *commandContext) Storage() kit.Storage        { return c.store }

func (c *commandContext) DataDir() (string, error) {
	dir := c.b.dataDir(c.plugin)
	return dir, os.MkdirAll(dir, 0o700)
}

func (c *commandContext) EditText(name, text string) (string, error) {
	dir, err := os.MkdirTemp("", "gt-edit-")
	if err != nil {
		return text, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return text, err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// Through sh, so editors configured with arguments ("code --wait") work.
	cmd := exec.Command("/bin/sh", "-c", editor+` "$1"`, "sh", path)
	if err := c.b.exec(cmd); err != nil {
		return text, fmt.Errorf("the editor (%s) failed: %w", editor, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return text, err
	}
	return string(data), nil
}
func (c *commandContext) Filter() string { return c.filter }
func (c *commandContext) Refresh()       { c.b.send(refreshMsg{}) }

func (c *commandContext) OpenTerminal(dir, command string) error {
	return c.b.openTerminal(dir, command)
}

func (c *commandContext) Toast(style kit.ToastStyle, title, message string) kit.Toast {
	t := toastHandle{b: c.b, id: c.b.toasts.Add(1)}
	t.Update(style, title, message)
	return t
}

func (c *commandContext) Confirm(cf kit.Confirmation) bool {
	reply := make(chan bool, 1)
	c.b.send(confirmMsg{c: cf, reply: reply})
	return <-reply
}

// withFilter returns a copy of c carrying a list's filter value.
func (c *commandContext) withFilter(value string) *commandContext {
	cp := *c
	cp.filter = value
	return &cp
}

// withArgs returns a copy of c carrying the given argument values.
func (c *commandContext) withArgs(args map[string]string) *commandContext {
	cp := *c
	cp.args = args
	return &cp
}

type toastHandle struct {
	b  *bridge
	id int64
}

func (t toastHandle) Update(style kit.ToastStyle, title, message string) {
	t.b.send(toastMsg{id: t.id, style: style, title: title, message: message})
}

// runPlugin returns a command that runs f (plugin code) off the UI loop,
// marking the UI busy until it finishes.
func runPlugin(busyText string, f func() error) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return busyMsg{text: busyText} },
		func() tea.Msg { return doneMsg{err: safely(f)} },
	)
}

// wrapActions turns a plugin's actions into UI actions bound to ctx.
func wrapActions(actions []kit.Action, ctx *commandContext) []action {
	var out []action
	for _, a := range actions {
		if a.IsDivider() {
			out = append(out, action{divider: true})
			continue
		}
		a := a
		out = append(out, action{
			title:       a.Title,
			shortcut:    a.Shortcut,
			destructive: a.Destructive,
			run: func() tea.Cmd {
				return runPlugin("", func() error {
					if a.Run == nil {
						return nil
					}
					return a.Run(ctx)
				})
			},
		})
	}
	return out
}

// convert turns a plugin list's contents into list items bound to ctx.
func convert(content kit.Content, ctx *commandContext) []listItem {
	var items []listItem
	for g, sec := range content.Sections {
		for _, it := range sec.Items {
			row := listRow{
				group:     g,
				header:    sec.Title,
				headerSub: sec.Subtitle,
				title:     it.Title,
				subtitle:  it.Subtitle,
				keywords:  it.Keywords,
			}
			for _, a := range it.Accessories {
				row.accessories = append(row.accessories, accessory{text: a.Text, color: a.Color})
			}
			items = append(items, listItem{row: row, actions: wrapActions(it.Actions, ctx), detail: it.Detail})
		}
	}
	return items
}

// loader returns a function that loads a plugin list's contents for the
// screen with id sid, with the list's filter set to the value it is given.
func loader(sid int, l *kit.List, base *commandContext) func(filter string) tea.Cmd {
	return func(filter string) tea.Cmd {
		ctx := base.withFilter(filter)
		return func() tea.Msg {
			var content kit.Content
			err := safely(func() error {
				var e error
				content, e = l.Load(ctx)
				return e
			})
			if err != nil {
				return contentMsg{screen: sid, err: err}
			}
			return contentMsg{
				screen:       sid,
				items:        convert(content, ctx),
				empty:        content.Empty,
				emptyA:       wrapActions(content.Empty.Actions, ctx),
				refreshAfter: content.RefreshAfter,
			}
		}
	}
}
