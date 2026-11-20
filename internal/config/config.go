// Package config loads gt's settings file and checks it against the settings
// gt and its plugins declare. It also renders those declarations as
// documentation and as a commented default file (see docs.go).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jakeobsen/gt/internal/terminal"
	"github.com/jakeobsen/gt/kit"
)

// EnvPath is the environment variable that overrides where the file lives.
const EnvPath = "GT_CONFIG"

// pluginsTable is the top-level table that holds one sub-table per plugin.
const pluginsTable = "plugins"

// Core declares gt's own settings: top-level keys in the file.
var Core = []kit.Setting{
	{
		Key:         "theme",
		Kind:        kit.Choice,
		Default:     "auto",
		Description: "Colour scheme.",
		Options: []kit.Option{
			{Value: "auto", Description: "follow the terminal's background colour"},
			{Value: "dark", Description: "colours for a dark background"},
			{Value: "light", Description: "colours for a light background"},
		},
	},
	{
		Key:         "terminal",
		Kind:        kit.Choice,
		Default:     terminal.Auto,
		Description: "Where actions like Open in Terminal open a terminal.",
		Options:     terminal.Options,
	},
}

// enabled is the setting every plugin gets.
var enabled = kit.Setting{
	Key:         kit.ReservedSettingKey,
	Kind:        kit.Bool,
	Default:     true,
	Description: "Show this plugin's commands in the search.",
}

// pluginSettings is what a plugin's section may contain: enabled first, then
// the plugin's own declarations.
func pluginSettings(p kit.Plugin) []kit.Setting {
	return append([]kit.Setting{enabled}, p.Settings...)
}

// Path returns where gt reads its settings: $GT_CONFIG if set, otherwise
// $XDG_CONFIG_HOME/gt/config.toml, otherwise ~/.config/gt/config.toml.
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "gt", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the config file: %w", err)
	}
	return filepath.Join(home, ".config", "gt", "config.toml"), nil
}

// Config holds every setting's value: from the file where set, otherwise the
// declared default.
type Config struct {
	core    values
	plugins map[string]values
	decls   map[string][]kit.Setting // per plugin, including enabled
}

type values map[string]any

// FileError lists every problem found in a config file, so they can all be
// fixed in one go.
type FileError struct {
	Path     string
	Problems []string
}

func (e *FileError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "problems in %s:", e.Path)
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(strings.ReplaceAll(strings.TrimSpace(p), "\n", "\n    "))
	}
	return b.String()
}

// Load reads the file at path and checks it against the declarations of gt
// and the given plugins. A missing file is fine: every setting then has its
// default. The plugins must already have passed kit.Validate.
func Load(path string, plugins []kit.Plugin) (*Config, error) {
	raw := map[string]any{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Defaults(plugins), nil
		}
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return nil, &FileError{Path: path, Problems: []string{perr.ErrorWithPosition()}}
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg, problems := build(raw, plugins)
	if len(problems) > 0 {
		return nil, &FileError{Path: path, Problems: problems}
	}
	return cfg, nil
}

// Defaults returns the configuration used when there is no file.
func Defaults(plugins []kit.Plugin) *Config {
	cfg, _ := build(map[string]any{}, plugins)
	return cfg
}

// build resolves every declared setting from raw (decoded TOML), collecting a
// readable message for each problem.
func build(raw map[string]any, plugins []kit.Plugin) (*Config, []string) {
	cfg := &Config{plugins: map[string]values{}, decls: map[string][]kit.Setting{}}
	var problems []string

	coreRaw := map[string]any{}
	pluginsRaw := map[string]any{}
	for k, v := range raw {
		if k != pluginsTable {
			coreRaw[k] = v
			continue
		}
		t, ok := v.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: must be a table, like [plugins.hello-world] (got %s)", k, describe(v)))
			continue
		}
		pluginsRaw = t
	}
	coreKeys := append(keysOf(Core), pluginsTable)
	cfg.core, problems = resolve("", Core, coreRaw, coreKeys, problems)

	var names []string
	for _, p := range plugins {
		names = append(names, p.Name)
		decls := pluginSettings(p)
		cfg.decls[p.Name] = decls
		section := map[string]any{}
		if v, ok := pluginsRaw[p.Name]; ok {
			t, isTable := v.(map[string]any)
			if !isTable {
				problems = append(problems, fmt.Sprintf("plugins.%s: must be a table, like [plugins.%s] (got %s)", p.Name, p.Name, describe(v)))
			} else {
				section = t
			}
		}
		cfg.plugins[p.Name], problems = resolve("plugins."+p.Name+".", decls, section, keysOf(decls), problems)
	}
	for _, name := range sortedKeys(pluginsRaw) {
		if !slices.Contains(names, name) {
			problems = append(problems, unknown("plugins."+name, "plugin", name, names))
		}
	}
	return cfg, problems
}

// resolve checks one table against its declarations and returns every
// declared setting's value. prefix is the table's dotted path for messages;
// allowed lists the keys that may appear (declarations plus any sub-tables).
func resolve(prefix string, decls []kit.Setting, raw map[string]any, allowed []string, problems []string) (values, []string) {
	out := values{}
	for _, s := range decls {
		v, set := raw[s.Key]
		if !set {
			out[s.Key] = normalize(s, s.Default)
			continue
		}
		val, err := coerce(s, v)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s%s: %v", prefix, s.Key, err))
			out[s.Key] = normalize(s, s.Default)
			continue
		}
		out[s.Key] = val
	}
	for _, k := range sortedKeys(raw) {
		if !slices.Contains(allowed, k) {
			problems = append(problems, unknown(prefix+k, "setting", k, allowed))
		}
	}
	return out, problems
}

// coerce checks a decoded TOML value against a declaration and converts it to
// the Go type plugins read.
func coerce(s kit.Setting, v any) (any, error) {
	switch s.Kind {
	case kit.Bool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("must be true or false (got %s)", describe(v))
	case kit.Int:
		if n, ok := v.(int64); ok && n >= math.MinInt && n <= math.MaxInt {
			return int(n), nil
		}
		return nil, fmt.Errorf("must be a whole number, without quotes (got %s)", describe(v))
	case kit.String, kit.Path:
		if str, ok := v.(string); ok {
			return normalize(s, str), nil
		}
		return nil, fmt.Errorf("must be text in double quotes (got %s)", describe(v))
	case kit.Choice:
		var allowed []string
		for _, o := range s.Options {
			allowed = append(allowed, fmt.Sprintf("%q", o.Value))
		}
		str, ok := v.(string)
		if !ok || !slices.ContainsFunc(s.Options, func(o kit.Option) bool { return o.Value == str }) {
			return nil, fmt.Errorf("must be one of %s (got %s)", strings.Join(allowed, ", "), describe(v))
		}
		return str, nil
	}
	return nil, fmt.Errorf("has an unknown kind %v", s.Kind)
}

// normalize turns a declared default into the value plugins see: Path values
// get "~/" expanded.
func normalize(s kit.Setting, v any) any {
	if s.Kind == kit.Path {
		return expandHome(v.(string))
	}
	return v
}

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// describe shows a decoded TOML value in an error message.
func describe(v any) string {
	switch v := v.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	case map[string]any:
		return "a table"
	case []any, []map[string]any:
		return "a list"
	}
	return fmt.Sprintf("%v", v)
}

// unknown builds the message for a key nobody declared, suggesting the
// closest known key when one is near enough to be a typo.
func unknown(path, what, key string, known []string) string {
	msg := fmt.Sprintf("%s: unknown %s", path, what)
	best, bestDist := "", math.MaxInt
	for _, k := range known {
		if d := editDistance(key, k); d < bestDist {
			best, bestDist = k, d
		}
	}
	if best != "" && bestDist <= max(1, len(key)/3) {
		msg += fmt.Sprintf(" (did you mean %q?)", best)
	}
	return msg
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func keysOf(decls []kit.Setting) []string {
	var out []string
	for _, s := range decls {
		out = append(out, s.Key)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Theme returns the theme setting: "auto", "dark" or "light".
func (c *Config) Theme() string { return c.core["theme"].(string) }

// Terminal returns the terminal setting, one of the terminal package's
// strategies.
func (c *Config) Terminal() string { return c.core["terminal"].(string) }

// Enabled reports whether a plugin's commands should appear in the search.
func (c *Config) Enabled(plugin string) bool {
	v, ok := c.plugins[plugin][kit.ReservedSettingKey].(bool)
	return !ok || v
}

// Plugin returns one plugin's settings for its commands to read.
func (c *Config) Plugin(name string) kit.Settings {
	return pluginValues{plugin: name, decls: c.decls[name], vals: c.plugins[name]}
}

// pluginValues implements kit.Settings. Misuse panics: it is a bug in the
// plugin, and gt reports plugin panics as errors.
type pluginValues struct {
	plugin string
	decls  []kit.Setting
	vals   values
}

func (p pluginValues) get(key string, kinds ...kit.SettingKind) any {
	i := slices.IndexFunc(p.decls, func(s kit.Setting) bool { return s.Key == key })
	if i < 0 {
		panic(fmt.Sprintf("plugin %q reads setting %q, which it doesn't declare", p.plugin, key))
	}
	if !slices.Contains(kinds, p.decls[i].Kind) {
		panic(fmt.Sprintf("plugin %q reads setting %q as the wrong type: it is a %s setting", p.plugin, key, p.decls[i].Kind))
	}
	return p.vals[key]
}

func (p pluginValues) Bool(key string) bool { return p.get(key, kit.Bool).(bool) }

func (p pluginValues) Int(key string) int { return p.get(key, kit.Int).(int) }

func (p pluginValues) String(key string) string {
	return p.get(key, kit.String, kit.Path, kit.Choice).(string)
}
