package main

import (
	"github.com/gotify/plugin-api"
)

// GetGotifyPluginInfo returns gotify plugin info
func GetGotifyPluginInfo() plugin.Info {
	return plugin.Info{
		Name:       "minimal plugin",
		ModulePath: "github.com/gotify/server/v2/example/minimal",
	}
}

// Plugin is plugin instance
type Plugin struct {
	enabled bool
}

// Enable implements plugin.Plugin
func (c *Plugin) Enable() error {
	c.enabled = true
	return nil
}

// Disable implements plugin.Plugin
func (c *Plugin) Disable() error {
	c.enabled = false
	return nil
}

// NewGotifyPluginInstance creates a plugin instance for a user context.
func NewGotifyPluginInstance(ctx plugin.UserContext) plugin.Plugin {
	return &Plugin{}
}

func main() {
	panic("this should be built as go plugin")
}
