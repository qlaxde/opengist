package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thomiceli/opengist/internal/auth"
	"github.com/thomiceli/opengist/internal/db"
	webtest "github.com/thomiceli/opengist/internal/web/test"
)

func TestTryAuthenticationForGit(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	// "thomas" registers with a real DB password.
	s.Register(t, "thomas")

	// "oidcuser" is a passwordless user (simulating OIDC) — created directly.
	oidcUser := &db.User{Username: "oidcuser"}
	require.NoError(t, oidcUser.Create())

	makeToken := func(t *testing.T, userID uint, scope uint) string {
		tok := &db.AccessToken{Name: "t", UserID: userID, ScopeGist: scope}
		plain, err := tok.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tok.Create())
		return plain
	}

	t.Run("DbPasswordStillWorks", func(t *testing.T) {
		u, err := auth.TryAuthenticationForGit("thomas", "thomas", true)
		require.NoError(t, err)
		require.Equal(t, "thomas", u.Username)
	})

	t.Run("WritePATAcceptedForPush", func(t *testing.T) {
		token := makeToken(t, oidcUser.ID, db.ReadWritePermission)
		u, err := auth.TryAuthenticationForGit("oidcuser", token, true)
		require.NoError(t, err)
		require.Equal(t, "oidcuser", u.Username)
	})

	t.Run("ReadPATAcceptedForPull", func(t *testing.T) {
		token := makeToken(t, oidcUser.ID, db.ReadPermission)
		u, err := auth.TryAuthenticationForGit("oidcuser", token, false)
		require.NoError(t, err)
		require.Equal(t, "oidcuser", u.Username)
	})

	t.Run("ReadPATRejectedForPush", func(t *testing.T) {
		token := makeToken(t, oidcUser.ID, db.ReadPermission)
		_, err := auth.TryAuthenticationForGit("oidcuser", token, true)
		require.Error(t, err)
	})

	t.Run("NoPermissionPATRejected", func(t *testing.T) {
		token := makeToken(t, oidcUser.ID, db.NoPermission)
		_, err := auth.TryAuthenticationForGit("oidcuser", token, false)
		require.Error(t, err)
	})

	t.Run("WrongUsernamePATRejected", func(t *testing.T) {
		token := makeToken(t, oidcUser.ID, db.ReadWritePermission)
		_, err := auth.TryAuthenticationForGit("someone-else", token, true)
		require.Error(t, err)
	})

	t.Run("BogusPasswordRejected", func(t *testing.T) {
		_, err := auth.TryAuthenticationForGit("oidcuser", "og_not_a_real_token", true)
		require.Error(t, err)
	})
}
