package app

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/jakeobsen/gt/kit"
)

// accessory is a short coloured tag at the right edge of a row.
type accessory struct {
	text  string
	color kit.Color
}

// listRow is one selectable row in a list.
type listRow struct {
	// group numbers the section the row is in. With an empty search, a new
	// group starts with its header, or a divider when it has no header.
	group     int
	header    string
	headerSub string

	title       string
	subtitle    string
	accessories []accessory
	keywords    []string
	destructive bool
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

// listView is a fuzzy-filtered, scrollable list of rows with section headers.
// It owns no text input: the screen feeds it the query.
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

// setRows replaces the rows, keeping the query and, when it is still there,
// the selected row.
func (l *listView) setRows(rows []listRow) {
	prev := ""
	if i := l.selected(); i >= 0 {
		prev = l.rows[i].title
	}
	l.rows = rows
	l.setQuery(l.query)
	for i, s := range l.shown {
		if l.rows[s.row].title == prev {
			l.cursor = i
			break
		}
	}
	l.scrollToCursor()
}

// setQuery refilters the rows. An empty query shows every row in its original
// order with section headers; otherwise rows are ranked by match quality and
// sections are dropped, so the best match is always on top.
func (l *listView) setQuery(q string) {
	l.query = q
	l.cursor, l.offset = 0, 0
	l.shown = l.shown[:0]
	if l.grouped() {
		for i := range l.rows {
			l.shown = append(l.shown, shownRow{row: i})
		}
		return
	}
	texts := make([]string, len(l.rows))
	for i, r := range l.rows {
		texts[i] = r.searchText()
	}
	// The same item can sit in several sections (a project with two tags);
	// search results drop the section headers, so show it once.
	seen := map[string]bool{}
	for _, m := range fuzzy.Find(q, texts) {
		r := l.rows[m.Index]
		if key := r.title + "\x00" + r.subtitle; seen[key] {
			continue
		} else {
			seen[key] = true
		}
		l.shown = append(l.shown, shownRow{row: m.Index, matched: titleRunes(r.title, m.MatchedIndexes)})
	}
}

func (l listView) grouped() bool { return strings.TrimSpace(l.query) == "" }

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

type lineKind int

const (
	rowLine lineKind = iota
	headerLine
	dividerLine
)

// line is one rendered line of the list.
type line struct {
	kind  lineKind
	text  string // header title
	sub   string // header subtitle
	shown int    // index into shown, for row lines
}

func (l listView) lines() []line {
	var out []line
	grouped := l.grouped()
	for i, s := range l.shown {
		r := l.rows[s.row]
		if grouped && (i == 0 || r.group != l.rows[l.shown[i-1].row].group) {
			switch {
			case r.header != "":
				out = append(out, line{kind: headerLine, text: r.header, sub: r.headerSub})
			case i > 0:
				out = append(out, line{kind: dividerLine})
			}
		}
		out = append(out, line{kind: rowLine, shown: i})
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
		if ln.kind == rowLine && ln.shown == l.cursor {
			at = i
			break
		}
	}
	top := at
	if at > 0 && lines[at-1].kind != rowLine {
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

// render draws the visible part of the list, exactly height lines tall. When
// nothing is shown it draws the empty lines instead.
func (l listView) render(width int, st styles, empty []string) string {
	if l.height <= 0 {
		return ""
	}
	out := make([]string, l.height)
	lines := l.lines()
	if len(lines) == 0 {
		for i, e := range empty {
			if i < len(out) {
				out[i] = e
			}
		}
		return strings.Join(out, "\n")
	}

	// Line subtitles up in a column after the widest title (across all rows,
	// so the column doesn't jump while filtering), using at most half the width.
	titleCol := 0
	for _, r := range l.rows {
		titleCol = max(titleCol, ansi.StringWidth(r.title))
	}
	titleCol = min(titleCol, (width-3)/2)

	for i := 0; i < l.height && l.offset+i < len(lines); i++ {
		ln := lines[l.offset+i]
		switch ln.kind {
		case headerLine:
			h := "  " + ln.text
			text := st.section.Render(ansi.Truncate(h, width, "…"))
			if ln.sub != "" && ansi.StringWidth(h)+2 < width {
				text += st.subtitle.Render(ansi.Truncate("  "+ln.sub, width-ansi.StringWidth(h), "…"))
			}
			out[i] = text
		case dividerLine:
			out[i] = st.divider.Render("  " + strings.Repeat("─", max(0, width-4)))
		default:
			s := l.shown[ln.shown]
			out[i] = renderRow(l.rows[s.row], s.matched, ln.shown == l.cursor, width, titleCol, st)
		}
	}
	return strings.Join(out, "\n")
}

// renderRow draws one row: marker, title, subtitle (starting at column
// titleCol when there's room), and accessories pushed to the right edge.
// When space runs out, accessories drop off first, then the subtitle is cut,
// then the title.
func renderRow(r listRow, matched []int, sel bool, width, titleCol int, st styles) string {
	const gap = 2
	marker := "  "
	if sel {
		marker = "› "
	}
	avail := width - ansi.StringWidth(marker) - 1 // keep one column free at the right edge

	accs := fitAccessories(r.accessories, max(0, avail/2))
	accWidth := 0
	for i, a := range accs {
		if i > 0 {
			accWidth += gap
		}
		accWidth += ansi.StringWidth(a.text)
	}
	if accWidth > 0 {
		avail -= accWidth + gap
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
	space := func(n int) string { return bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(n, 0))) }
	titleStyle := bg(st.title)
	if r.destructive {
		titleStyle = bg(st.statusError)
	}
	if sel {
		titleStyle = titleStyle.Bold(true)
	}

	var b strings.Builder
	b.WriteString(bg(st.marker).Render(marker))
	b.WriteString(lipgloss.StyleRunes(title, visibleMatches(title, matched), bg(st.match), titleStyle))
	if subtitle != "" {
		b.WriteString(space(colPad + gap))
		b.WriteString(bg(st.subtitle).Render(subtitle))
	}
	pad := max(avail, 0)
	if accWidth > 0 {
		pad += gap
	}
	b.WriteString(space(pad))
	for i, a := range accs {
		if i > 0 {
			b.WriteString(space(gap))
		}
		b.WriteString(bg(st.colour(a.color)).Render(a.text))
	}
	b.WriteString(space(1))
	return b.String()
}

// fitAccessories drops accessories from the front (the last ones are the most
// specific, like an age) until they fit in limit columns, truncating the last
// one if it alone is too wide.
func fitAccessories(accs []accessory, limit int) []accessory {
	width := func(as []accessory) int {
		w := 0
		for i, a := range as {
			if i > 0 {
				w += 2
			}
			w += ansi.StringWidth(a.text)
		}
		return w
	}
	for len(accs) > 1 && width(accs) > limit {
		accs = accs[1:]
	}
	if len(accs) == 1 && width(accs) > limit {
		if limit < 2 {
			return nil
		}
		accs = []accessory{{text: ansi.Truncate(accs[0].text, limit, "…"), color: accs[0].color}}
	}
	return accs
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
