package app

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// listRow is one selectable row in a list.
type listRow struct {
	section   string
	title     string
	subtitle  string
	accessory string
	keywords  []string
}

// searchText is what the fuzzy matcher sees for a row. The title comes first,
// so match positions below len(title) can be highlighted in the title.
func (r listRow) searchText() string {
	parts := append([]string{r.title, r.subtitle}, r.keywords...)
	return strings.Join(parts, " ")
}

// shownRow is a row that survived filtering, with the title characters (rune
// positions) that matched the query.
type shownRow struct {
	row     int
	matched []int
}

// listView is a fuzzy-filtered, scrollable list of rows with optional section
// headers. It owns no text input: the screen feeds it the query.
type listView struct {
	rows   []listRow
	query  string
	shown  []shownRow
	cursor int // index into shown
	offset int // first visible line
	height int // lines available for the list
}

func newListView(rows []listRow) listView {
	l := listView{rows: rows}
	l.setQuery("")
	return l
}

// setQuery refilters the rows. An empty query shows every row in its original
// order with section headers; otherwise rows are ranked by match quality and
// sections are dropped, so the best match is always on top.
func (l *listView) setQuery(q string) {
	l.query = q
	l.cursor, l.offset = 0, 0
	l.shown = l.shown[:0]
	if strings.TrimSpace(q) == "" {
		for i := range l.rows {
			l.shown = append(l.shown, shownRow{row: i})
		}
		return
	}
	texts := make([]string, len(l.rows))
	for i, r := range l.rows {
		texts[i] = r.searchText()
	}
	for _, m := range fuzzy.Find(q, texts) {
		title := l.rows[m.Index].title
		l.shown = append(l.shown, shownRow{row: m.Index, matched: titleRunes(title, m.MatchedIndexes)})
	}
}

// titleRunes converts the matcher's byte offsets into rune positions, keeping
// only those that fall inside the title.
func titleRunes(title string, byteIdx []int) []int {
	var out []int
	for _, b := range byteIdx {
		if b < len(title) {
			out = append(out, utf8.RuneCountInString(title[:b]))
		}
	}
	return out
}

// selected returns the index into rows of the selected row, or -1 if nothing
// is shown.
func (l listView) selected() int {
	if len(l.shown) == 0 {
		return -1
	}
	return l.shown[l.cursor].row
}

// move shifts the selection by delta rows, stopping at either end.
func (l *listView) move(delta int) {
	if len(l.shown) == 0 {
		return
	}
	l.cursor = max(0, min(len(l.shown)-1, l.cursor+delta))
	l.scrollToCursor()
}

func (l *listView) setHeight(h int) {
	l.height = max(0, h)
	l.scrollToCursor()
}

// line is one rendered line of the list: a section header or a row.
type line struct {
	header string
	shown  int // index into shown; -1 for a header
}

func (l listView) lines() []line {
	var out []line
	lastSection := ""
	sections := l.query == ""
	for i, s := range l.shown {
		if sec := l.rows[s.row].section; sections && sec != "" && sec != lastSection {
			out = append(out, line{header: sec, shown: -1})
			lastSection = sec
		}
		out = append(out, line{shown: i})
	}
	return out
}

// scrollToCursor adjusts the scroll offset so the selected row is visible,
// along with its section header when the row is the first in its section.
func (l *listView) scrollToCursor() {
	lines := l.lines()
	if l.height <= 0 || len(lines) == 0 {
		l.offset = 0
		return
	}
	at := 0
	for i, ln := range lines {
		if ln.shown == l.cursor {
			at = i
			break
		}
	}
	top := at
	if at > 0 && lines[at-1].shown == -1 {
		top = at - 1
	}
	if top < l.offset {
		l.offset = top
	}
	if at >= l.offset+l.height {
		l.offset = at - l.height + 1
	}
	l.offset = max(0, min(l.offset, len(lines)-l.height))
}

// render draws the visible part of the list, exactly height lines tall.
func (l listView) render(width int, st styles, emptyText string) string {
	if l.height <= 0 {
		return ""
	}
	var b strings.Builder
	lines := l.lines()
	if len(lines) == 0 {
		b.WriteString(st.empty.Render(ansi.Truncate("  "+emptyText, width, "…")))
		for i := 1; i < l.height; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}
	// Line subtitles up in a column after the widest title (across all rows,
	// so the column doesn't jump while filtering), using at most half the width.
	titleCol := 0
	for _, r := range l.rows {
		titleCol = max(titleCol, ansi.StringWidth(r.title))
	}
	titleCol = min(titleCol, (width-3)/2)

	end := min(len(lines), l.offset+l.height)
	for i := l.offset; i < l.offset+l.height; i++ {
		if i > l.offset {
			b.WriteString("\n")
		}
		if i >= end {
			continue
		}
		ln := lines[i]
		if ln.shown == -1 {
			b.WriteString(st.section.Render(ansi.Truncate("  "+ln.header, width, "…")))
			continue
		}
		s := l.shown[ln.shown]
		b.WriteString(renderRow(l.rows[s.row], s.matched, ln.shown == l.cursor, width, titleCol, st))
	}
	return b.String()
}

// renderRow draws one row: marker, title, subtitle (starting at column
// titleCol when there's room), and an accessory pushed to the right edge.
// When space runs out the subtitle is cut first, then the title.
func renderRow(r listRow, matched []int, sel bool, width, titleCol int, st styles) string {
	const gap = 2
	marker := "  "
	if sel {
		marker = "› "
	}
	avail := width - ansi.StringWidth(marker) - 1 // keep one column free at the right edge

	accessory := r.accessory
	if ansi.StringWidth(accessory) > avail/3 {
		accessory = ansi.Truncate(accessory, avail/3, "…")
	}
	if accessory != "" {
		avail -= ansi.StringWidth(accessory) + gap
	}

	title := ansi.Truncate(r.title, max(avail, 1), "…")
	avail -= ansi.StringWidth(title)
	subtitle := ""
	colPad := 0
	if r.subtitle != "" {
		colPad = max(0, titleCol-ansi.StringWidth(title))
		if avail-colPad <= gap+1 {
			colPad = 0 // too tight to align; keep what room there is for the subtitle
		}
		if avail-colPad > gap+1 {
			subtitle = ansi.Truncate(r.subtitle, avail-colPad-gap, "…")
			avail -= colPad + gap + ansi.StringWidth(subtitle)
		}
	}

	bg := func(s lipgloss.Style) lipgloss.Style { return st.onSelected(s, sel) }
	titleStyle := bg(st.title)
	if sel {
		titleStyle = titleStyle.Bold(true)
	}
	var b strings.Builder
	b.WriteString(bg(st.marker).Render(marker))
	b.WriteString(lipgloss.StyleRunes(title, visibleMatches(title, matched), bg(st.match), titleStyle))
	if subtitle != "" {
		b.WriteString(bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", colPad+gap)))
		b.WriteString(bg(st.subtitle).Render(subtitle))
	}
	pad := max(avail, 0)
	if accessory != "" {
		pad += gap
	}
	b.WriteString(bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", pad)))
	if accessory != "" {
		b.WriteString(bg(st.accessory).Render(accessory))
	}
	b.WriteString(bg(lipgloss.NewStyle()).Render(" "))
	return b.String()
}

// visibleMatches drops match positions that fell off a truncated title.
func visibleMatches(title string, matched []int) []int {
	n := utf8.RuneCountInString(title)
	if strings.HasSuffix(title, "…") {
		n-- // never highlight the ellipsis
	}
	out := matched[:0:0]
	for _, i := range matched {
		if i < n {
			out = append(out, i)
		}
	}
	return out
}
