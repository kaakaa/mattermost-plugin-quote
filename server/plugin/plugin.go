package plugin

import (
	"errors"
	"fmt"
	"sync"

	"github.com/blang/semver/v4"
	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

const minimumServerVersion = "5.18.0"

// SharePostPlugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type SharePostPlugin struct {
	plugin.MattermostPlugin
	router *mux.Router

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	ServerConfig *model.Config
}

// OnActivate initialize the plugin
func (p *SharePostPlugin) OnActivate() error {
	err := p.checkServerVersion()
	if err != nil {
		return err
	}

	if p.ServerConfig.ServiceSettings.SiteURL == nil {
		return errors.New("siteURL is not set. Please set a siteURL and restart the plugin")
	}

	p.router = p.InitAPI()
	return nil
}

func (p *SharePostPlugin) checkServerVersion() error {
	serverVersion, err := semver.Parse(p.API.GetServerVersion())
	if err != nil {
		return fmt.Errorf("failed to parse server version %w", err)
	}

	r := semver.MustParseRange(">=" + minimumServerVersion)
	if !r(serverVersion) {
		return fmt.Errorf("this plugin requires Mattermost v%s or later", minimumServerVersion)
	}
	return nil
}

// SendEphemeralPost send ephemeral post
func (p *SharePostPlugin) SendEphemeralPost(channelID, userID, message string) {
	ephemeralPost := &model.Post{
		ChannelId: channelID,
		UserId:    userID,
		Message:   message,
	}
	_ = p.API.SendEphemeralPost(userID, ephemeralPost)
}
