package main

import (
	"github.com/kaakaa/mattermost-plugin-sharepost/server/plugin"
	mmplugin "github.com/mattermost/mattermost-server/v5/plugin"
)

func main() {
	mmplugin.ClientMain(&plugin.SharePostPlugin{})
}
