package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rowsTitled(titles ...string) []listRow {
	rows := make([]listRow, len(titles))
	for i, t := range titles {
		rows[i] = listRow{title: t}
	}
	return rows
}

func shownTitles(l listView) []string {
	var out []string
	for _, s := range l.shown {
		out = append(out, l.rows[s.row].title)
	}
	return out
}

func TestEmptyQueryShowsEveryRowInOrder(t *testing.T) {
	l := newListView(rowsTitled("Say Hello", "Create Scratch", "Next Task"))
	if got := strings.Join(shownTitles(l), ","); got != "Say Hello,Create Scratch,Next Task" {
		t.Fatalf("shown = %s", got)
	}
}

func TestQueryRanksBestMatchFirst(t *testing.T) {
	l := newListView(rowsTitled("Search Project Manager", "Scratch Folders", "Create Scratch"))
	l.setQuery("scr")
	got := shownTitles(l)
	if len(got) == 0 || got[0] != "Scratch Folders" {
		t.Fatalf("shown = %v, want Scratch Folders first", got)
	}
	l.setQuery("zzz")
	if len(l.shown) != 0 || l.selected() != -1 {
		t.Fatalf("query with no matches: shown = %v, selected = %d", shownTitles(l), l.selected())
	}
}

func TestQueryMatchesSubtitleAndKeywords(t *testing.T) {
	l := newListView([]listRow{
		{title: "Say Hello", subtitle: "Hello World"},
		{title: "Next Task", keywords: []string{"jira", "todo"}},
	})
	l.setQuery("jira")
	if got := shownTitles(l); len(got) != 1 || got[0] != "Next Task" {
		t.Fatalf("shown = %v, want [Next Task]", got)
	}
	// Matches outside the title must not be highlighted in the title.
	if m := l.shown[0].matched; len(m) != 0 {
		t.Fatalf("matched = %v, want none in the title", m)
	}
}

func TestTitleRunesConvertsByteOffsets(t *testing.T) {
	// "é" is two bytes; the "l" after it is byte 3 but rune 2.
	if got := titleRunes("hél", []int{0, 3, 10}); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("titleRunes = %v, want [0 2]", got)
	}
}

func TestMoveStopsAtEnds(t *testing.T) {
	l := newListView(rowsTitled("a", "b", "c"))
	l.setHeight(10)
	l.move(-1)
	if l.cursor != 0 {
		t.Fatalf("cursor = %d after moving up from top", l.cursor)
	}
	l.move(10)
	if l.cursor != 2 {
		t.Fatalf("cursor = %d after moving past bottom", l.cursor)
	}
}

func TestScrollKeepsCursorAndSectionHeaderVisible(t *testing.T) {
	rows := []listRow{
		{group: 0, header: "One", title: "a"}, {group: 0, header: "One", title: "b"},
		{group: 1, header: "Two", title: "c"}, {group: 1, header: "Two", title: "d"},
	}
	l := newListView(rows)
	l.setHeight(2) // lines: [One] a b [Two] c d
	l.move(2)      // select c
	if l.offset != 3 {
		t.Fatalf("offset = %d, want 3 so the Two header shows above c", l.offset)
	}
	l.move(-2) // back to a
	if l.offset != 0 {
		t.Fatalf("offset = %d, want 0 so the One header shows above a", l.offset)
	}
}

func TestSubtitlesLineUpInAColumn(t *testing.T) {
	l := newListView([]listRow{
		{title: "Say Hello", subtitle: "Hello World"},
		{title: "Search Project Manager", subtitle: "Better VS Code"},
	})
	l.setHeight(2)
	lines := strings.Split(ansi.Strip(l.render(80, newStyles(true), nil)), "\n")
	column := func(line, sub string) int { // display column, not byte offset
		i := strings.Index(line, sub)
		if i < 0 {
			return -1
		}
		return ansi.StringWidth(line[:i])
	}
	a, b := column(lines[0], "Hello World"), column(lines[1], "Better VS Code")
	if a < 0 || a != b {
		t.Fatalf("subtitles start at columns %d and %d, want the same:\n%s", a, b, strings.Join(lines, "\n"))
	}
}

func TestRenderFillsWidthAndHeightExactly(t *testing.T) {
	st := newStyles(true)
	l := newListView([]listRow{
		{title: "Search Project Manager", subtitle: "Better Visual Studio Code", accessories: []accessory{{text: "large tree"}, {text: "3 days ago"}}},
		{title: "Say Hello", subtitle: "Hello World"},
	})
	for _, width := range []int{80, 30, 12} {
		l.setHeight(4)
		lines := strings.Split(l.render(width, st, []string{"nothing"}), "\n")
		if len(lines) != 4 {
			t.Fatalf("width %d: %d lines, want 4", width, len(lines))
		}
		for i, ln := range lines[:2] {
			if w := ansi.StringWidth(ln); w != width {
				t.Errorf("width %d: line %d is %d wide: %q", width, i, w, ansi.Strip(ln))
			}
		}
	}
}

func TestUntitledGroupsAreSeparatedByDividers(t *testing.T) {
	l := newListView([]listRow{{group: 0, title: "Open"}, {group: 1, title: "Copy"}, {group: 1, title: "Paste"}})
	l.setHeight(5)
	lines := strings.Split(ansi.Strip(l.render(40, newStyles(true), nil)), "\n")
	if !strings.Contains(lines[1], "──") || !strings.Contains(lines[2], "Copy") {
		t.Fatalf("want a divider between the groups:\n%s", strings.Join(lines, "\n"))
	}
	l.setQuery("p")
	if out := ansi.Strip(l.render(40, newStyles(true), nil)); strings.Contains(out, "──") {
		t.Fatalf("no dividers while searching:\n%s", out)
	}
}

func TestSetRowsKeepsSelectionAndQuery(t *testing.T) {
	l := newListView(rowsTitled("alpha", "beta", "gamma"))
	l.setHeight(5)
	l.move(2) // gamma
	l.setRows(rowsTitled("zeta", "gamma", "alpha"))
	if got := l.rows[l.selected()].title; got != "gamma" {
		t.Fatalf("selected %q after setRows, want gamma", got)
	}
	l.setQuery("a")
	l.setRows(rowsTitled("alpha", "omega"))
	if l.query != "a" {
		t.Fatal("setRows dropped the query")
	}
}

func TestAccessoriesDropFromTheFrontWhenTight(t *testing.T) {
	accs := []accessory{{text: "manual"}, {text: "large tree"}, {text: "3 days ago"}}
	if got := fitAccessories(accs, 12); len(got) != 1 || got[0].text != "3 days ago" {
		t.Fatalf("fitAccessories = %v, want only the last", got)
	}
	if got := fitAccessories(accs, 100); len(got) != 3 {
		t.Fatalf("fitAccessories = %v, want all three", got)
	}
}

func TestSearchShowsAnItemInSeveralSectionsOnce(t *testing.T) {
	l := newListView([]listRow{
		{group: 0, header: "go", title: "gt", subtitle: "~/src"},
		{group: 1, header: "work", title: "gt", subtitle: "~/src"},
		{group: 1, header: "work", title: "gt", subtitle: "~/other"},
	})
	if len(l.shown) != 3 {
		t.Fatal("without a search every section shows its copy")
	}
	l.setQuery("gt")
	if len(l.shown) != 2 {
		t.Fatalf("search shows %d rows, want 2 (duplicates collapsed, different paths kept)", len(l.shown))
	}
}
