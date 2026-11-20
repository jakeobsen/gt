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

	// Run performs a command that has no screen of its own, like a Raycast
	// "no-view" command. It runs off the UI goroutine, so it may block. A
	// returned error is shown in goto's status line.
	Run func(ctx Context) error
}

// Context is how a running command talks to goto. Its methods are safe to call
// from any goroutine.
type Context interface {
	// HUD closes goto and prints text in the terminal, like Raycast's showHUD.
	HUD(text string)

	// Settings returns the plugin's settings, like Raycast's
	// getPreferenceValues.
	Settings() Settings
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
			if c.Run == nil {
				errs = append(errs, fmt.Errorf("%s: Run is not set", cwhere))
			}
		}
		errs = append(errs, validateSettings(where, p.Settings)...)
	}
	return errors.Join(errs...)
}
