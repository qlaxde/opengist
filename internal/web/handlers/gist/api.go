package gist

import (
	"strings"

	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/web/context"
)

// API endpoints used by external tooling (e.g. the gist-push skill) that
// authenticates with a personal access token via the Authorization header.
// All responses are JSON.

type apiMetadata struct {
	Visibility  string   `json:"visibility"`
	Topics      []string `json:"topics"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
}

type apiMetadataUpdate struct {
	Visibility  *string  `json:"visibility,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
}

func APIGetMetadata(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	topics, err := gist.GetTopics()
	if err != nil {
		return ctx.ErrorRes(500, "could not read topics", err)
	}
	return ctx.JsonWithCode(200, apiMetadata{
		Visibility:  visibilityString(gist.Private),
		Topics:      topics,
		Title:       gist.Title,
		Description: gist.Description,
	})
}

func APIUpdateMetadata(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)

	var body apiMetadataUpdate
	if err := ctx.Bind(&body); err != nil {
		return ctx.ErrorRes(400, "invalid JSON body", err)
	}

	if body.Visibility != nil {
		gist.Private = db.ParseVisibility(*body.Visibility)
	}
	if body.Title != nil {
		gist.Title = strings.TrimSpace(*body.Title)
	}
	if body.Description != nil {
		gist.Description = strings.TrimSpace(*body.Description)
	}

	if body.Topics != nil {
		if err := db.ReplaceGistTopics(gist.ID, body.Topics); err != nil {
			return ctx.ErrorRes(500, "could not update topics", err)
		}
	}

	if err := gist.UpdateNoTimestamps(); err != nil {
		return ctx.ErrorRes(500, "could not update gist", err)
	}
	gist.AddInIndex()

	topics, _ := gist.GetTopics()
	return ctx.JsonWithCode(200, apiMetadata{
		Visibility:  visibilityString(gist.Private),
		Topics:      topics,
		Title:       gist.Title,
		Description: gist.Description,
	})
}

// APIListTopics returns the unique topics across all gists owned by the
// authenticated user, sorted alphabetically. Useful for the skill so it can
// surface "tags I've already used" rather than letting the user invent a new
// spelling every time.
func APIListTopics(ctx *context.Context) error {
	user := ctx.User
	if user == nil {
		return ctx.ErrorRes(401, "authentication required", nil)
	}
	topics, err := db.GetTopicsByUserID(user.ID)
	if err != nil {
		return ctx.ErrorRes(500, "could not list topics", err)
	}
	return ctx.JsonWithCode(200, map[string]any{"topics": topics})
}

func visibilityString(v db.Visibility) string {
	return v.String()
}
