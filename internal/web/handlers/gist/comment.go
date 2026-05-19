package gist

import (
	"strconv"
	"strings"

	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/web/context"
)

const maxCommentLength = 100_000

func CommentCreate(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	user := ctx.User

	content := strings.TrimSpace(ctx.FormValue("content"))
	if content == "" {
		ctx.AddFlash("Comment cannot be empty", "error")
		return ctx.RedirectTo(gistURL(gist) + "#comments")
	}
	if len(content) > maxCommentLength {
		return ctx.ErrorRes(400, "Comment is too long", nil)
	}

	comment := &db.GistComment{
		GistID:  gist.ID,
		UserID:  user.ID,
		Content: content,
	}
	if err := comment.Create(); err != nil {
		return ctx.ErrorRes(500, "Error creating comment", err)
	}

	return ctx.RedirectTo(gistURL(gist) + "#comments")
}

func CommentEdit(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	user := ctx.User

	comment, err := loadComment(ctx, gist.ID)
	if err != nil {
		return err
	}
	if comment.UserID != user.ID {
		return ctx.ErrorRes(403, "You can only edit your own comments", nil)
	}

	content := strings.TrimSpace(ctx.FormValue("content"))
	if content == "" {
		ctx.AddFlash("Comment cannot be empty", "error")
		return ctx.RedirectTo(gistURL(gist) + "#comments")
	}
	if len(content) > maxCommentLength {
		return ctx.ErrorRes(400, "Comment is too long", nil)
	}

	comment.Content = content
	if err := comment.Update(); err != nil {
		return ctx.ErrorRes(500, "Error updating comment", err)
	}
	return ctx.RedirectTo(gistURL(gist) + "#comments")
}

func CommentDelete(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	user := ctx.User

	comment, err := loadComment(ctx, gist.ID)
	if err != nil {
		return err
	}

	isAuthor := comment.UserID == user.ID
	isGistOwner := gist.UserID == user.ID
	isAdmin := user.IsAdmin
	if !isAuthor && !isGistOwner && !isAdmin {
		return ctx.ErrorRes(403, "You don't have permission to delete this comment", nil)
	}

	if err := comment.Delete(); err != nil {
		return ctx.ErrorRes(500, "Error deleting comment", err)
	}
	return ctx.RedirectTo(gistURL(gist) + "#comments")
}

func loadComment(ctx *context.Context, gistID uint) (*db.GistComment, error) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		return nil, ctx.NotFound("Comment not found")
	}
	comment, err := db.GetCommentByID(uint(id))
	if err != nil || comment.GistID != gistID {
		return nil, ctx.NotFound("Comment not found")
	}
	return comment, nil
}

func gistURL(gist *db.Gist) string {
	return "/" + gist.User.Username + "/" + gist.Identifier()
}
