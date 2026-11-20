package kit

import (
	"errors"
	"fmt"
	"slices"
)

// SettingKind is the type of value a setting holds.
type SettingKind int

const (
	// Bool is true or false.
	Bool SettingKind = iota + 1
	// Int is a whole number.
	Int
	// String is free text.
	String
	// Path is a file or directory path. A leading "~/" means the home
	// directory; plugins receive the expanded path.
	Path
	// Choice is one of a fixed set of strings, listed in Setting.Options.
	Choice
)

// String names the kind the way the docs do.
func (k SettingKind) String() string {
	switch k {
	case Bool:
		return "true/false"
	case Int:
		return "whole number"
	case String:
		return "text"
	case Path:
		return "path"
	case Choice:
		return "choice"
	}
	return fmt.Sprintf("SettingKind(%d)", int(k))
}

// Setting declares one value users can set in the config file, like a
// Raycast preference. gt documents every declared setting and refuses to
// start when the config file sets an unknown key or a value of the wrong type.
type Setting struct {
	// Key is the setting's name in the config file, in kebab-case.
	Key string

	// Kind is the type of value the setting holds.
	Kind SettingKind

	// Default is used when the config file doesn't set the key. Its Go type
	// must match Kind: bool for Bool, int for Int, string for the rest.
	Default any

	// Description says what the setting does. It is copied into the docs and
	// the default config file, so write it for users.
	Description string

	// Options are the allowed values of a Choice setting.
	Options []Option
}

// Option is one allowed value of a Choice setting.
type Option struct {
	Value       string
	Description string
}

// Settings reads a plugin's settings. Values have already been checked
// against the plugin's declarations, so reading one can't fail; asking for an
// undeclared key, or for the wrong type, is a bug in the plugin and panics.
type Settings interface {
	Bool(key string) bool
	Int(key string) int
	// String returns a String, Path or Choice setting.
	String(key string) string
}

// ReservedSettingKey is the setting every plugin gets automatically: whether
// its commands appear in the search. Plugins can't declare it themselves.
const ReservedSettingKey = "enabled"

// validateSettings reports every problem with a list of declarations. where
// prefixes each message, e.g. `plugin 0 ("scratch")`.
func validateSettings(where string, settings []Setting) []error {
	var errs []error
	seen := map[string]bool{}
	for i, s := range settings {
		at := fmt.Sprintf("%s setting %d (%q)", where, i, s.Key)
		switch {
		case !kebab.MatchString(s.Key):
			errs = append(errs, fmt.Errorf("%s: key must be kebab-case, like \"stale-days\"", at))
		case s.Key == ReservedSettingKey:
			errs = append(errs, fmt.Errorf("%s: %q is reserved; every plugin gets it automatically", at, s.Key))
		case seen[s.Key]:
			errs = append(errs, fmt.Errorf("%s: another setting already uses this key", at))
		}
		seen[s.Key] = true
		if s.Description == "" {
			errs = append(errs, fmt.Errorf("%s: description is empty", at))
		}
		if err := checkDefault(s); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", at, err))
		}
	}
	return errs
}

func checkDefault(s Setting) error {
	var ok bool
	switch s.Kind {
	case Bool:
		_, ok = s.Default.(bool)
	case Int:
		_, ok = s.Default.(int)
	case String, Path:
		_, ok = s.Default.(string)
	case Choice:
		return checkChoice(s)
	default:
		return fmt.Errorf("kind is not set")
	}
	if !ok {
		return fmt.Errorf("default %#v doesn't match kind %s", s.Default, s.Kind)
	}
	if len(s.Options) > 0 {
		return errors.New("only choice settings take options")
	}
	return nil
}

func checkChoice(s Setting) error {
	if len(s.Options) == 0 {
		return errors.New("a choice setting needs options")
	}
	var values []string
	for _, o := range s.Options {
		if o.Value == "" {
			return errors.New("an option has an empty value")
		}
		if slices.Contains(values, o.Value) {
			return fmt.Errorf("option %q is listed twice", o.Value)
		}
		values = append(values, o.Value)
	}
	d, ok := s.Default.(string)
	if !ok || !slices.Contains(values, d) {
		return fmt.Errorf("default %#v is not one of the options", s.Default)
	}
	return nil
}
