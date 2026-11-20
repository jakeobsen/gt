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
		{section: "One", title: "a"}, {section: "One", title: "b"},
		{section: "Two", title: "c"}, {section: "Two", title: "d"},
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
	lines := strings.Split(ansi.Strip(l.render(80, newStyles(true), "")), "\n")
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
		{title: "Search Project Manager", subtitle: "Better Visual Studio Code", accessory: "3 days ago"},
		{title: "Say Hello", subtitle: "Hello World"},
	})
	for _, width := range []int{80, 30, 12} {
		l.setHeight(4)
		lines := strings.Split(l.render(width, st, "nothing"), "\n")
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
