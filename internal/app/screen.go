package app

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// screen is one level of the navigation stack. The Model draws the shared
// chrome (header brand, dividers, footer) and asks the top screen for the rest.
type screen interface {
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

	resize(width, bodyHeight int)
	setStyles(st styles)
}

type hint struct{ key, label string }

// action is something the user can do with a list item. The first action of
// an item is its primary action, run by Enter.
type action struct {
	title string
	run   func() tea.Cmd
}

type listItem struct {
	row     listRow
	actions []action
}

// listScreen is a search box over a fuzzy-filtered list of items.
type listScreen struct {
	name      string
	input     textinput.Model
	list      listView
	items     []listItem
	emptyText string
}

func newListScreen(name, placeholder, emptyText string, items []listItem, st styles) *listScreen {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.SetStyles(textinput.DefaultStyles(st.isDark))
	in.Focus()

	rows := make([]listRow, len(items))
	for i, it := range items {
		rows[i] = it.row
	}
	return &listScreen{
		name:      name,
		input:     in,
		list:      newListView(rows),
		items:     items,
		emptyText: emptyText,
	}
}

func (s *listScreen) init() tea.Cmd { return textinput.Blink }

func (s *listScreen) title() string { return s.name }

func (s *listScreen) query() string { return s.input.Value() }

func (s *listScreen) header(width int) string {
	s.input.SetWidth(max(1, width-1))
	return s.input.View()
}

func (s *listScreen) body(width int, st styles) string {
	return s.list.render(width, st, s.emptyText)
}

func (s *listScreen) hints() []hint {
	if i := s.list.selected(); i >= 0 && len(s.items[i].actions) > 0 {
		return []hint{{key: "↵", label: s.items[i].actions[0].title}}
	}
	return nil
}

func (s *listScreen) resize(_, bodyHeight int) { s.list.setHeight(bodyHeight) }

func (s *listScreen) setStyles(st styles) { s.input.SetStyles(textinput.DefaultStyles(st.isDark)) }

func (s *listScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
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
			if i := s.list.selected(); i >= 0 && len(s.items[i].actions) > 0 {
				return s, s.items[i].actions[0].run()
			}
			return s, nil
		case "esc":
			if s.input.Value() != "" {
				s.input.Reset()
				s.list.setQuery("")
				return s, nil
			}
			return s, func() tea.Msg { return popMsg{} }
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
