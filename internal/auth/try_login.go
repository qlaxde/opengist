package auth

import (
	"errors"

	"github.com/rs/zerolog/log"
	"github.com/thomiceli/opengist/internal/auth/ldap"
	passwordpkg "github.com/thomiceli/opengist/internal/auth/password"
	"github.com/thomiceli/opengist/internal/db"
	"gorm.io/gorm"
)

type AuthError struct {
	message string
}

func (e AuthError) Error() string {
	return e.message
}

func TryAuthentication(username, password string) (*db.User, error) {
	user, err := db.GetUserByUsername(username)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Error().Err(err).Msgf("Cannot get user by username %s", username)
			return nil, err
		}
	}

	if user.Password != "" {
		return tryDbLogin(user, password)
	} else {
		if ldap.Enabled() {
			return tryLdapLogin(username, password)
		}
		return nil, AuthError{"no authentication method available"}
	}
}

// TryAuthenticationForGit authenticates a git HTTP request. It accepts the same
// credentials as TryAuthentication (DB password, LDAP), and additionally accepts
// a personal access token in the password field — provided the token's scope
// covers the operation (read for pull, write for push).
func TryAuthenticationForGit(username, password string, write bool) (*db.User, error) {
	user, err := db.GetUserByUsername(username)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Error().Err(err).Msgf("Cannot get user by username %s", username)
		return nil, err
	}

	// Try DB password first if one is set, but fall through to PAT on failure.
	// Without the fall-through, an account with a stale local password could
	// never use a PAT for git auth — which is exactly the OIDC-only case this
	// function exists to support.
	if user.Password != "" {
		if u, derr := tryDbLogin(user, password); derr == nil {
			return u, nil
		}
	}
	if ldap.Enabled() {
		if u, lerr := tryLdapLogin(username, password); lerr == nil {
			return u, nil
		}
	}

	token, err := db.GetAccessTokenByToken(password)
	if err != nil || token.User.Username != username || token.IsExpired() {
		return nil, AuthError{"invalid credentials"}
	}
	if write && !token.HasGistWritePermission() {
		return nil, AuthError{"access token lacks write permission"}
	}
	if !write && !token.HasGistReadPermission() {
		return nil, AuthError{"access token lacks read permission"}
	}
	_ = token.UpdateLastUsed()
	return &token.User, nil
}


func tryDbLogin(user *db.User, password string) (*db.User, error) {
	if ok, err := passwordpkg.VerifyPassword(password, user.Password); !ok {
		if err != nil {
			log.Error().Err(err).Msg("Password verification failed")
			return nil, err
		}
		return nil, AuthError{"invalid password"}
	}

	return user, nil
}

func tryLdapLogin(username, password string) (user *db.User, err error) {
	ok, err := ldap.Authenticate(username, password)
	if err != nil {
		log.Error().Err(err).Msg("LDAP authentication failed")
		return nil, err
	}

	if !ok {
		return nil, AuthError{"invalid LDAP credentials"}
	}

	if user, err = db.GetUserByUsername(username); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Error().Err(err).Msgf("Cannot get user by username %s", username)
			return nil, err
		}
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = &db.User{
			Username: username,
		}
		if err = user.Create(); err != nil {
			log.Warn().Err(err).Msg("Cannot create user after LDAP authentication")
			return nil, err
		}

		return user, nil
	}

	return user, nil
}
