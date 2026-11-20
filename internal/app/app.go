// Package app is goto's terminal UI: the root search, the screens plugins
// open, and the bridge that lets plugin code talk back to the UI.
package app

import (
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

// Run shows the goto UI until the user quits or a command closes it. If a
// command closed it with a message (kit.Context.HUD), that message is printed
// to stdout once the UI is gone.
func Run(plugins []kit.Plugin, cfg *config.Config) error {
	b := &bridge{}
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

// Messages the UI sends itself.
type (
	// popMsg asks to close the top screen (quitting when it is the root).
	popMsg struct{}
	// busyMsg marks a command as running, and blocks input until it finishes.
	busyMsg struct{ text string }
	// doneMsg reports that a command finished, with its error if it failed.
	doneMsg struct{ err error }
	// hudMsg closes goto and prints text afterwards.
	hudMsg struct{ text string }
)

// bridge implements kit.Context by turning calls from plugin goroutines into
// messages for the UI loop.
type bridge struct {
	send func(tea.Msg)
}

func (b *bridge) HUD(text string) { b.send(hudMsg{text: text}) }

// commandContext is the kit.Context a command runs with: the shared bridge
// plus its own plugin's settings.
type commandContext struct {
	*bridge
	settings kit.Settings
}

func (c commandContext) Settings() kit.Settings { return c.settings }

// Model is the whole UI: a stack of screens with a shared header, status line
// and footer.
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

	status      string
	statusError bool
	busy        bool
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

// rootScreen lists every command from every enabled plugin, alphabetically.
func (m Model) rootScreen(plugins []kit.Plugin) screen {
	var items []listItem
	for _, p := range plugins {
		if !m.cfg.Enabled(p.Name) {
			continue
		}
		ctx := commandContext{bridge: m.bridge, settings: m.cfg.Plugin(p.Name)}
		for _, c := range p.Commands {
			items = append(items, listItem{
				row: listRow{
					title:    c.Title,
					subtitle: p.Title,
					keywords: c.Keywords,
				},
				actions: []action{{title: "Run Command", run: runCommand(c, ctx)}},
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
	empty := "No matching commands"
	if len(items) == 0 {
		empty = "No commands: every plugin is turned off in the config file (gt --config-path)"
	}
	return newListScreen("", "Search commands…", empty, items, m.st)
}

// runCommand returns an action that runs a no-view command off the UI loop.
func runCommand(c kit.Command, ctx kit.Context) func() tea.Cmd {
	return func() tea.Cmd {
		return tea.Sequence(
			func() tea.Msg { return busyMsg{text: "Running " + c.Title + "…"} },
			func() tea.Msg { return doneMsg{err: safely(func() error { return c.Run(ctx) })} },
		)
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
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		m.status, m.statusError = "", false

	case busyMsg:
		m.busy = true
		m.status, m.statusError = msg.text, false
		return m, nil

	case doneMsg:
		m.busy = false
		m.status, m.statusError = "", false
		if msg.err != nil {
			m.status, m.statusError = firstLine(msg.err.Error()), true
		}
		return m, nil

	case hudMsg:
		m.exitMessage = msg.text
		return m, tea.Quit

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
		brand += m.st.subtitle.Render("› " + t + " ")
	}
	header := brand + " " + s.header(m.width-ansi.StringWidth(brand)-1)

	parts := []string{header, divider}
	if body := m.bodyHeight(); body > 0 {
		parts = append(parts, s.body(m.width, m.st))
	}
	parts = append(parts, divider, m.footer(s))
	return strings.Join(parts, "\n")
}

// footer shows the status (or error) on the left and key hints on the right.
func (m Model) footer(s screen) string {
	esc := "Back"
	switch {
	case s.query() != "":
		esc = "Clear"
	case len(m.stack) == 1:
		esc = "Quit"
	}
	hints := append(s.hints(), hint{key: "esc", label: esc})

	var right []string
	for _, h := range hints {
		right = append(right, m.st.footerKey.Render(h.key)+" "+m.st.footerLabel.Render(h.label))
	}
	r := strings.Join(right, "   ") + " "
	if m.busy {
		r = ""
	}

	leftWidth := max(0, m.width-ansi.StringWidth(r)-2)
	style := m.st.status
	if m.statusError {
		style = m.st.statusError
	}
	left := style.Render(ansi.Truncate(" "+m.status, leftWidth, "…"))
	pad := max(0, m.width-ansi.StringWidth(left)-ansi.StringWidth(r))
	return left + strings.Repeat(" ", pad) + r
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
