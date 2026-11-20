package main

import (
	"github.com/jakeobsen/gt/kit"
	"github.com/jakeobsen/gt/plugins/hello"
	"github.com/jakeobsen/gt/plugins/scratch"
)

// plugins lists every plugin compiled into gt. To add one, create a package
// under plugins/ and add a line here.
func plugins() []kit.Plugin {
	return []kit.Plugin{
		hello.Plugin(),
		scratch.Plugin(),
	}
}
