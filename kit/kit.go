// Package kit is goto's plugin kit: the types a plugin uses to describe its
// commands and to talk to the goto UI while they run. It plays the part that
// @raycast/api played for the Raycast extensions these plugins come from.
//
// A plugin is a Go package that exports a function returning a [Plugin]. It is
// compiled into gt by adding one line to cmd/gt/plugins.go.
package kit

import (
	"errors"
	"fmt"
	"regexp"
)

// Plugin is one extension: a named group of commands.
type Plugin struct {
	// Name is the plugin's stable id in kebab-case, e.g. "scratch". It will
	// name the plugin's section in the config file and its saved data, so
	// renaming a plugin orphans its settings.
	Name string

	// Title is shown next to each of the plugin's commands in the root search.
	Title string

	// Description says what the plugin is for.
	Description string

	// Commands are the entries this plugin adds to the root search.
	Commands []Command

	// Settings are the values users can set for this plugin in the config
	// file, under [plugins.<Name>]. Commands read them with Context.Settings.
	Settings []Setting
}

// Command is one entry in the root search.
type Command struct {
	// Name is the command's id in kebab-case, unique within its plugin.
	Name string

	// Title is what the root search shows and matches against.
	Title string

	// Description is a one-line summary of what the command does.
	Description string

	// Keywords are extra words the root search matches against.
	Keywords []string

	// Arguments are asked for before the command runs; read them with
	// Context.Argument.
	Arguments []Argument

	// Exactly one of Run and Open must be set.

	// Run performs a command that has no screen of its own, like a Raycast
	// "no-view" command. It runs off the UI loop, so it may block. A returned
	// error is shown as a failure toast.
	Run func(ctx Context) error

	// Open returns the screen of a command that has one, like a Raycast
	// "view" command.
	Open func(ctx Context) (View, error)
}

// Context is how a running command talks to goto. Its methods are safe to call
// from any goroutine.
type Context interface {
	// HUD closes goto and prints text in the terminal, like Raycast's showHUD.
	HUD(text string)

	// Close closes goto, like Raycast's closeMainWindow.
	Close()

	// Settings returns the plugin's settings, like Raycast's
	// getPreferenceValues.
	Settings() Settings

	// Argument returns the value the user gave for one of the command's
	// Arguments ("" when it was left empty).
	Argument(name string) string

	// Toast shows a message in the status line and returns a handle to
	// update it, like Raycast's showToast.
	Toast(style ToastStyle, title, message string) Toast

	// Confirm asks a yes/no question and blocks until the user answers, like
	// Raycast's confirmAlert.
	Confirm(c Confirmation) bool

	// Push opens a new screen on top of the current one.
	Push(v View)

	// Refresh reloads the list on screen, like revalidate in Raycast.
	Refresh()

	// Storage is the plugin's persistent key-value store, like Raycast's
	// LocalStorage.
	Storage() Storage

	// Filter returns the value chosen in the list's Filter ("" when the list
	// has none). Only meaningful inside List.Load.
	Filter() string

	// DataDir is a folder for the plugin's own files, like Raycast's
	// environment.supportPath. It is created on first use.
	DataDir() (string, error)

	// EditText opens text in the user's editor ($VISUAL, else $EDITOR, else
	// vi) in this terminal, with goto paused, and returns the edited text.
	// name becomes the temporary file's name, so its extension can pick the
	// editor's syntax highlighting.
	EditText(name, text string) (string, error)

	// OpenTerminal opens a terminal at dir, running command if it isn't
	// empty. Where it opens — a new tmux window, or a window of the terminal
	// gt runs in — follows the "terminal" setting.
	OpenTerminal(dir, command string) error
}

var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate reports every problem with a set of plugins: missing or malformed
// names, duplicates, missing titles, commands with nothing to run, and
// setting declarations that are incomplete or inconsistent. gt
// refuses to start if this returns an error, so mistakes in a plugin show up
// on the first run instead of as a confusing UI.
func Validate(plugins []Plugin) error {
	var errs []error
	seenPlugins := map[string]bool{}
	for i, p := range plugins {
		where := fmt.Sprintf("plugin %d (%q)", i, p.Name)
		if !kebab.MatchString(p.Name) {
			errs = append(errs, fmt.Errorf("%s: name must be kebab-case, like \"scratch-folders\"", where))
		}
		if seenPlugins[p.Name] {
			errs = append(errs, fmt.Errorf("%s: another plugin already uses this name", where))
		}
		seenPlugins[p.Name] = true
		if p.Title == "" {
			errs = append(errs, fmt.Errorf("%s: title is empty", where))
		}
		if len(p.Commands) == 0 {
			errs = append(errs, fmt.Errorf("%s: has no commands", where))
		}

		seenCommands := map[string]bool{}
		for j, c := range p.Commands {
			cwhere := fmt.Sprintf("%s command %d (%q)", where, j, c.Name)
			if !kebab.MatchString(c.Name) {
				errs = append(errs, fmt.Errorf("%s: name must be kebab-case, like \"create-scratch\"", cwhere))
			}
			if seenCommands[c.Name] {
				errs = append(errs, fmt.Errorf("%s: another command in this plugin already uses this name", cwhere))
			}
			seenCommands[c.Name] = true
			if c.Title == "" {
				errs = append(errs, fmt.Errorf("%s: title is empty", cwhere))
			}
			if (c.Run == nil) == (c.Open == nil) {
				errs = append(errs, fmt.Errorf("%s: set exactly one of Run and Open", cwhere))
			}
			seenArgs := map[string]bool{}
			for _, a := range c.Arguments {
				if !kebab.MatchString(a.Name) {
					errs = append(errs, fmt.Errorf("%s argument %q: name must be kebab-case", cwhere, a.Name))
				}
				if seenArgs[a.Name] {
					errs = append(errs, fmt.Errorf("%s argument %q: listed twice", cwhere, a.Name))
				}
				seenArgs[a.Name] = true
			}
		}
		errs = append(errs, validateSettings(where, p.Settings)...)
	}
	return errors.Join(errs...)
}
