package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
	"github.com/pkg/errors"
)

const (
	toChannelKey      = "to_channel"
	shareTypeKey      = "share_type"
	additionalTextKey = "additional_text"

	shareTypeShare = "share"
	shareTypeMove  = "move"

	postPropsKeyAdditionalText = "sharepost.additional_text"
)

var messageGenericError = toPtr("Something went wrong. Please try again later.")

type submitDialogHandler func(map[string]string, *model.SubmitDialogRequest) (*string, *model.SubmitDialogResponse, error)

// InitAPI initialize API of the plugin
func (p *SharePostPlugin) InitAPI() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/", p.handleInfo).Methods(http.MethodGet)

	apiV1 := r.PathPrefix("/api/v1").Subrouter()
	apiV1.Use(checkAuthenticity)
	apiV1.HandleFunc("/share", p.handleSubmitDialogRequest(p.handleSharePost)).Methods(http.MethodPost)
	// apiV1.HandleFunc("/move", p.handleSubmitDialogRequest(p.handleMovePost).Methods(http.MethodPost)
	return r
}

// ServeHTTP handle a http request
func (p *SharePostPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.API.LogDebug("New request:", "Host", r.Host, "RequestURI", r.RequestURI, "Method", r.Method)
	p.router.ServeHTTP(w, r)
}

func (p *SharePostPlugin) handleInfo(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, fmt.Sprintf("Installed SharePostPlugin v%s", manifest.Version))
}

func checkAuthenticity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mattermost-User-ID") == "" {
			http.Error(w, "not authorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (p *SharePostPlugin) handleSubmitDialogRequest(handler submitDialogHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request := model.SubmitDialogRequestFromJson(r.Body)
		if request == nil {
			p.API.LogWarn("Failed to decode SubmitDialogRequest")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if request.UserId != r.Header.Get("Mattermost-User-Id") {
			p.API.LogWarn("invalid user")
			http.Error(w, "not authorized", http.StatusUnauthorized)
			return
		}

		msg, response, err := handler(mux.Vars(r), request)
		if err != nil {
			p.API.LogWarn("Failed to handle SubmitDialogRequest", "error", err.Error())
		}

		if msg != nil {
			p.SendEphemeralPost(request.ChannelId, request.UserId, *msg)
		}

		if response != nil {
			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(response)
			if err != nil {
				p.API.LogWarn("Failed to write SubmitDialogRequest", "error", err.Error())
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}
}

func (p *SharePostPlugin) handleSharePost(vars map[string]string, request *model.SubmitDialogRequest) (*string, *model.SubmitDialogResponse, error) {
	toChannel, ok := request.Submission[toChannelKey].(string)
	if !ok {
		return messageGenericError, nil, errors.Errorf("failed to get toChannel key. Value is: %v", request.Submission[toChannelKey])
	}
	shareType, ok := request.Submission[shareTypeKey].(string)
	if !ok {
		return messageGenericError, nil, errors.Errorf("failed to get shareType key. Value is: %v", request.Submission[shareTypeKey])
	}
	additionalText, ok := request.Submission[additionalTextKey].(string)
	if ok {
		additionalText = fmt.Sprintf("%s\n\n", additionalText)
	}

	switch shareType {
	case shareTypeShare:
		return p.sharePost(request, toChannel, additionalText)
	case shareTypeMove:
		return p.movePost(request, toChannel, additionalText)
	default:
		return messageGenericError, nil, fmt.Errorf("invalid share_type %s", shareType)
	}
}

func (p *SharePostPlugin) sharePost(request *model.SubmitDialogRequest, toChannel, additionalText string) (*string, *model.SubmitDialogResponse, error) {
	postID := request.CallbackId
	userID := request.UserId
	channelID := request.ChannelId
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.API.LogError("failed to get channel", "channel_id", channelID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get channel %w", appErr)
	}
	newChannel, appErr := p.API.GetChannel(toChannel)
	if appErr != nil {
		p.API.LogError("failed to get channel", "channel_id", toChannel, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get channel %w", appErr)
	}

	teamID := request.TeamId
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		p.API.LogError("failed to get team", "team_id", teamID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get team %w", appErr)
	}

	postList, appErr := p.API.GetPostThread(postID)
	if appErr != nil {
		p.API.LogError("failed to get post list", "post_id", postID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get post list %w", appErr)
	}
	p.API.LogDebug("ROOT: ", "post_id", postID)
	postList.UniqueOrder()

	newPost := &model.Post{
		Type:      model.POST_DEFAULT,
		UserId:    request.UserId,
		ChannelId: toChannel,
		Message:   fmt.Sprintf("> Shared from ~%s. ([original post](%s))", channel.Name, p.makePostLink(team.Name, postID)),
	}
	newPost.SetProps(model.StringInterface{postPropsKeyAdditionalText: additionalText})

	newPost, err := p.API.CreatePost(newPost)
	if err != nil {
		p.API.LogWarn("failed to create post", "error", err.Error())
		return messageGenericError, nil, fmt.Errorf("failed to create post %w", err)
	}
	p.SendEphemeralPost(channelID, userID, fmt.Sprintf("[This post](%s) is shared to ~%s. [New post](%s).", p.makePostLink(team.Name, postID), newChannel.Name, p.makePostLink(team.Name, newPost.Id)))
	return nil, nil, nil
}

func (p *SharePostPlugin) movePost(request *model.SubmitDialogRequest, toChannel, additionalText string) (*string, *model.SubmitDialogResponse, error) {
	postID := request.CallbackId
	userID := request.UserId
	teamID := request.TeamId

	postList, appErr := p.API.GetPostThread(postID)
	if appErr != nil {
		p.API.LogError("failed to get post list", "post_id", postID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get post list %w", appErr)
	}
	oldPost, appErr := p.API.GetPost(postID)
	if appErr != nil {
		p.API.LogError("failed to get post", "post_id", postID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get post %w", appErr)
	}

	// Cannot move any child posts in thread to other channel
	if len(postList.Posts) > 1 && oldPost.RootId != "" {
		p.API.LogWarn("the post that has parent posts cannot be moved to other channel.", "post_id", postID)
		return toPtr("the post that has parent posts cannot be moved to other channel."), nil, nil
	}
	// Cannot move the post to same channel
	if oldPost.ChannelId == toChannel {
		p.API.LogWarn("cannot move the post to same channel.")
		return toPtr("cannot move the post to same channel."), nil, nil
	}

	newChannel, appErr := p.API.GetChannel(toChannel)
	if appErr != nil {
		p.API.LogError("failed to get channel", "channel_id", toChannel, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get channel %w", appErr)
	}
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		p.API.LogError("failed to get team", "team_id", teamID, "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to get team %w", appErr)
	}

	// Create new post object
	newPost, err := p.clonePost(oldPost, userID)
	if err != nil {
		return messageGenericError, nil, fmt.Errorf("failed to clone post %w", err)
	}
	newPost.ChannelId = toChannel
	newPost.SetProps(model.StringInterface{postPropsKeyAdditionalText: additionalText})

	movedPost, appErr := p.API.CreatePost(newPost)
	if appErr != nil {
		p.API.LogWarn("failed to create post", "error", appErr.Error())
		return messageGenericError, nil, fmt.Errorf("failed to create post %w", appErr)
	}
	p.API.LogDebug("success to create new post", "original_post_id", postID, "moved_post_id", movedPost.Id)

	// Move children in thread
	createdPostIds := []string{movedPost.Id}
	willDeletePostIds := []string{}
	if len(postList.Posts) > 1 {
		postList.UniqueOrder()
		postList.SortByCreateAt()
		for _, id := range postList.Order {
			if id == postID {
				continue
			}
			p.API.LogDebug("start to move children in thread.", "post_id", id)
			oldChildPost, appErr := p.API.GetPost(id)
			if appErr != nil {
				p.API.LogWarn("failed to get post.", "post_id", oldChildPost.Id)
				if appErr = p.rollback(createdPostIds); appErr != nil {
					p.API.LogWarn("failed to rollback post thread")
					return messageGenericError, nil, fmt.Errorf("failed to create post thread: %s", appErr.Error())
				}
			}
			newChildPost, err := p.clonePost(oldChildPost, userID)
			if err != nil {
				if appErr = p.rollback(createdPostIds); appErr != nil {
					p.API.LogWarn("failed to rollback post thread")
					return messageGenericError, nil, fmt.Errorf("failed to create post thread: %w", err)
				}
			}
			newChildPost.ChannelId = toChannel
			newChildPost.RootId = movedPost.Id
			newChildPost.ParentId = movedPost.Id
			newCreatedChildPost, appErr := p.API.CreatePost(newChildPost)
			if appErr != nil {
				p.API.LogWarn("failed to update post.", "post_id", newChildPost.Id, "error", appErr.Error())
				if appErr = p.rollback(createdPostIds); appErr != nil {
					p.API.LogWarn("failed to rollback post thread")
					return messageGenericError, nil, fmt.Errorf("failed to create post thread and rollback: %s", appErr.Error())
				}
				return messageGenericError, nil, fmt.Errorf("failed to create post thread: %s", appErr.Error())
			}
			createdPostIds = append(createdPostIds, newCreatedChildPost.Id)
			willDeletePostIds = append(willDeletePostIds, id)
		}
		p.API.LogDebug("done moving thread.", "original_post_id", postID)
	}

	oldPost.Type = model.POST_SYSTEM_GENERIC
	oldPost.Message = fmt.Sprintf("Ttis post is moved to ~%s. [New post](%s)", newChannel.Name, p.makePostLink(team.Name, movedPost.Id))
	oldPost.FileIds = model.StringArray{}
	model.ParseSlackAttachment(oldPost, []*model.SlackAttachment{})
	oldPost.Metadata = &model.PostMetadata{}

	if _, appErr := p.API.UpdatePost(oldPost); appErr != nil {
		p.API.LogWarn("failed to update moved post.", "post_id", oldPost.Id, "error", appErr.Error())
	}

	for _, id := range willDeletePostIds {
		if appErr := p.API.DeletePost(id); appErr != nil {
			p.API.LogWarn("failed to delete post", "post_id", id)
		}
	}
	return nil, nil, nil
}

func (p *SharePostPlugin) clonePost(old *model.Post, userID string) (*model.Post, error) {
	// Create new post object
	newPost := old.Clone()
	newPost.Id = ""
	newPost.UpdateAt = time.Now().UnixNano()

	// Create the reference to attached files
	newFileIds, appErr := p.API.CopyFileInfos(userID, old.FileIds)
	if appErr != nil {
		p.API.LogWarn("failed to copy file ids", "error", appErr.Error())
		return nil, fmt.Errorf("failed to copy fie ids %w", appErr)
	}
	newPost.FileIds = newFileIds
	return newPost, nil
}

func (p *SharePostPlugin) makePostLink(teamName, postID string) string {
	return fmt.Sprintf("%s/%s/pl/%s", *p.ServerConfig.ServiceSettings.SiteURL, teamName, postID)
}

func (p *SharePostPlugin) rollback(ids []string) *model.AppError {
	for _, id := range ids {
		if appErr := p.API.DeletePost(id); appErr != nil {
			p.API.LogWarn("failed to delete post for rollback", "post_id", id, "error", appErr.Error())
			return appErr
		}
	}
	return nil
}

func toPtr(s string) *string {
	return &s
}
