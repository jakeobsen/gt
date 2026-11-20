// Package app is goto's terminal UI: the root search, the screens plugins
// open, and the bridge that lets plugin code talk back to the UI.
package app

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/internal/storage"
	"github.com/jakeobsen/gt/internal/terminal"
	"github.com/jakeobsen/gt/kit"
)

// Run shows the goto UI until the user quits or a command closes it. If a
// command closed it with a message (kit.Context.HUD), that message is printed
// to stdout once the UI is gone.
func Run(plugins []kit.Plugin, cfg *config.Config) error {
	dir, err := storage.Dir()
	if err != nil {
		return err
	}
	stores := storage.NewStores(dir)
	b := &bridge{
		openTerminal: func(dir, command string) error { return terminal.Open(cfg.Terminal(), dir, command) },
		storage:      func(plugin string) kit.Storage { return stores.For(plugin) },
		dataDir:      func(plugin string) string { return filepath.Join(dir, "plugins", plugin) },
	}
	b.exec = b.execViaUI
	p := tea.NewProgram(newModel(plugins, b, cfg))
	b.send = p.Send
	final, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := final.(Model); ok && m.exitMessage != "" {
		fmt.Println(m.exitMessage)
	}
	return nil
}

// toast is the message in the status line. Toasts from plugins have ids from
// 1 up; the UI's own (running…, errors) use 0.
type toast struct {
	id      int64
	style   kit.ToastStyle
	title   string
	message string
}

// Model is the whole UI: a stack of screens with a shared header, status line
// and footer, plus an optional confirmation dialog on top.
type Model struct {
	stack  []screen
	bridge *bridge
	cfg    *config.Config
	st     styles
	// fixedTheme is set when the theme setting overrides the terminal's
	// background colour.
	fixedTheme bool
	width      int
	height     int
	lastID     int

	busy      bool
	toast     *toast
	lastToast int64
	confirm   *confirmMsg

	exitMessage string
}

func newModel(plugins []kit.Plugin, b *bridge, cfg *config.Config) Model {
	m := Model{bridge: b, cfg: cfg, st: newStyles(true)}
	switch cfg.Theme() {
	case "dark", "light":
		m.fixedTheme = true
		m.st = newStyles(cfg.Theme() == "dark")
	}
	m.stack = []screen{m.rootScreen(plugins)}
	return m
}

func (m *Model) nextID() int {
	m.lastID++
	return m.lastID
}

// rootScreen lists every command from every enabled plugin, alphabetically.
func (m *Model) rootScreen(plugins []kit.Plugin) screen {
	var items []listItem
	for _, p := range plugins {
		if !m.cfg.Enabled(p.Name) {
			continue
		}
		for _, c := range p.Commands {
			ctx := &commandContext{b: m.bridge, plugin: p.Name, title: c.Title, settings: m.cfg.Plugin(p.Name), store: m.bridge.storage(p.Name)}
			items = append(items, listItem{
				row: listRow{
					title:    c.Title,
					subtitle: p.Title,
					keywords: c.Keywords,
				},
				actions: []action{commandAction(c, ctx)},
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].row, items[j].row
		if a.title != b.title {
			return strings.ToLower(a.title) < strings.ToLower(b.title)
		}
		return a.subtitle < b.subtitle
	})
	s := newListScreen(m.nextID(), "", "Search commands…", m.st)
	s.emptyTitle = "No commands"
	s.emptyDesc = "Every plugin is turned off in the config file (gt --config-path)"
	s.setItems(items)
	return s
}

// commandAction runs or opens a command from the root search, asking for its
// arguments first when it has any.
func commandAction(c kit.Command, ctx *commandContext) action {
	title := "Run Command"
	if c.Open != nil {
		title = "Open Command"
	}
	return action{title: title, run: func() tea.Cmd {
		if len(c.Arguments) > 0 {
			return func() tea.Msg { return openArgsMsg{cmd: c, ctx: ctx} }
		}
		return startCommand(c, ctx.withArgs(nil))
	}}
}

// startCommand runs a no-view command, or opens a view command's screen.
func startCommand(c kit.Command, ctx *commandContext) tea.Cmd {
	if c.Run != nil {
		return runPlugin("Running "+c.Title+"…", func() error { return c.Run(ctx) })
	}
	return func() tea.Msg {
		var v kit.View
		err := safely(func() error {
			var e error
			v, e = c.Open(ctx)
			return e
		})
		if err != nil {
			return doneMsg{err: err}
		}
		return pushMsg{view: v, ctx: ctx}
	}
}

// safely runs plugin code, turning a panic into an error so one broken
// plugin can't take down the UI.
func safely(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin panicked: %v\n%s", r, debug.Stack())
		}
	}()
	return f()
}

func (m Model) top() screen { return m.stack[len(m.stack)-1] }

// bodyHeight is the space left for a screen's body after the header, two
// dividers and the footer.
func (m Model) bodyHeight() int { return max(0, m.height-4) }

func (m Model) Init() tea.Cmd {
	if m.fixedTheme {
		return m.top().init()
	}
	return tea.Batch(tea.RequestBackgroundColor, m.top().init())
}

// push puts a screen on the stack, sized for the window.
func (m Model) push(s screen) Model {
	s.resize(m.width, m.bodyHeight())
	m.stack = append(m.stack, s)
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, s := range m.stack {
			s.resize(m.width, m.bodyHeight())
		}
		return m, nil

	case tea.BackgroundColorMsg:
		if m.fixedTheme {
			return m, nil
		}
		m.st = newStyles(msg.IsDark())
		for _, s := range m.stack {
			s.setStyles(m.st)
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.confirm != nil {
			return m.answer(msg.String())
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		if m.toast != nil && m.toast.style != kit.Loading {
			m.toast = nil
		}

	case busyMsg:
		m.busy = true
		if msg.text != "" {
			m.toast = &toast{style: kit.Loading, title: msg.text}
		}
		return m, nil

	case doneMsg:
		m.busy = false
		if m.toast != nil && m.toast.id == 0 && m.toast.style == kit.Loading {
			m.toast = nil
		}
		if msg.err != nil {
			m.toast = &toast{style: kit.Failure, title: firstLine(msg.err.Error())}
		}
		return m, nil

	case toastMsg:
		switch {
		case msg.id > m.lastToast:
			m.lastToast = msg.id
		case m.toast == nil || msg.id != m.toast.id:
			return m, nil // an update to a toast that has been replaced
		}
		m.toast = &toast{id: msg.id, style: msg.style, title: msg.title, message: msg.message}
		return m, nil

	case confirmMsg:
		m.confirm = &msg
		return m, nil

	case hudMsg:
		m.exitMessage = msg.text
		return m, tea.Quit

	case closeMsg:
		return m, tea.Quit

	case pushMsg:
		l, ok := msg.view.(*kit.List)
		if !ok {
			return m, nil
		}
		name := l.Title
		if name == "" {
			name = msg.ctx.title
		}
		s := newListScreen(m.nextID(), name, l.SearchPlaceholder, m.st)
		s.reload = loader(s.sid, l, msg.ctx)
		s.showDetail = l.ShowDetail
		if l.Filter != nil {
			s.filter, s.filterValue = l.Filter, l.Filter.Default
		}
		cmd := s.load()
		return m.push(s), cmd

	case refreshMsg:
		if s, ok := m.top().(*listScreen); ok && s.reload != nil {
			return m, s.load()
		}
		return m, nil

	case openFilterMsg:
		s := newFilterScreen(m.nextID(), msg.screen, msg.filter, msg.current, m.st)
		return m.push(s), nil

	case filterChosenMsg:
		for _, sc := range m.stack {
			if s, ok := sc.(*listScreen); ok && s.sid == msg.screen && s.reload != nil {
				s.filterValue = msg.value
				return m, s.load()
			}
		}
		return m, nil

	case contentMsg:
		for _, sc := range m.stack {
			s, ok := sc.(*listScreen)
			if !ok || s.sid != msg.screen {
				continue
			}
			s.isLoading = false
			if msg.err != nil {
				m.toast = &toast{style: kit.Failure, title: "Could not load: " + firstLine(msg.err.Error())}
				return m, nil
			}
			s.emptyTitle, s.emptyDesc, s.emptyActions = msg.empty.Title, msg.empty.Description, msg.emptyA
			s.setItems(msg.items)
			s.tickGen++
			if msg.refreshAfter > 0 {
				id, gen := s.sid, s.tickGen
				return m, tea.Tick(msg.refreshAfter, func(time.Time) tea.Msg { return tickMsg{screen: id, gen: gen} })
			}
		}
		return m, nil

	case tickMsg:
		for _, sc := range m.stack {
			if s, ok := sc.(*listScreen); ok && s.sid == msg.screen && s.gen() == msg.gen && s.reload != nil {
				return m, s.reload(s.filterValue) // quietly: no loading indicator
			}
		}
		return m, nil

	case execMsg:
		done := msg.done
		return m, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			done <- err
			return nil
		})

	// Build the screen first: m.push copies m when called, so an id taken
	// in its argument list would be lost.
	case openMenuMsg:
		s := newMenuScreen(m.nextID(), msg.title, msg.actions, m.st)
		return m.push(s), nil

	case openArgsMsg:
		s := m.argsScreen(msg.cmd, msg.ctx)
		return m.push(s), nil

	case popMsg:
		if len(m.stack) == 1 {
			return m, tea.Quit
		}
		m.stack = m.stack[:len(m.stack)-1]
		return m, nil
	}

	s, cmd := m.top().update(msg)
	m.stack[len(m.stack)-1] = s
	return m, cmd
}

// answer handles a key while the confirmation dialog is open.
func (m Model) answer(key string) (tea.Model, tea.Cmd) {
	var yes bool
	switch key {
	case "enter", "y":
		yes = true
	case "esc", "n":
	case "ctrl+c":
		m.confirm.reply <- false
		m.confirm = nil
		return m, tea.Quit
	default:
		return m, nil
	}
	m.confirm.reply <- yes
	m.confirm = nil
	return m, nil
}

func (m *Model) argsScreen(c kit.Command, ctx *commandContext) screen {
	s := &argsScreen{sid: m.nextID(), name: c.Title, verb: commandAction(c, ctx).title}
	for _, a := range c.Arguments {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = a.Placeholder
		if in.Placeholder == "" {
			in.Placeholder = a.Name
		}
		in.SetStyles(m.st.inputStyles())
		s.inputs = append(s.inputs, in)
		s.names = append(s.names, a.Name)
		s.labels = append(s.labels, in.Placeholder)
		s.need = append(s.need, a.Required)
	}
	s.inputs[0].Focus()
	s.submit = func(values map[string]string) tea.Cmd { return startCommand(c, ctx.withArgs(values)) }
	return s
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	s := m.top()
	divider := m.st.divider.Render(strings.Repeat("─", m.width))

	brand := m.st.brand.Render(" goto ")
	if t := s.title(); t != "" {
		brand += m.st.subtitle.Render(ansi.Truncate("› "+t, max(0, m.width/2), "…") + " ")
	}
	header := brand + " " + s.header(m.width-ansi.StringWidth(brand)-1)

	parts := []string{header, divider}
	if body := m.bodyHeight(); body > 0 {
		if m.confirm != nil {
			parts = append(parts, m.renderConfirm(body))
		} else {
			parts = append(parts, s.body(m.width, m.st))
		}
	}
	parts = append(parts, divider, m.footer(s))
	return strings.Join(parts, "\n")
}

// renderConfirm draws the confirmation dialog in place of the body.
func (m Model) renderConfirm(height int) string {
	c := m.confirm.c
	lines := []string{"", m.st.modalTitle.Render("  " + c.Title), ""}
	for _, l := range strings.Split(c.Message, "\n") {
		if l != "" {
			lines = append(lines, m.st.subtitle.Render("  "+l))
		}
	}
	verb := c.Action
	if verb == "" {
		verb = "Confirm"
	}
	verbStyle := m.st.footerKey
	if c.Destructive {
		verbStyle = m.st.statusError.Bold(true)
	}
	lines = append(lines, "",
		"  "+m.st.footerKey.Render("enter")+" "+verbStyle.Render(verb)+"     "+
			m.st.footerKey.Render("esc")+" "+m.st.footerLabel.Render("Cancel"))

	out := make([]string, height)
	for i := range out {
		if i < len(lines) {
			out[i] = ansi.Truncate(lines[i], m.width, "…")
		}
	}
	return strings.Join(out, "\n")
}

// footer shows the toast (or loading state) on the left and key hints on the
// right.
func (m Model) footer(s screen) string {
	var r string
	if !m.busy && m.confirm == nil {
		esc := "Back"
		switch {
		case s.query() != "":
			esc = "Clear"
		case len(m.stack) == 1:
			esc = "Quit"
		}
		var parts []string
		for _, h := range append(s.hints(), hint{key: "esc", label: esc}) {
			parts = append(parts, m.st.footerKey.Render(h.key)+" "+m.st.footerLabel.Render(h.label))
		}
		r = strings.Join(parts, "   ") + " "
	}

	leftWidth := max(0, m.width-ansi.StringWidth(r)-2)
	var left string
	switch {
	case m.toast != nil:
		left = m.renderToast(*m.toast, leftWidth)
	case s.loading():
		left = m.st.status.Render(ansi.Truncate(" Loading…", leftWidth, "…"))
	}
	pad := max(0, m.width-ansi.StringWidth(left)-ansi.StringWidth(r))
	return left + strings.Repeat(" ", pad) + r
}

func (m Model) renderToast(t toast, width int) string {
	text := t.title
	if t.message != "" {
		text += " — " + firstLine(t.message)
		if n := strings.Count(strings.TrimSpace(t.message), "\n"); n > 0 {
			text += fmt.Sprintf(" (+%d more)", n)
		}
	}
	style, icon := m.st.status, "… "
	switch t.style {
	case kit.Success:
		style, icon = m.st.success, "✓ "
	case kit.Failure:
		style, icon = m.st.statusError, "✗ "
	}
	return style.Render(ansi.Truncate(" "+icon+text, width, "…"))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
