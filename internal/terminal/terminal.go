// Package terminal opens a terminal at a directory, optionally running a
// command, in the place that suits how gt is running: a new tmux window when
// gt runs inside tmux, otherwise a new window of the terminal app gt runs in.
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jakeobsen/gt/kit"
)

// Strategies, as named by the "terminal" setting.
const (
	Auto        = "auto"
	Tmux        = "tmux"
	TerminalApp = "terminal"
	ITerm       = "iterm2"
	Kitty       = "kitty"
	Ghostty     = "ghostty"
	WezTerm     = "wezterm"
)

// Options documents each strategy for the "terminal" setting.
var Options = []kit.Option{
	{Value: Auto, Description: "a new tmux window inside tmux, otherwise a new window of the terminal gt runs in"},
	{Value: Tmux, Description: "a new tmux window"},
	{Value: TerminalApp, Description: "a new Terminal.app window"},
	{Value: ITerm, Description: "a new iTerm2 window"},
	{Value: Kitty, Description: "a new kitty window"},
	{Value: Ghostty, Description: "a new Ghostty window"},
	{Value: WezTerm, Description: "a new WezTerm window"},
}

// Env is the part of the environment that decides what "auto" means.
type Env struct {
	Tmux        string // $TMUX
	TermProgram string // $TERM_PROGRAM
	Term        string // $TERM
	KittyWindow string // $KITTY_WINDOW_ID
	Shell       string // $SHELL
}

// FromOS reads Env from the process environment.
func FromOS() Env {
	return Env{
		Tmux:        os.Getenv("TMUX"),
		TermProgram: os.Getenv("TERM_PROGRAM"),
		Term:        os.Getenv("TERM"),
		KittyWindow: os.Getenv("KITTY_WINDOW_ID"),
		Shell:       os.Getenv("SHELL"),
	}
}

// Resolve turns a setting value into a concrete strategy, working out what
// "auto" means in env. Terminal.app is the fallback: every Mac has it.
func Resolve(setting string, env Env) string {
	if setting != Auto && setting != "" {
		return setting
	}
	switch {
	case env.Tmux != "":
		return Tmux
	case env.TermProgram == "iTerm.app":
		return ITerm
	case env.TermProgram == "ghostty":
		return Ghostty
	case env.TermProgram == "WezTerm":
		return WezTerm
	case env.KittyWindow != "" || env.Term == "xterm-kitty":
		return Kitty
	}
	return TerminalApp
}

// Command returns the program and arguments that open a terminal at dir for
// a concrete strategy, running command when it isn't empty.
func Command(strategy string, env Env, dir, command string) (string, []string, error) {
	shell := env.Shell
	if shell == "" {
		shell = "/bin/zsh"
	}
	// What to type into a shell for terminals driven by AppleScript.
	line := "cd " + kit.ShellQuote(dir)
	if command != "" {
		line += " && " + command
	}
	// How to run command as a program for terminals that take argv.
	var argv []string
	if command != "" {
		argv = []string{shell, "-lic", command}
	}

	switch strategy {
	case Tmux:
		args := []string{"new-window", "-c", dir}
		if command != "" {
			args = append(args, command)
		}
		return "tmux", args, nil
	case TerminalApp:
		script := "tell application \"Terminal\"\nactivate\ndo script " + kit.AppleScriptQuote(line) + "\nend tell"
		return "/usr/bin/osascript", []string{"-e", script}, nil
	case ITerm:
		script := "tell application \"iTerm\"\nactivate\nset w to (create window with default profile)\n" +
			"tell current session of w to write text " + kit.AppleScriptQuote(line) + "\nend tell"
		return "/usr/bin/osascript", []string{"-e", script}, nil
	case Kitty:
		args := []string{"-na", "kitty", "--args", "--single-instance", "--directory", dir}
		return "/usr/bin/open", append(args, argv...), nil
	case Ghostty:
		args := []string{"-na", "Ghostty", "--args", "--working-directory=" + dir}
		if argv != nil {
			args = append(args, "-e")
			args = append(args, argv...)
		}
		return "/usr/bin/open", args, nil
	case WezTerm:
		args := []string{"-na", "WezTerm", "--args", "start", "--cwd", dir}
		if argv != nil {
			args = append(args, "--")
			args = append(args, argv...)
		}
		return "/usr/bin/open", args, nil
	}
	return "", nil, fmt.Errorf("unknown terminal %q", strategy)
}

// Open opens a terminal at dir as the setting asks, running command if it
// isn't empty.
func Open(setting, dir, command string) error {
	env := FromOS()
	name, args, err := Command(Resolve(setting, env), env, dir, command)
	if err != nil {
		return err
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("opening a terminal: %s", msg)
		}
		return fmt.Errorf("opening a terminal: %w", err)
	}
	return nil
}
