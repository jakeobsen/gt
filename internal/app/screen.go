package app

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jakeobsen/gt/kit"
)

// screen is one level of the navigation stack. The Model draws the shared
// chrome (header brand, dividers, footer) and asks the top screen for the rest.
type screen interface {
	id() int
	init() tea.Cmd
	update(msg tea.Msg) (screen, tea.Cmd)

	// title is shown after the brand in the header; empty for the root.
	title() string
	// header draws the rest of the header line (usually the search box).
	header(width int) string
	// body draws the screen's main area at the size given to resize.
	body(width int, st styles) string
	// query is the current search text, which decides what esc does.
	query() string
	// hints are the key hints for the footer, before the esc hint.
	hints() []hint
	// loading reports that the screen is waiting for its contents.
	loading() bool

	resize(width, bodyHeight int)
	setStyles(st styles)
}

type hint struct{ key, label string }

// action is something the user can do with a list item. The first action
// that isn't a divider is the item's primary action, run by Enter.
type action struct {
	title       string
	shortcut    string
	destructive bool
	divider     bool
	run         func() tea.Cmd
}

func primary(actions []action) *action {
	for i := range actions {
		if !actions[i].divider {
			return &actions[i]
		}
	}
	return nil
}

func countActions(actions []action) int {
	n := 0
	for _, a := range actions {
		if !a.divider {
			n++
		}
	}
	return n
}

type listItem struct {
	row     listRow
	actions []action
	detail  string
}

func popCmd() tea.Msg { return popMsg{} }

// listScreen is a search box over a fuzzy-filtered list of items. Plugin
// lists load their items asynchronously through reload.
type listScreen struct {
	sid   int
	name  string
	input textinput.Model
	list  listView
	items []listItem

	emptyTitle   string
	emptyDesc    string
	emptyActions []action

	isLoading bool
	// reload starts loading the contents again with a filter value; nil for
	// fixed lists.
	reload func(filter string) tea.Cmd
	// filter is the list's dropdown, if it has one, and filterValue its choice.
	filter      *kit.Filter
	filterValue string
	// hideMenu stops ctrl+k opening an actions menu (used by the menu itself).
	hideMenu bool
	// showDetail splits the body: list left, the selected item's detail right.
	showDetail bool
	// tickGen numbers scheduled quiet refreshes; only the latest one runs.
	tickGen int
	width   int
}

func newListScreen(sid int, name, placeholder string, st styles) *listScreen {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.SetStyles(st.inputStyles())
	in.Focus()
	return &listScreen{sid: sid, name: name, input: in, list: newListView(nil)}
}

func (s *listScreen) setItems(items []listItem) {
	s.items = items
	rows := make([]listRow, len(items))
	for i, it := range items {
		rows[i] = it.row
	}
	s.list.setRows(rows)
}

func (s *listScreen) id() int             { return s.sid }
func (s *listScreen) init() tea.Cmd       { return nil }
func (s *listScreen) title() string       { return s.name }
func (s *listScreen) query() string       { return s.input.Value() }
func (s *listScreen) loading() bool       { return s.isLoading }
func (s *listScreen) setStyles(st styles) { s.input.SetStyles(st.inputStyles()) }

func (s *listScreen) resize(width, bodyHeight int) {
	s.width = width
	s.list.setHeight(bodyHeight)
}

func (s *listScreen) header(width int) string {
	label := ""
	if s.filter != nil {
		label = "[" + s.filterTitle() + " ▾] "
		if ansi.StringWidth(label) > width/2 {
			label = ansi.Truncate(label, width/2, "…")
		}
	}
	s.input.SetWidth(max(1, width-1-ansi.StringWidth(label)))
	in := s.input.View()
	pad := max(0, width-ansi.StringWidth(in)-ansi.StringWidth(label))
	return in + strings.Repeat(" ", pad) + label
}

// filterTitle is the title of the chosen filter option.
func (s *listScreen) filterTitle() string {
	for _, o := range s.filter.Options {
		if o.Value == s.filterValue {
			return o.Title
		}
	}
	return s.filterValue
}

// load starts loading the list with its current filter value.
func (s *listScreen) load() tea.Cmd {
	s.isLoading = true
	return s.reload(s.filterValue)
}

func (s *listScreen) body(width int, st styles) string {
	var empty []string
	switch {
	case len(s.items) == 0 && s.isLoading:
		empty = []string{"", st.empty.Render("  Loading…")}
	case len(s.items) == 0:
		empty = []string{"", st.emptyTitle.Render(ansi.Truncate("  "+s.emptyTitle, width, "…"))}
		if s.emptyDesc != "" {
			empty = append(empty, st.empty.Render(ansi.Truncate("  "+s.emptyDesc, width, "…")))
		}
	default:
		empty = []string{"", st.empty.Render("  No results")}
	}
	i := s.list.selected()
	if !s.showDetail || i < 0 || width < 50 {
		return s.list.render(width, st, empty)
	}
	// List on the left, the selected item's detail on the right.
	left := max(24, width*2/5)
	right := width - left - 3
	lines := strings.Split(s.list.render(left, st, empty), "\n")
	detail := renderDetail(s.items[i].detail, right, st)
	sep := st.divider.Render(" │ ")
	for n := range lines {
		d := ""
		if n < len(detail) {
			d = detail[n]
		}
		lines[n] = lines[n] + strings.Repeat(" ", max(0, left-ansi.StringWidth(lines[n]))) + sep + d
	}
	return strings.Join(lines, "\n")
}

// current returns the actions for what is on screen: the selected item's, or
// the empty view's while the list has no items at all.
func (s *listScreen) current() []action {
	if i := s.list.selected(); i >= 0 {
		return s.items[i].actions
	}
	if len(s.items) == 0 && !s.isLoading {
		return s.emptyActions
	}
	return nil
}

func (s *listScreen) hints() []hint {
	acts := s.current()
	var hs []hint
	if a := primary(acts); a != nil {
		hs = append(hs, hint{key: "↵", label: a.title})
	}
	if !s.hideMenu && countActions(acts) > 1 {
		hs = append(hs, hint{key: "ctrl+k", label: "Actions"})
	}
	if s.filter != nil {
		hs = append(hs, hint{key: "tab", label: s.filter.Title})
	}
	return hs
}

func (s *listScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		k := key.String()
		switch k {
		case "up", "ctrl+p":
			s.list.move(-1)
			return s, nil
		case "down", "ctrl+n":
			s.list.move(1)
			return s, nil
		case "pgup":
			s.list.move(-max(1, s.list.height-1))
			return s, nil
		case "pgdown":
			s.list.move(max(1, s.list.height-1))
			return s, nil
		case "enter":
			if a := primary(s.current()); a != nil {
				return s, a.run()
			}
			return s, nil
		case "ctrl+k":
			acts := s.current()
			if s.hideMenu || countActions(acts) == 0 {
				return s, nil
			}
			title := "Actions"
			if i := s.list.selected(); i >= 0 {
				title = s.items[i].row.title
			}
			return s, func() tea.Msg { return openMenuMsg{title: title, actions: acts} }
		case "esc":
			if s.input.Value() != "" {
				s.input.Reset()
				s.list.setQuery("")
				return s, nil
			}
			return s, popCmd
		case "tab":
			if s.filter != nil {
				return s, func() tea.Msg { return openFilterMsg{screen: s.sid, filter: s.filter, current: s.filterValue} }
			}
			return s, nil
		}
		for _, a := range s.current() {
			if !a.divider && a.shortcut != "" && a.shortcut == k {
				return s, a.run()
			}
		}
	}

	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if v := s.input.Value(); v != before {
		s.list.setQuery(v)
	}
	return s, cmd
}

// newMenuScreen lists actions for ctrl+k. Choosing one closes the menu and
// runs the action on the screen underneath, so a Refresh or Push it does
// lands where the user expects.
func newMenuScreen(sid int, title string, actions []action, st styles) *listScreen {
	s := newListScreen(sid, title, "Search actions…", st)
	s.hideMenu = true
	var items []listItem
	group := 0
	for _, a := range actions {
		if a.divider {
			group++
			continue
		}
		a := a
		row := listRow{group: group, title: a.title, destructive: a.destructive}
		if a.shortcut != "" {
			row.accessories = []accessory{{text: a.shortcut}}
		}
		items = append(items, listItem{row: row, actions: []action{{
			title: "Run Action",
			run:   func() tea.Cmd { return tea.Sequence(popCmd, a.run()) },
		}}})
	}
	s.setItems(items)
	return s
}

// newFilterScreen lets the user pick a list's filter value.
func newFilterScreen(sid, target int, f *kit.Filter, current string, st styles) *listScreen {
	s := newListScreen(sid, f.Title, "Search "+strings.ToLower(f.Title)+"…", st)
	s.hideMenu = true
	var items []listItem
	for _, o := range f.Options {
		value := o.Value
		row := listRow{title: o.Title}
		if value == current {
			row.accessories = []accessory{{text: "✓", color: kit.Green}}
		}
		items = append(items, listItem{row: row, actions: []action{{
			title: "Choose",
			run: func() tea.Cmd {
				return tea.Sequence(popCmd, func() tea.Msg { return filterChosenMsg{screen: target, value: value} })
			},
		}}})
	}
	s.setItems(items)
	return s
}

// argsScreen asks for a command's arguments before it runs.
type argsScreen struct {
	sid     int
	name    string
	verb    string
	labels  []string
	need    []bool
	inputs  []textinput.Model
	focus   int
	problem string
	height  int
	submit  func(values map[string]string) tea.Cmd
	names   []string
}

func (s *argsScreen) id() int         { return s.sid }
func (s *argsScreen) init() tea.Cmd   { return nil }
func (s *argsScreen) title() string   { return s.name }
func (s *argsScreen) query() string   { return "" }
func (s *argsScreen) loading() bool   { return false }
func (s *argsScreen) resize(_, h int) { s.height = h }

func (s *argsScreen) setStyles(st styles) {
	for i := range s.inputs {
		s.inputs[i].SetStyles(st.inputStyles())
	}
}

func (s *argsScreen) header(width int) string {
	in := &s.inputs[s.focus]
	in.SetWidth(max(1, width-1))
	return in.View()
}

func (s *argsScreen) body(width int, st styles) string {
	out := make([]string, max(s.height, 0))
	lines := []string{""}
	for i, label := range s.labels {
		marker := "  "
		if i == s.focus {
			marker = "› "
		}
		val := s.inputs[i].Value()
		if val == "" {
			val = st.empty.Render("(empty)")
		}
		req := ""
		if s.need[i] {
			req = st.statusError.Render(" *")
		}
		lines = append(lines, st.marker.Render(marker)+st.subtitle.Render(label)+req+"  "+val)
	}
	if s.problem != "" {
		lines = append(lines, "", st.statusError.Render("  "+s.problem))
	}
	for i := range out {
		if i < len(lines) {
			out[i] = ansi.Truncate(lines[i], width, "…")
		}
	}
	return strings.Join(out, "\n")
}

func (s *argsScreen) hints() []hint {
	hs := []hint{{key: "↵", label: s.verb}}
	if len(s.inputs) > 1 {
		hs = append(hs, hint{key: "tab", label: "Next"})
	}
	return hs
}

func (s *argsScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return s, popCmd
		case "tab", "down":
			s.setFocus((s.focus + 1) % len(s.inputs))
			return s, nil
		case "shift+tab", "up":
			s.setFocus((s.focus + len(s.inputs) - 1) % len(s.inputs))
			return s, nil
		case "enter":
			values := map[string]string{}
			for i, in := range s.inputs {
				v := strings.TrimSpace(in.Value())
				if s.need[i] && v == "" {
					s.problem = s.labels[i] + " is required"
					s.setFocus(i)
					return s, nil
				}
				values[s.names[i]] = v
			}
			return s, tea.Sequence(popCmd, s.submit(values))
		}
	}
	var cmd tea.Cmd
	s.inputs[s.focus], cmd = s.inputs[s.focus].Update(msg)
	s.problem = ""
	return s, cmd
}

func (s *argsScreen) setFocus(i int) {
	s.inputs[s.focus].Blur()
	s.focus = i
	s.inputs[s.focus].Focus()
}

func (s *listScreen) gen() int { return s.tickGen }

// renderDetail lays out an item's detail text for the side panel: wrapped
// to width, with "## " headings and **bold** in bold and ``` blocks dimmed.
func renderDetail(text string, width int, st styles) []string {
	if width < 10 || strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	code := false
	for _, para := range strings.Split(strings.TrimSpace(text), "\n") {
		switch {
		case strings.HasPrefix(para, "```"):
			code = !code
			continue
		case code:
			out = append(out, st.subtitle.Render(ansi.Truncate(para, width, "…")))
			continue
		case strings.HasPrefix(para, "## "):
			for _, l := range wrap(strings.TrimPrefix(para, "## "), width) {
				out = append(out, st.modalTitle.Render(l))
			}
			continue
		case strings.TrimSpace(para) == "":
			out = append(out, "")
			continue
		}
		for _, l := range wrap(para, width) {
			out = append(out, boldSpans(l, st))
		}
	}
	return out
}

// wrap breaks text into lines of at most width columns at spaces.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case ansi.StringWidth(line)+1+ansi.StringWidth(w) <= width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
		for ansi.StringWidth(line) > width { // a single word wider than the panel
			lines = append(lines, ansi.Truncate(line, width, ""))
			line = ansi.TruncateLeft(line, width, "")
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// boldSpans renders **bold** spans; the markers themselves are dropped.
func boldSpans(line string, st styles) string {
	parts := strings.Split(line, "**")
	if len(parts) < 3 {
		return st.title.Render(line)
	}
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 1 {
			b.WriteString(st.modalTitle.Render(p))
		} else {
			b.WriteString(st.title.Render(p))
		}
	}
	return b.String()
}
