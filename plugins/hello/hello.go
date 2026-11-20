// Package hello is the smallest possible goto plugin, ported from the Raycast
// hello-world extension. Copy it as a starting point for a new plugin.
package hello

import "github.com/jakeobsen/gt/kit"

// Plugin returns the hello-world plugin.
func Plugin() kit.Plugin {
	return kit.Plugin{
		Name:        "hello-world",
		Title:       "Hello World",
		Description: "Minimal seed plugin, used as a template for new plugins.",
		Commands: []kit.Command{
			{
				Name:        "say-hello",
				Title:       "Say Hello",
				Description: "Closes goto and prints a greeting.",
				Run: func(ctx kit.Context) error {
					ctx.HUD("Hello from goto!")
					return nil
				},
			},
		},
	}
}
