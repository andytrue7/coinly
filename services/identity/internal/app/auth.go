package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// DefaultRefreshTokenTTL is used when AuthConfig.RefreshTokenTTL is zero.
const DefaultRefreshTokenTTL = 30 * 24 * time.Hour

// AuthDeps are the outbound ports AuthService needs.
type AuthDeps struct {
	Users  UserRepository
	Tokens RefreshTokenRepository
	Hasher domain.PasswordHasher
	Issuer AccessTokenIssuer
	Clock  Clock
}

// AuthConfig tunes AuthService.
type AuthConfig struct {
	// RefreshTokenTTL is how long a freshly minted refresh token lives.
	// Zero selects DefaultRefreshTokenTTL.
	RefreshTokenTTL time.Duration
}

// TokenPair is what a client receives after any successful authentication.
type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// AuthResult is the outcome of Register and Login.
type AuthResult struct {
	User   *domain.User
	Tokens TokenPair
}

// AuthService implements the identity use cases: Register, Login,
// Refresh, Logout and GetUser.
type AuthService struct {
	users      UserRepository
	tokens     RefreshTokenRepository
	hasher     domain.PasswordHasher
	issuer     AccessTokenIssuer
	clock      Clock
	refreshTTL time.Duration
}

// NewAuthService wires an AuthService from its ports.
func NewAuthService(deps AuthDeps, cfg AuthConfig) *AuthService {
	ttl := cfg.RefreshTokenTTL
	if ttl <= 0 {
		ttl = DefaultRefreshTokenTTL
	}
	return &AuthService{
		users:      deps.Users,
		tokens:     deps.Tokens,
		hasher:     deps.Hasher,
		issuer:     deps.Issuer,
		clock:      deps.Clock,
		refreshTTL: ttl,
	}
}

// Register creates an active user and starts their first session.
// Validation errors (ErrInvalidEmail, ErrWeakPassword, ...) and
// ErrEmailTaken are returned unwrapped-by-sentinel for the adapter to map.
func (s *AuthService) Register(ctx context.Context, email, password string) (AuthResult, error) {
	now := s.clock.Now()

	user, err := domain.NewUser(email, password, s.hasher, now)
	if err != nil {
		return AuthResult{}, err
	}
	if err := s.users.Create(ctx, user); err != nil {
		return AuthResult{}, fmt.Errorf("create user: %w", err)
	}

	tokens, err := s.startSession(ctx, user, now)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Login authenticates by email + password and starts a new session.
// Unknown email, malformed email and wrong password all yield
// ErrInvalidCredentials so the response doesn't reveal which addresses
// are registered; a suspended account with the right password yields
// ErrUserSuspended.
func (s *AuthService) Login(ctx context.Context, email, password string) (AuthResult, error) {
	now := s.clock.Now()

	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return AuthResult{}, domain.ErrInvalidCredentials
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		// Burn the same KDF cost as a real verification so that response
		// time doesn't distinguish "no such user" from "wrong password".
		_, _ = s.hasher.Hash(password)
		return AuthResult{}, domain.ErrInvalidCredentials
	case err != nil:
		return AuthResult{}, fmt.Errorf("find user: %w", err)
	}

	if err := user.Authenticate(password, s.hasher); err != nil {
		return AuthResult{}, err
	}

	tokens, err := s.startSession(ctx, user, now)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Refresh exchanges a valid refresh token for a new token pair. The
// presented token is revoked (rotation). Presenting a token that was
// already revoked is treated as evidence of theft: every session for
// that user is revoked and ErrRefreshTokenRevoked is returned.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	now := s.clock.Now()

	token, err := s.tokens.FindByHash(ctx, domain.HashRefreshToken(refreshToken))
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenNotFound) {
			return TokenPair{}, err
		}
		return TokenPair{}, fmt.Errorf("find refresh token: %w", err)
	}

	if err := token.Validate(now); err != nil {
		if errors.Is(err, domain.ErrRefreshTokenRevoked) {
			if rerr := s.tokens.RevokeAllForUser(ctx, token.UserID, now); rerr != nil {
				return TokenPair{}, fmt.Errorf("revoke sessions after token reuse: %w", rerr)
			}
		}
		return TokenPair{}, err
	}

	user, err := s.users.FindByID(ctx, token.UserID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("find user: %w", err)
	}
	if user.Status == domain.UserStatusSuspended {
		return TokenPair{}, domain.ErrUserSuspended
	}

	token.Revoke(now)
	if err := s.tokens.Update(ctx, token); err != nil {
		return TokenPair{}, fmt.Errorf("revoke refresh token: %w", err)
	}

	return s.startSession(ctx, user, now)
}

// Logout revokes the session identified by refreshToken. It is
// idempotent: unknown or already-revoked tokens are not errors, so a
// client retrying a logout can't fail.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	now := s.clock.Now()

	token, err := s.tokens.FindByHash(ctx, domain.HashRefreshToken(refreshToken))
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenNotFound) {
			return nil
		}
		return fmt.Errorf("find refresh token: %w", err)
	}
	if token.RevokedAt != nil {
		return nil
	}

	token.Revoke(now)
	if err := s.tokens.Update(ctx, token); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

// GetUser returns the user with the given ID, or ErrUserNotFound.
func (s *AuthService) GetUser(ctx context.Context, id ids.ID) (*domain.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

// startSession mints a refresh token, persists it, and issues an access
// token for user.
func (s *AuthService) startSession(ctx context.Context, user *domain.User, now time.Time) (TokenPair, error) {
	refresh, secret, err := domain.NewRefreshToken(user.ID, now, s.refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.tokens.Create(ctx, refresh); err != nil {
		return TokenPair{}, fmt.Errorf("store refresh token: %w", err)
	}

	access, accessExp, err := s.issuer.Issue(user, now)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue access token: %w", err)
	}

	return TokenPair{
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     secret,
		RefreshExpiresAt: refresh.ExpiresAt,
	}, nil
}
