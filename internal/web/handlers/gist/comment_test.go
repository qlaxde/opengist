package gist_test

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thomiceli/opengist/internal/db"
	webtest "github.com/thomiceli/opengist/internal/web/test"
)

func TestGistComments(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	s.Register(t, "thomas")
	s.Register(t, "alice")
	s.Register(t, "admin")

	// Promote admin.
	adminUser, err := db.GetUserByUsername("admin")
	require.NoError(t, err)
	adminUser.IsAdmin = true
	require.NoError(t, adminUser.Update())

	// thomas creates a public gist.
	_, gist, user, ident := s.CreateGist(t, "0")

	postComment := func(t *testing.T, who, content string, expect int) {
		s.Login(t, who)
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments", url.Values{
			"content": {content},
		}, expect)
	}

	t.Run("AnonymousCannotComment", func(t *testing.T) {
		s.Logout()
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments", url.Values{
			"content": {"nope"},
		}, 302) // logged middleware redirects to /all
		comments, err := db.GetCommentsByGistID(gist.ID)
		require.NoError(t, err)
		require.Len(t, comments, 0)
	})

	t.Run("LoggedUserCanComment", func(t *testing.T) {
		postComment(t, "thomas", "hello from thomas", 302)
		postComment(t, "alice", "hi from alice", 302)
		comments, err := db.GetCommentsByGistID(gist.ID)
		require.NoError(t, err)
		require.Len(t, comments, 2)
		require.Equal(t, "hello from thomas", comments[0].Content)
		require.Equal(t, "hi from alice", comments[1].Content)
	})

	t.Run("EmptyCommentRejected", func(t *testing.T) {
		s.Login(t, "thomas")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments", url.Values{
			"content": {"   "},
		}, 302)
		comments, _ := db.GetCommentsByGistID(gist.ID)
		require.Len(t, comments, 2)
	})

	t.Run("AuthorCanEdit", func(t *testing.T) {
		comments, _ := db.GetCommentsByGistID(gist.ID)
		thomasComment := comments[0]
		s.Login(t, "thomas")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(thomasComment.ID))+"/edit", url.Values{
			"content": {"edited by thomas"},
		}, 302)
		c, _ := db.GetCommentByID(thomasComment.ID)
		require.Equal(t, "edited by thomas", c.Content)
	})

	t.Run("NonAuthorCannotEdit", func(t *testing.T) {
		comments, _ := db.GetCommentsByGistID(gist.ID)
		thomasComment := comments[0]
		s.Login(t, "alice")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(thomasComment.ID))+"/edit", url.Values{
			"content": {"hacked"},
		}, 403)
		c, _ := db.GetCommentByID(thomasComment.ID)
		require.Equal(t, "edited by thomas", c.Content)
	})

	t.Run("NonAuthorCannotDelete", func(t *testing.T) {
		comments, _ := db.GetCommentsByGistID(gist.ID)
		thomasComment := comments[0]
		s.Login(t, "alice")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(thomasComment.ID))+"/delete", nil, 403)
	})

	t.Run("AuthorCanDelete", func(t *testing.T) {
		comments, _ := db.GetCommentsByGistID(gist.ID)
		aliceComment := comments[1]
		s.Login(t, "alice")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(aliceComment.ID))+"/delete", nil, 302)
		after, _ := db.GetCommentsByGistID(gist.ID)
		require.Len(t, after, 1)
	})

	t.Run("GistOwnerCanDeleteOthers", func(t *testing.T) {
		// alice posts again
		postComment(t, "alice", "alice again", 302)
		comments, _ := db.GetCommentsByGistID(gist.ID)
		var aliceID uint
		for _, c := range comments {
			if c.User.Username == "alice" {
				aliceID = c.ID
			}
		}
		require.NotZero(t, aliceID)
		s.Login(t, "thomas")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(aliceID))+"/delete", nil, 302)
	})

	t.Run("AdminCanDeleteOthers", func(t *testing.T) {
		postComment(t, "alice", "yet another", 302)
		comments, _ := db.GetCommentsByGistID(gist.ID)
		victim := comments[len(comments)-1]
		s.Login(t, "admin")
		s.Request(t, "POST", "/"+user+"/"+ident+"/comments/"+strconv.Itoa(int(victim.ID))+"/delete", nil, 302)
	})

	t.Run("DeletingGistCascadesComments", func(t *testing.T) {
		// At least one comment exists.
		postComment(t, "thomas", "to be cascaded", 302)
		s.Login(t, "thomas")
		s.Request(t, "POST", "/"+user+"/"+ident+"/delete", nil, 302)
		comments, err := db.GetCommentsByGistID(gist.ID)
		require.NoError(t, err)
		require.Len(t, comments, 0)
	})
}
