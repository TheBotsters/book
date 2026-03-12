package main

import (
	"flag"

	"github.com/TheBotsters/book/cli"
	impl "github.com/TheBotsters/book/cli/full-impl"
)

func main() {
	parser := &impl.Parser{
		AuthHandler:   func(c *cli.Config) { handleAuthCommand(c) },
		ReloadHandler: func(c *cli.Config) { handleReload(c) },
		ServerHandler: func(c *cli.Config) { runServer(c) },
	}

	flag.Usage = impl.Usage
	config, args := cli.ParseFlags(parser)

	if err := parser.HandleCommand(config, args); err != nil {
		panic(err)
	}
}
