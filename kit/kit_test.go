package kit

import (
	"strings"
	"testing"
)

func noop(Context) error { return nil }

func validPlugin(name string) Plugin {
	return Plugin{
		Name:  name,
		Title: "Example",
		Commands: []Command{
			{Name: "do-thing", Title: "Do Thing", Run: noop},
		},
	}
}

func TestValidateAcceptsWellFormedPlugins(t *testing.T) {
	if err := Validate([]Plugin{validPlugin("one"), validPlugin("two-words")}); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	bad := Plugin{
		Name: "Not Kebab",
		Commands: []Command{
			{Name: "same", Title: "A", Run: noop},
			{Name: "same", Title: "", Run: nil},
		},
	}
	err := Validate([]Plugin{bad, validPlugin("dup"), validPlugin("dup"), {Name: "empty", Title: "Empty"}})
	if err == nil {
		t.Fatal("Validate() = nil, want errors")
	}
	for _, want := range []string{
		"name must be kebab-case",
		"title is empty",
		"another command in this plugin already uses this name",
		"Run is not set",
		"another plugin already uses this name",
		"has no commands",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error is missing %q; got:\n%v", want, err)
		}
	}
}

func TestValidateChecksSettingDeclarations(t *testing.T) {
	p := validPlugin("with-settings")
	p.Settings = []Setting{
		{Key: "stale-days", Kind: Int, Default: 7, Description: "Days."},
		{Key: "root", Kind: Path, Default: "~/scratch", Description: "Where."},
		{Key: "mode", Kind: Choice, Default: "trash", Description: "How.", Options: []Option{{Value: "trash"}, {Value: "delete"}}},
	}
	if err := Validate([]Plugin{p}); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	p.Settings = []Setting{
		{Key: "Bad Key", Kind: Bool, Default: true, Description: "x"},
		{Key: "enabled", Kind: Bool, Default: true, Description: "x"},
		{Key: "dup", Kind: Bool, Default: true, Description: "x"},
		{Key: "dup", Kind: Bool, Default: true, Description: "x"},
		{Key: "no-description", Kind: Bool, Default: true},
		{Key: "wrong-default", Kind: Int, Default: "7", Description: "x"},
		{Key: "no-kind", Default: 1, Description: "x"},
		{Key: "no-options", Kind: Choice, Default: "a", Description: "x"},
		{Key: "bad-choice", Kind: Choice, Default: "c", Description: "x", Options: []Option{{Value: "a"}, {Value: "b"}}},
		{Key: "twice", Kind: Choice, Default: "a", Description: "x", Options: []Option{{Value: "a"}, {Value: "a"}}},
		{Key: "stray-options", Kind: String, Default: "", Description: "x", Options: []Option{{Value: "a"}}},
	}
	err := Validate([]Plugin{p})
	if err == nil {
		t.Fatal("Validate() = nil, want errors")
	}
	for _, want := range []string{
		"key must be kebab-case",
		`"enabled" is reserved`,
		"another setting already uses this key",
		"description is empty",
		`default "7" doesn't match kind whole number`,
		"kind is not set",
		"a choice setting needs options",
		`default "c" is not one of the options`,
		`option "a" is listed twice`,
		"only choice settings take options",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error is missing %q; got:\n%v", want, err)
		}
	}
}
