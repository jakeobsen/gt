package app

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

// harness drives a Model without a terminal, collecting what plugin code
// sends through the bridge.
type harness struct {
	t    *testing.T
	m    Model
	sent []tea.Msg
}

func newHarness(t *testing.T, plugins ...kit.Plugin) *harness {
	return newHarnessWithConfig(t, config.Defaults(plugins), plugins...)
}

func newHarnessWithConfig(t *testing.T, cfg *config.Config, plugins ...kit.Plugin) *harness {
	h := &harness{t: t}
	b := &bridge{send: func(msg tea.Msg) { h.sent = append(h.sent, msg) }}
	h.m = newModel(plugins, b, cfg)
	h.update(tea.WindowSizeMsg{Width: 60, Height: 12})
	return h
}

func (h *harness) update(msg tea.Msg) tea.Cmd {
	next, cmd := h.m.Update(msg)
	h.m = next.(Model)
	return cmd
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (h *harness) press(code rune) tea.Cmd { return h.update(tea.KeyPressMsg{Code: code}) }

// runSequence executes a command the way the runtime would, running the parts
// of a tea.Sequence in order and feeding each message back into Update.
func (h *harness) runSequence(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		h.t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	cmds, ok := asCmds(msg)
	if !ok {
		h.update(msg)
		return
	}
	for _, c := range cmds {
		if c != nil {
			h.update(c())
		}
	}
}

// asCmds unwraps the message tea.Sequence produces. Its type is unexported,
// so match it by its underlying type, []tea.Cmd.
func asCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	cmdsType := reflect.TypeFor[[]tea.Cmd]()
	v := reflect.ValueOf(msg)
	if !v.IsValid() || !v.Type().ConvertibleTo(cmdsType) {
		return nil, false
	}
	return v.Convert(cmdsType).Interface().([]tea.Cmd), true
}

func plugin(cmds ...kit.Command) kit.Plugin {
	return kit.Plugin{Name: "test", Title: "Test Plugin", Commands: cmds}
}

func TestRootListsCommandsAlphabetically(t *testing.T) {
	h := newHarness(t, plugin(
		kit.Command{Name: "b", Title: "Zebra", Run: func(kit.Context) error { return nil }},
		kit.Command{Name: "a", Title: "apple", Run: func(kit.Context) error { return nil }},
	))
	view := ansi.Strip(h.m.render())
	if a, z := strings.Index(view, "apple"), strings.Index(view, "Zebra"); a < 0 || z < 0 || a > z {
		t.Fatalf("want apple listed before Zebra:\n%s", view)
	}
}

func TestEnterRunsCommandAndHUDQuitsWithMessage(t *testing.T) {
	ran := false
	h := newHarness(t, plugin(
		kit.Command{Name: "other", Title: "Other Thing", Run: func(kit.Context) error { return nil }},
		kit.Command{Name: "say-hello", Title: "Say Hello", Run: func(ctx kit.Context) error {
			ran = true
			ctx.HUD("hi there")
			return nil
		}},
	))
	h.typeText("say")
	h.runSequence(h.press(tea.KeyEnter))
	if !ran {
		t.Fatal("command did not run")
	}
	if len(h.sent) != 1 {
		t.Fatalf("bridge sent %d messages, want 1 (the HUD)", len(h.sent))
	}
	cmd := h.update(h.sent[0])
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatal("HUD did not quit")
	}
	if h.m.exitMessage != "hi there" {
		t.Fatalf("exitMessage = %q", h.m.exitMessage)
	}
}

func TestCommandErrorShowsInStatusLine(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "fail", Title: "Fail", Run: func(kit.Context) error {
		return errors.New("disk on fire\nmore detail")
	}}))
	h.runSequence(h.press(tea.KeyEnter))
	if h.m.busy {
		t.Fatal("still busy after the command finished")
	}
	if !h.m.statusError || h.m.status != "disk on fire" {
		t.Fatalf("status = %q (error %v), want the first line of the error", h.m.status, h.m.statusError)
	}
	if !strings.Contains(ansi.Strip(h.m.render()), "disk on fire") {
		t.Fatal("error not shown in the footer")
	}
}

func TestPanickingCommandBecomesError(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "boom", Title: "Boom", Run: func(kit.Context) error {
		panic("oops")
	}}))
	h.runSequence(h.press(tea.KeyEnter))
	if !h.m.statusError || !strings.Contains(h.m.status, "plugin panicked: oops") {
		t.Fatalf("status = %q, want the panic reported", h.m.status)
	}
}

func TestEscClearsSearchThenQuits(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: func(kit.Context) error { return nil }}))
	h.typeText("zz")
	if !strings.Contains(ansi.Strip(h.m.render()), "No matching commands") {
		t.Fatal("expected the empty-results message")
	}
	if cmd := h.press(tea.KeyEscape); cmd != nil || h.m.top().query() != "" {
		t.Fatal("first esc should clear the search without quitting")
	}
	cmd := h.press(tea.KeyEscape)
	if cmd == nil {
		t.Fatal("second esc should ask to close the root screen")
	}
	quit := h.update(cmd())
	if quit == nil || quit() != (tea.QuitMsg{}) {
		t.Fatal("closing the root screen should quit")
	}
}

func TestRenderFitsTheWindow(t *testing.T) {
	h := newHarness(t, plugin(kit.Command{Name: "x", Title: "X", Run: func(kit.Context) error { return nil }}))
	lines := strings.Split(h.m.render(), "\n")
	if len(lines) != 12 {
		t.Fatalf("rendered %d lines, want 12", len(lines))
	}
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w > 60 {
			t.Errorf("line %d is %d wide, more than the window", i, w)
		}
	}
}

// loadConfig writes a config file and loads it for the given plugins.
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

func TestDisabledPluginsAreHidden(t *testing.T) {
	ok := func(kit.Context) error { return nil }
	shown := kit.Plugin{Name: "shown", Title: "Shown", Commands: []kit.Command{{Name: "a", Title: "Visible Command", Run: ok}}}
	hidden := kit.Plugin{Name: "hidden", Title: "Hidden", Commands: []kit.Command{{Name: "b", Title: "Secret Command", Run: ok}}}
	cfg := loadConfig(t, "[plugins.hidden]\nenabled = false\n", shown, hidden)
	view := ansi.Strip(newHarnessWithConfig(t, cfg, shown, hidden).m.render())
	if !strings.Contains(view, "Visible Command") || strings.Contains(view, "Secret Command") {
		t.Fatalf("want only the enabled plugin's command:\n%s", view)
	}

	cfg = loadConfig(t, "[plugins.shown]\nenabled = false\n[plugins.hidden]\nenabled = false\n", shown, hidden)
	view = ansi.Strip(newHarnessWithConfig(t, cfg, shown, hidden).m.render())
	if !strings.Contains(view, "every plugin is turned off") {
		t.Fatalf("want an explanation when nothing is enabled:\n%s", view)
	}
}

func TestThemeSettingOverridesTerminalBackground(t *testing.T) {
	p := plugin(kit.Command{Name: "x", Title: "X", Run: func(kit.Context) error { return nil }})
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
	h.runSequence(h.press(tea.KeyEnter))
	if got != 42 {
		t.Fatalf("command read limit = %d, want 42 from the config file", got)
	}
}
