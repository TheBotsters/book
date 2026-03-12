package main

import (
	"path/filepath"

	"github.com/TheBotsters/book/adminifier"
	"github.com/TheBotsters/book/cli"
	"github.com/TheBotsters/book/webserver"
)

func runServer(c *cli.Config) {
	// setup server options from config
	opts := webserver.Options{
		Config:   c.Config,
		WikisDir: filepath.Join(c.QuikiDir, "wikis"),
		Bind:     c.Bind,
		Port:     c.Port,
		Host:     c.Host,
	}

	// if running wizard, create a new config file
	if c.Wizard {
		webserver.CreateWizardConfig(opts)
	}

	// run webserver
	webserver.Configure(opts)
	adminifier.Configure()
	writePIDFile(c)

	// handle SIGHUP to rehash server config
	go handleSignals()
	webserver.Listen()
}
