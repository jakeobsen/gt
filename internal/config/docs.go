package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jakeobsen/gt/kit"
)

// Reference renders every setting gt and the given plugins declare as the
// Markdown reference in docs/configuration.md.
func Reference(plugins []kit.Plugin) string {
	var b strings.Builder
	b.WriteString(`<!-- Generated from the setting declarations by "make docs". Edit the declarations, not this file. -->

# Configuration

gt reads its settings from a [TOML](https://toml.io) file. Every setting is optional: leave it out and gt uses the default.

## Where the file lives

gt uses the first of these that applies:

1. ` + "`$GT_CONFIG`" + `, if it is set.
2. ` + "`$XDG_CONFIG_HOME/gt/config.toml`" + `, if ` + "`XDG_CONFIG_HOME`" + ` is set.
3. ` + "`~/.config/gt/config.toml`" + `.

` + "`gt --config-path`" + ` prints the path gt is using. ` + "`gt --default-config`" + ` prints a complete file with every setting at its default, commented out — a good place to start:

` + "```sh" + `
mkdir -p ~/.config/gt
gt --default-config > ~/.config/gt/config.toml
` + "```" + `

## When something is wrong

gt checks the file every time it starts. If a key is misspelled, a value has the wrong type, or a choice isn't one of the allowed values, gt lists every problem by its setting name and doesn't open until they are fixed.

## Value types

| Type | How to write it | Example |
|---|---|---|
| true/false | ` + "`true` or `false`" + ` | ` + "`enabled = false`" + ` |
| whole number | digits, no quotes | ` + "`days = 7`" + ` |
| text | in double quotes | ` + "`command = \"lazygit\"`" + ` |
| path | in double quotes; a leading ` + "`~/`" + ` means your home directory | ` + "`folder = \"~/scratch\"`" + ` |
| choice | one of the listed values, in double quotes | ` + "`theme = \"dark\"`" + ` |

## General settings

These go at the top of the file, before any ` + "`[section]`" + `.

`)
	writeTable(&b, Core)

	b.WriteString("\n## Plugin settings\n\nEach plugin's settings go in its own `[plugins.<name>]` section. Every plugin has `enabled`.\n")
	for _, p := range plugins {
		fmt.Fprintf(&b, "\n### %s\n\n", p.Title)
		if p.Description != "" {
			fmt.Fprintf(&b, "%s\n\n", p.Description)
		}
		fmt.Fprintf(&b, "```toml\n[plugins.%s]\n```\n\n", p.Name)
		writeTable(&b, pluginSettings(p))
	}
	return b.String()
}

func writeTable(b *strings.Builder, settings []kit.Setting) {
	b.WriteString("| Setting | Type | Default | Description |\n|---|---|---|---|\n")
	for _, s := range settings {
		desc := s.Description
		if s.Kind == kit.Choice {
			var opts []string
			for _, o := range s.Options {
				opt := "`" + tomlValue(o.Value) + "`"
				if o.Description != "" {
					opt += ": " + o.Description
				}
				opts = append(opts, opt)
			}
			desc += " One of " + strings.Join(opts, "; ") + "."
		}
		fmt.Fprintf(b, "| `%s` | %s | `%s` | %s |\n", s.Key, s.Kind, tomlValue(s.Default), cell(desc))
	}
}

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}

// DefaultFile renders a complete config file with every setting commented out
// at its default value, ready to be edited.
func DefaultFile(plugins []kit.Plugin) string {
	var b strings.Builder
	b.WriteString(`# gt settings.
#
# Every setting is optional and shown here at its default. To change one,
# remove the "# " in front of it and edit the value. "gt --help" says where
# this file lives; docs/configuration.md in the gt source describes it fully.
`)
	writeSettings(&b, Core)
	for _, p := range plugins {
		b.WriteString("\n")
		title := p.Title
		if p.Description != "" {
			title += ": " + p.Description
		}
		writeComment(&b, title)
		fmt.Fprintf(&b, "[plugins.%s]\n", p.Name)
		writeSettings(&b, pluginSettings(p))
	}
	return b.String()
}

func writeSettings(b *strings.Builder, settings []kit.Setting) {
	for _, s := range settings {
		b.WriteString("\n")
		writeComment(b, s.Description)
		if s.Kind == kit.Choice {
			width := 0
			for _, o := range s.Options {
				width = max(width, len(tomlValue(o.Value)))
			}
			for _, o := range s.Options {
				v := tomlValue(o.Value)
				line := "  " + v
				if o.Description != "" {
					line += strings.Repeat(" ", width-len(v)) + "  " + o.Description
				}
				writeComment(b, line)
			}
		}
		fmt.Fprintf(b, "# %s = %s\n", s.Key, tomlValue(s.Default))
	}
}

// writeComment writes text as "# " lines wrapped at about 78 columns.
func writeComment(b *strings.Builder, text string) {
	if strings.HasPrefix(text, "  ") { // pre-formatted (an option line)
		b.WriteString("#" + text + "\n")
		return
	}
	line := "#"
	for _, w := range strings.Fields(text) {
		if len(line)+1+len(w) > 78 && line != "#" {
			b.WriteString(line + "\n")
			line = "#"
		}
		line += " " + w
	}
	b.WriteString(line + "\n")
}

// tomlValue writes v the way it would appear in a TOML file.
func tomlValue(v any) string {
	out, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "v = "))
}
