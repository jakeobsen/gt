// Command gt opens goto, a keyboard launcher for the terminal.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jakeobsen/gt/internal/app"
	"github.com/jakeobsen/gt/internal/config"
	"github.com/jakeobsen/gt/kit"
)

// version is set at build time.
var version = "dev"

const usage = `gt opens goto, a keyboard launcher for the terminal.

Usage:
  gt                    open the launcher
  gt --config-path      print where gt reads its settings
  gt --default-config   print a settings file with every setting at its default
  gt --version          print the version
  gt --help             print this help

In the launcher: type to search, ↑/↓ to move, enter to run, esc to go back.

Settings live in a TOML file, by default ~/.config/gt/config.toml
(override with $GT_CONFIG). To start one:

  mkdir -p ~/.config/gt
  gt --default-config > ~/.config/gt/config.toml`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	ps := plugins()
	if err := kit.Validate(ps); err != nil {
		fmt.Fprintf(stderr, "gt: a plugin is misconfigured:\n%v\n", err)
		return 2
	}

	if len(args) > 0 {
		switch args[0] {
		case "-v", "--version", "version":
			fmt.Fprintln(stdout, "gt", version)
		case "-h", "--help", "help":
			fmt.Fprintln(stdout, usage)
		case "--config-path":
			path, err := config.Path()
			if err != nil {
				fmt.Fprintln(stderr, "gt:", err)
				return 1
			}
			fmt.Fprintln(stdout, path)
		case "--default-config":
			fmt.Fprint(stdout, config.DefaultFile(ps))
		default:
			fmt.Fprintf(stderr, "gt: unknown argument %q\n\n%s\n", args[0], usage)
			return 2
		}
		return 0
	}

	path, err := config.Path()
	if err != nil {
		fmt.Fprintln(stderr, "gt:", err)
		return 1
	}
	cfg, err := config.Load(path, ps)
	if err != nil {
		fmt.Fprintln(stderr, "gt:", err)
		return 2
	}
	if err := app.Run(ps, cfg); err != nil {
		fmt.Fprintln(stderr, "gt:", err)
		return 1
	}
	return 0
}
