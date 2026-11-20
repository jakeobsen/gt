package terminal

import (
	"slices"
	"strings"
	"testing"
)

func TestResolveAuto(t *testing.T) {
	for _, c := range []struct {
		env  Env
		want string
	}{
		{Env{Tmux: "/tmp/tmux-1/default,1,0", TermProgram: "iTerm.app"}, Tmux},
		{Env{TermProgram: "iTerm.app"}, ITerm},
		{Env{TermProgram: "ghostty"}, Ghostty},
		{Env{TermProgram: "WezTerm"}, WezTerm},
		{Env{KittyWindow: "1"}, Kitty},
		{Env{Term: "xterm-kitty"}, Kitty},
		{Env{TermProgram: "Apple_Terminal"}, TerminalApp},
		{Env{}, TerminalApp},
	} {
		if got := Resolve(Auto, c.env); got != c.want {
			t.Errorf("Resolve(auto, %+v) = %q, want %q", c.env, got, c.want)
		}
	}
	if got := Resolve(Kitty, Env{Tmux: "x"}); got != Kitty {
		t.Errorf("an explicit setting must win over detection; got %q", got)
	}
}

func TestEveryOptionHasACommand(t *testing.T) {
	for _, o := range Options {
		s := Resolve(o.Value, Env{})
		if _, _, err := Command(s, Env{}, "/tmp/x", ""); err != nil {
			t.Errorf("%s: %v", o.Value, err)
		}
	}
}

func TestTmuxCommand(t *testing.T) {
	name, args, _ := Command(Tmux, Env{}, "/a b", "lazygit")
	if name != "tmux" || !slices.Equal(args, []string{"new-window", "-c", "/a b", "lazygit"}) {
		t.Fatalf("got %s %q", name, args)
	}
	_, args, _ = Command(Tmux, Env{}, "/a b", "")
	if !slices.Equal(args, []string{"new-window", "-c", "/a b"}) {
		t.Fatalf("without a command: %q", args)
	}
}

func TestAppleScriptTerminalsQuoteTheDirectory(t *testing.T) {
	for _, s := range []string{TerminalApp, ITerm} {
		_, args, _ := Command(s, Env{}, `/it's "here"`, "lazygit")
		script := args[1]
		if !strings.Contains(script, `cd '/it'\\''s \"here\"' && lazygit`) {
			t.Errorf("%s script doesn't quote the path for the shell and AppleScript:\n%s", s, script)
		}
	}
}

func TestArgvTerminalsRunTheCommandInTheLoginShell(t *testing.T) {
	env := Env{Shell: "/bin/bash"}
	for _, s := range []string{Kitty, Ghostty, WezTerm} {
		_, args, _ := Command(s, env, "/d", "lazygit")
		if !slices.Contains(args, "/bin/bash") || args[len(args)-1] != "lazygit" {
			t.Errorf("%s: %q", s, args)
		}
		_, args, _ = Command(s, env, "/d", "")
		if slices.Contains(args, "/bin/bash") {
			t.Errorf("%s without a command shouldn't run a shell command: %q", s, args)
		}
	}
}
