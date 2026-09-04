package domain

import "errors"

// Sentinel errors. The app layer and adapters map these to transport
// status codes; nothing outside this package should need to inspect
// error strings.
var (
	ErrInvalidEmail    = errors.New("invalid email address")
	ErrWeakPassword    = errors.New("password too short")
	ErrPasswordTooLong = errors.New("password too long")
	ErrEmailTaken      = errors.New("email already registered")

	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserSuspended      = errors.New("user is suspended")

	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")
)
