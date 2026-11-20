package app

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/internal/storage"
	"github.com/jakeobsen/gt/kit"
)

// harness drives a Model without a terminal. Commands run synchronously:
// messages plugin code sends through the bridge are delivered before the
// command's own result, as they would be by the Bubble Tea runtime.
type harness struct {
	t       *testing.T
	m       Model
	pending []tea.Msg
	quit    bool
	// answer, when set, answers every confirmation without showing it.
	answer    *bool
	confirms  []kit.Confirmation
	terminals [][2]string
	execs     [][]string
	// editTo, when set, is what the stand-in editor saves.
	editTo *string
}

func newHarness(t *testing.T, plugins ...kit.Plugin) *harness {
	return newHarnessWithConfig(t, config.Defaults(plugins), plugins...)
}

func newHarnessWithConfig(t *testing.T, cfg *config.Config, plugins ...kit.Plugin) *harness {
	t.Helper()
	if err := kit.Validate(plugins); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t}
	stores := storage.NewStores(t.TempDir())
	dataRoot := t.TempDir()
	b := &bridge{
		storage: func(p string) kit.Storage { return stores.For(p) },
		dataDir: func(p string) string { return filepath.Join(dataRoot, p) },
		exec: func(cmd *exec.Cmd) error {
			h.execs = append(h.execs, cmd.Args)
			if h.editTo != nil {
				return os.WriteFile(cmd.Args[len(cmd.Args)-1], []byte(*h.editTo), 0o600)
			}
			return nil
		},
		send: func(msg tea.Msg) {
			if c, ok := msg.(confirmMsg); ok && h.answer != nil {
				h.confirms = append(h.confirms, c.c)
				c.reply <- *h.answer
				return
			}
			h.pending = append(h.pending, msg)
		},
		openTerminal: func(dir, command string) error {
			h.terminals = append(h.terminals, [2]string{dir, command})
			return nil
		},
	}
	h.m = newModel(plugins, b, cfg)
	h.update(tea.WindowSizeMsg{Width: 70, Height: 16})
	return h
}

func (h *harness) update(msg tea.Msg) tea.Cmd {
	next, cmd := h.m.Update(msg)
	h.m = next.(Model)
	return cmd
}

// exec runs cmd and delivers its message. Commands that don't return
// promptly are timers (tea.Tick, e.g. a list's quiet refresh) and are
// dropped; tests deliver those messages themselves.
func (h *harness) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case msg := <-result:
		h.flush()
		h.deliver(msg)
	case <-time.After(500 * time.Millisecond):
	}
}

func (h *harness) deliver(msg tea.Msg) {
	if msg == nil {
		return
	}
	if cmds, ok := asCmds(msg); ok { // tea.Sequence and tea.Batch
		for _, c := range cmds {
			h.exec(c)
		}
		return
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		h.quit = true
		return
	}
	h.exec(h.update(msg))
	h.flush()
}

func (h *harness) flush() {
	for len(h.pending) > 0 {
		msg := h.pending[0]
		h.pending = h.pending[1:]
		h.deliver(msg)
	}
}

// asCmds unwraps tea.Sequence's message (an unexported []tea.Cmd type) and
// tea.BatchMsg.
func asCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	cmdsType := reflect.TypeFor[[]tea.Cmd]()
	v := reflect.ValueOf(msg)
	if !v.IsValid() || !v.Type().ConvertibleTo(cmdsType) {
		return nil, false
	}
	return v.Convert(cmdsType).Interface().([]tea.Cmd), true
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.exec(h.update(tea.KeyPressMsg{Code: r, Text: string(r)}))
	}
}

func (h *harness) press(code rune, mod ...tea.KeyMod) {
	k := tea.KeyPressMsg{Code: code}
	for _, m := range mod {
		k.Mod |= m
	}
	h.exec(h.update(k))
}

func (h *harness) ctrl(r rune) { h.press(r, tea.ModCtrl) }

func (h *harness) screen() string { return ansi.Strip(h.m.render()) }

func (h *harness) mustShow(want ...string) {
	h.t.Helper()
	view := h.screen()
	for _, w := range want {
		if !strings.Contains(view, w) {
			h.t.Fatalf("screen is missing %q:\n%s", w, view)
		}
	}
}

func ok(kit.Context) error { return nil }

func plugin(cmds ...kit.Command) kit.Plugin {
	return kit.Plugin{Name: "test", Title: "Test Plugin", Commands: cmds}
}

func loadConfig(t *testing.T, content string, plugins ...kit.Plugin) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, plugins)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// --- root search ----------------------------------------------------------

func TestRootListsCommandsAlphabetically(t *testing.T) {
	h := newHarness(t, plugin(
		kit.Command{Name: "b", Title: "Zebra", Run: ok},
		kit.Command{Name: "a", Title: "apple", Run: ok},
	))
	view := h.screen()
	if a, z := strings.Index(view, "apple"), strings.Index(view, "Zebra"); a < 0 || z < 0 || a > z {
		t.Fatalf("want apple listed before Zebra:\n%s", view)
	}
}

func TestEnterRunsCommandAndHUDQuitsWithMessage(t *testing.T) {
	ran := false
	h := newHarness(t, plugin(
		kit.Command{Name: "other", Title: "Other Thing", Run: ok},
		kit.Command{Name: "say-hello", Title: "Say Hello", Run: func(ctx kit.Context) error {
			ran = true
			ctx.HUD("hi there")
			return nil
		}},
	))
	h.typeText("say")
	h.press(tea.KeyEnter)
	if !ran || !h.quit || h.m.exitMessage != "hi there" {
		t.Fatalf("ran=%v quit=%v exitMessage=%q", ran, h.quit, h.m.exitMessage)
	}
}

func TestCommandErrorShowsAsFailureToast(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "fail", Title: "Fail", Run: func(kit.Context) error {
		return errors.New("disk on fire\nmore detail")
	}}))
	h.press(tea.KeyEnter)
	if h.m.busy {
		t.Fatal("still busy after the command finished")
	}
	h.mustShow("✗ disk on fire")
}

func TestPanickingCommandBecomesError(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "boom", Title: "Boom", Run: func(kit.Context) error {
		panic("oops")
	}}))
	h.press(tea.KeyEnter)
	h.mustShow("plugin panicked: oops")
}

func TestEscClearsSearchThenQuits(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: ok}))
	h.typeText("zz")
	h.mustShow("No results")
	h.press(tea.KeyEscape)
	if h.quit || h.m.top().query() != "" {
		t.Fatal("first esc should clear the search without quitting")
	}
	h.press(tea.KeyEscape)
	if !h.quit {
		t.Fatal("second esc should quit")
	}
}

func TestRenderFitsTheWindow(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: ok}))
	lines := strings.Split(h.m.render(), "\n")
	if len(lines) != 16 {
		t.Fatalf("rendered %d lines, want 16", len(lines))
	}
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w > 70 {
			t.Errorf("line %d is %d wide, more than the window", i, w)
		}
	}
}

// --- settings ---------------------------------------------------------------

func TestDisabledPluginsAreHidden(t *testing.T) {
	shown := kit.Plugin{Name: "shown", Title: "Shown", Commands: []kit.Command{{Name: "a", Title: "Visible Command", Run: ok}}}
	hidden := kit.Plugin{Name: "hidden", Title: "Hidden", Commands: []kit.Command{{Name: "b", Title: "Secret Command", Run: ok}}}
	cfg := loadConfig(t, "[plugins.hidden]\nenabled = false\n", shown, hidden)
	view := ansi.Strip(newHarnessWithConfig(t, cfg, shown, hidden).m.render())
	if !strings.Contains(view, "Visible Command") || strings.Contains(view, "Secret Command") {
		t.Fatalf("want only the enabled plugin's command:\n%s", view)
	}

	cfg = loadConfig(t, "[plugins.shown]\nenabled = false\n[plugins.hidden]\nenabled = false\n", shown, hidden)
	newHarnessWithConfig(t, cfg, shown, hidden).mustShow("No commands", "Every plugin is turned off")
}

func TestThemeSettingOverridesTerminalBackground(t *testing.T) {
	p := plugin(kit.Command{Name: "x", Title: "X", Run: ok})
	h := newHarnessWithConfig(t, loadConfig(t, `theme = "light"`, p), p)
	h.update(tea.BackgroundColorMsg{Color: color.Black})
	if h.m.st.isDark {
		t.Fatal("theme = \"light\" was overridden by a dark terminal background")
	}
	h = newHarness(t, p) // theme = "auto"
	h.update(tea.BackgroundColorMsg{Color: color.White})
	if h.m.st.isDark {
		t.Fatal("theme = \"auto\" ignored a light terminal background")
	}
}

func TestCommandReadsItsPluginSettings(t *testing.T) {
	var got int
	p := kit.Plugin{
		Name: "counter", Title: "Counter",
		Settings: []kit.Setting{{Key: "limit", Kind: kit.Int, Default: 3, Description: "Limit."}},
		Commands: []kit.Command{{Name: "read", Title: "Read Limit", Run: func(ctx kit.Context) error {
			got = ctx.Settings().Int("limit")
			return nil
		}}},
	}
	h := newHarnessWithConfig(t, loadConfig(t, "[plugins.counter]\nlimit = 42\n", p), p)
	h.press(tea.KeyEnter)
	if got != 42 {
		t.Fatalf("command read limit = %d, want 42 from the config file", got)
	}
}

// --- view commands ----------------------------------------------------------

// fruitList is a plugin list with two sections whose items record what ran.
type fruitList struct {
	loads int
	ran   []string
}

func (f *fruitList) plugin() kit.Plugin {
	act := func(name, shortcut string) kit.Action {
		return kit.Action{Title: name, Shortcut: shortcut, Run: func(kit.Context) error {
			f.ran = append(f.ran, name)
			return nil
		}}
	}
	return plugin(kit.Command{Name: "fruit", Title: "Fruit", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{SearchPlaceholder: "Search fruit", Load: func(kit.Context) (kit.Content, error) {
			f.loads++
			return kit.Content{Sections: []kit.Section{
				{Title: "Fresh", Subtitle: "2", Items: []kit.Item{
					{Title: "Apple", Accessories: []kit.Accessory{{Text: "today"}}, Actions: []kit.Action{act("Eat", ""), kit.Divider, act("Peel", "ctrl+e")}},
					{Title: "Banana", Actions: []kit.Action{act("Eat Banana", "")}},
				}},
				{Title: "Stale", Items: []kit.Item{
					{Title: "Cherry", Accessories: []kit.Accessory{{Text: "9 days ago", Color: kit.Red}}, Actions: []kit.Action{act("Toss", "")}},
				}},
			}}, nil
		}}, nil
	}})
}

func TestViewCommandOpensListWithSections(t *testing.T) {
	f := &fruitList{}
	h := newHarness(t, f.plugin())
	h.press(tea.KeyEnter)
	h.mustShow("goto › Fruit", "Fresh  2", "Apple", "today", "Stale", "Cherry", "9 days ago", "↵ Eat", "ctrl+k Actions", "esc Back")
	if f.loads != 1 {
		t.Fatalf("loaded %d times, want 1", f.loads)
	}
	h.press(tea.KeyDown)
	h.press(tea.KeyEnter)
	if strings.Join(f.ran, ",") != "Eat Banana" {
		t.Fatalf("ran %v, want the selected item's primary action", f.ran)
	}
	h.press(tea.KeyEscape)
	h.mustShow("Search commands…")
}

func TestShortcutRunsAction(t *testing.T) {
	f := &fruitList{}
	h := newHarness(t, f.plugin())
	h.press(tea.KeyEnter)
	h.ctrl('e')
	if strings.Join(f.ran, ",") != "Peel" {
		t.Fatalf("ran %v, want Peel via ctrl+e", f.ran)
	}
}

func TestActionsMenuRunsChosenActionAndCloses(t *testing.T) {
	f := &fruitList{}
	h := newHarness(t, f.plugin())
	h.press(tea.KeyEnter)
	h.ctrl('k')
	h.mustShow("goto › Apple", "Eat", "Peel", "ctrl+e", "──")
	h.typeText("peel")
	h.press(tea.KeyEnter)
	if strings.Join(f.ran, ",") != "Peel" {
		t.Fatalf("ran %v, want Peel from the menu", f.ran)
	}
	h.mustShow("goto › Fruit") // the menu closed
}

func TestRefreshReloadsKeepingSelection(t *testing.T) {
	f := &fruitList{}
	p := f.plugin()
	p.Commands[0].Open = func(kit.Context) (kit.View, error) {
		return &kit.List{Load: func(kit.Context) (kit.Content, error) {
			f.loads++
			return kit.Content{Sections: []kit.Section{{Items: []kit.Item{
				{Title: "One"}, {Title: "Two", Actions: []kit.Action{{Title: "Reload", Shortcut: "ctrl+r", Run: func(ctx kit.Context) error {
					ctx.Refresh()
					return nil
				}}}},
			}}}}, nil
		}}, nil
	}
	h := newHarness(t, p)
	h.press(tea.KeyEnter)
	h.press(tea.KeyDown)
	h.ctrl('r')
	if f.loads != 2 {
		t.Fatalf("loaded %d times, want 2 after a refresh", f.loads)
	}
	if s := h.m.top().(*listScreen); s.items[s.list.selected()].row.title != "Two" {
		t.Fatal("refresh lost the selection")
	}
}

func TestEmptyListShowsEmptyViewAndItsActions(t *testing.T) {
	created := false
	h := newHarness(t, plugin(kit.Command{Name: "e", Title: "Empty", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{Load: func(kit.Context) (kit.Content, error) {
			return kit.Content{Empty: kit.Empty{
				Title: "Nothing here", Description: "ctrl+k makes one",
				Actions: []kit.Action{{Title: "Make One", Run: func(kit.Context) error { created = true; return nil }}},
			}}, nil
		}}, nil
	}}))
	h.press(tea.KeyEnter)
	h.mustShow("Nothing here", "ctrl+k makes one", "↵ Make One")
	h.press(tea.KeyEnter)
	if !created {
		t.Fatal("enter on an empty list should run the empty view's first action")
	}
}

func TestLoadErrorShowsToast(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "e", Title: "Broken", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{Load: func(kit.Context) (kit.Content, error) { return kit.Content{}, errors.New("no access") }}, nil
	}}))
	h.press(tea.KeyEnter)
	h.mustShow("✗ Could not load: no access")
}

func TestPushOpensScreenWithItsTitle(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "p", Title: "Pusher", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{Load: func(kit.Context) (kit.Content, error) {
			return kit.Content{Sections: []kit.Section{{Items: []kit.Item{{Title: "Go deeper", Actions: []kit.Action{{
				Title: "Deeper", Run: func(ctx kit.Context) error {
					ctx.Push(&kit.List{Title: "Second", Load: func(kit.Context) (kit.Content, error) {
						return kit.Content{Sections: []kit.Section{{Items: []kit.Item{{Title: "Bottom"}}}}}, nil
					}})
					return nil
				},
			}}}}}}}, nil
		}}, nil
	}}))
	h.press(tea.KeyEnter)
	h.press(tea.KeyEnter)
	h.mustShow("goto › Second", "Bottom")
	h.press(tea.KeyEscape)
	h.mustShow("goto › Pusher")
}

// --- toasts, confirmations, closing ------------------------------------------

func TestToastShowsAndUpdates(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "t", Title: "Toaster", Run: func(ctx kit.Context) error {
		toast := ctx.Toast(kit.Loading, "Cleaning up 3 folders…", "")
		toast.Update(kit.Failure, "Removed 2 of 3 folders", "c: permission denied\nd: busy")
		return nil
	}}))
	h.press(tea.KeyEnter)
	h.mustShow("✗ Removed 2 of 3 folders — c: perm")
	if got := h.m.renderToast(*h.m.toast, 200); !strings.Contains(ansi.Strip(got), "c: permission denied (+1 more)") {
		t.Fatalf("toast = %q, want the first line of the message and a count of the rest", ansi.Strip(got))
	}
	h.press(tea.KeyDown)
	if strings.Contains(h.screen(), "Removed 2") {
		t.Fatal("a finished toast should clear on the next key")
	}
}

func TestStaleToastUpdatesAreIgnored(t *testing.T) {
	h := newHarness(t)
	h.update(toastMsg{id: 1, style: kit.Loading, title: "first"})
	h.update(toastMsg{id: 2, style: kit.Success, title: "second"})
	h.update(toastMsg{id: 1, style: kit.Failure, title: "first, updated"})
	h.mustShow("✓ second")
}

func TestConfirmDialog(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: ok}))
	reply := make(chan bool, 1)
	h.update(confirmMsg{c: kit.Confirmation{Title: "Move to Trash?", Message: "~/scratch/otter", Action: "Move to Trash", Destructive: true}, reply: reply})
	h.mustShow("Move to Trash?", "~/scratch/otter", "enter Move to Trash", "esc Cancel")
	h.press('x') // other keys do nothing
	h.press(tea.KeyEscape)
	if got := <-reply; got {
		t.Fatal("esc should cancel")
	}
	if h.quit || h.m.confirm != nil {
		t.Fatal("esc on the dialog should only close the dialog")
	}

	h.update(confirmMsg{c: kit.Confirmation{Title: "Sure?"}, reply: reply})
	h.press(tea.KeyEnter)
	if got := <-reply; !got {
		t.Fatal("enter should confirm")
	}
}

func TestPluginConfirmFlow(t *testing.T) {
	var answered bool
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "Ask", Run: func(ctx kit.Context) error {
		answered = ctx.Confirm(kit.Confirmation{Title: "Delete?"})
		return nil
	}}))
	yes := true
	h.answer = &yes
	h.press(tea.KeyEnter)
	if !answered || len(h.confirms) != 1 || h.confirms[0].Title != "Delete?" {
		t.Fatalf("answered=%v confirms=%v", answered, h.confirms)
	}
}

func TestCloseQuits(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: func(ctx kit.Context) error { ctx.Close(); return nil }}))
	h.press(tea.KeyEnter)
	if !h.quit || h.m.exitMessage != "" {
		t.Fatalf("quit=%v exitMessage=%q", h.quit, h.m.exitMessage)
	}
}

func TestOpenTerminalGoesThroughTheBridge(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "Term", Run: func(ctx kit.Context) error {
		return ctx.OpenTerminal("/tmp/here", "lazygit")
	}}))
	h.press(tea.KeyEnter)
	if len(h.terminals) != 1 || h.terminals[0] != [2]string{"/tmp/here", "lazygit"} {
		t.Fatalf("terminals = %v", h.terminals)
	}
}

// --- arguments ----------------------------------------------------------------

func TestArgumentsArePromptedBeforeRunning(t *testing.T) {
	var label string
	ran := false
	h := newHarness(t, plugin(kit.Command{
		Name: "create", Title: "Create Scratch",
		Arguments: []kit.Argument{{Name: "label", Placeholder: "label (optional)"}},
		Run: func(ctx kit.Context) error {
			ran, label = true, ctx.Argument("label")
			return nil
		},
	}))
	h.press(tea.KeyEnter)
	h.mustShow("goto › Create Scratch", "label (optional)", "↵ Run Command")
	if ran {
		t.Fatal("ran before the arguments were given")
	}
	h.typeText("redis repro")
	h.press(tea.KeyEnter)
	if !ran || label != "redis repro" {
		t.Fatalf("ran=%v label=%q", ran, label)
	}
	h.mustShow("Search commands…") // the prompt closed
}

func TestRequiredArgumentMustBeFilled(t *testing.T) {
	ran := false
	h := newHarness(t, plugin(kit.Command{
		Name: "go", Title: "Go",
		Arguments: []kit.Argument{{Name: "where", Placeholder: "where", Required: true}},
		Run:       func(kit.Context) error { ran = true; return nil },
	}))
	h.press(tea.KeyEnter)
	h.press(tea.KeyEnter)
	if ran {
		t.Fatal("ran without a required argument")
	}
	h.mustShow("where is required")
}

// --- filters and storage -----------------------------------------------------

func TestFilterPickerReloadsWithTheChoice(t *testing.T) {
	var seen []string
	h := newHarness(t, plugin(kit.Command{Name: "f", Title: "Projects", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{
			Filter: &kit.Filter{Title: "Tag", Default: "", Options: []kit.FilterOption{{Title: "All Tags", Value: ""}, {Title: "work", Value: "work"}}},
			Load: func(ctx kit.Context) (kit.Content, error) {
				seen = append(seen, ctx.Filter())
				return kit.Content{Sections: []kit.Section{{Items: []kit.Item{{Title: "showing " + ctx.Filter()}}}}}, nil
			},
		}, nil
	}}))
	h.press(tea.KeyEnter)
	h.mustShow("[All Tags ▾]", "tab Tag")
	h.press(tea.KeyTab)
	h.mustShow("goto › Tag", "All Tags", "✓", "work")
	h.typeText("work")
	h.press(tea.KeyEnter)
	h.mustShow("goto › Projects", "[work ▾]", "showing work")
	if strings.Join(seen, ",") != ",work" {
		t.Fatalf("Load saw filters %q, want \"\" then work", seen)
	}
}

func TestPluginStoragePersistsAcrossCommands(t *testing.T) {
	var got string
	h := newHarness(t, plugin(
		kit.Command{Name: "a", Title: "Write", Run: func(ctx kit.Context) error { return ctx.Storage().Set("k", "v") }},
		kit.Command{Name: "b", Title: "Zread", Run: func(ctx kit.Context) error { got, _ = ctx.Storage().Get("k"); return nil }},
	))
	h.press(tea.KeyEnter) // Write
	h.press(tea.KeyDown)
	h.press(tea.KeyEnter) // Zread
	if got != "v" {
		t.Fatalf("read %q, want v", got)
	}
}

// --- details, quiet refresh, editing, data folder ---------------------------

func TestDetailPanelShowsSelectedItem(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "d", Title: "Detailed", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{ShowDetail: true, Load: func(kit.Context) (kit.Content, error) {
			return kit.Content{Sections: []kit.Section{{Items: []kit.Item{
				{Title: "First", Detail: "## Heading one\n\nThe **reason** it matters."},
				{Title: "Second", Detail: "Other detail"},
			}}}}, nil
		}}, nil
	}}))
	h.update(tea.WindowSizeMsg{Width: 100, Height: 14})
	h.press(tea.KeyEnter)
	h.mustShow("│ Heading one", "The reason it matters.")
	h.press(tea.KeyDown)
	h.mustShow("│ Other detail")
	if strings.Contains(h.screen(), "**") {
		t.Fatal("bold markers should not be shown")
	}
}

func TestQuietRefreshReloadsWithoutLoadingAndStaleTicksAreIgnored(t *testing.T) {
	loads := 0
	h := newHarness(t, plugin(kit.Command{Name: "p", Title: "Poller", Open: func(kit.Context) (kit.View, error) {
		return &kit.List{Load: func(kit.Context) (kit.Content, error) {
			loads++
			return kit.Content{Sections: []kit.Section{{Items: []kit.Item{{Title: fmt.Sprint("load ", loads)}}}}, RefreshAfter: time.Hour}, nil
		}}, nil
	}}))
	// Open the list; its Load returns RefreshAfter, which schedules a tick.
	h.exec(h.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	s := h.m.top().(*listScreen)
	gen := s.tickGen
	h.deliver(tickMsg{screen: s.sid, gen: gen})
	if loads != 2 {
		t.Fatalf("loads = %d after a tick, want 2", loads)
	}
	if s.isLoading {
		t.Fatal("a quiet refresh must not show the loading indicator")
	}
	h.deliver(tickMsg{screen: s.sid, gen: gen}) // superseded by the reload it caused
	if loads != 2 {
		t.Fatalf("a stale tick reloaded (loads = %d)", loads)
	}
}

func TestEditTextUsesTheEditor(t *testing.T) {
	var got string
	edited := "new text\n"
	h := newHarness(t, plugin(kit.Command{Name: "e", Title: "Edit", Run: func(ctx kit.Context) error {
		var err error
		got, err = ctx.EditText("workflow.md", "old text\n")
		return err
	}}))
	t.Setenv("VISUAL", "nano -w")
	h.editTo = &edited
	h.press(tea.KeyEnter)
	if got != edited {
		t.Fatalf("EditText returned %q", got)
	}
	if args := h.execs[0]; args[0] != "/bin/sh" || !strings.Contains(args[2], "nano -w") || !strings.HasSuffix(args[len(args)-1], "workflow.md") {
		t.Fatalf("editor command = %q", args)
	}
}

func TestDataDirIsPerPluginAndCreated(t *testing.T) {
	var dir string
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: func(ctx kit.Context) error {
		var err error
		dir, err = ctx.DataDir()
		return err
	}}))
	h.press(tea.KeyEnter)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() || filepath.Base(dir) != "test" {
		t.Fatalf("DataDir() = %q (%v)", dir, err)
	}
}
