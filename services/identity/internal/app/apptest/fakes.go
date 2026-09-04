// Package apptest provides in-memory implementations of the app layer's
// outbound ports for tests: the app package's own unit tests and any
// inbound adapter that wants a fully wired AuthService without Postgres.
package apptest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// Clock is a settable app.Clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// NewClock returns a Clock frozen at t.
func NewClock(t time.Time) *Clock { return &Clock{t: t} }

// Now implements app.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Hasher is a reversible, call-counting domain.PasswordHasher. Hashes are
// "fake:<password>", so tests can assert on what was stored without
// paying for argon2.
type Hasher struct {
	mu    sync.Mutex
	calls int
}

// Hash implements domain.PasswordHasher.
func (h *Hasher) Hash(password string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	return "fake:" + password, nil
}

// Verify implements domain.PasswordHasher.
func (h *Hasher) Verify(password, hash string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	return hash == "fake:"+password, nil
}

// Calls reports how many times Hash or Verify ran.
func (h *Hasher) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// Issuer is a fake app.AccessTokenIssuer producing readable, unsigned
// tokens. Set Err to make issuance fail.
type Issuer struct {
	TTL time.Duration
	Err error
}

// Issue implements app.AccessTokenIssuer.
func (i *Issuer) Issue(user *domain.User, now time.Time) (string, time.Time, error) {
	if i.Err != nil {
		return "", time.Time{}, i.Err
	}
	return fmt.Sprintf("access:%s:%d", user.ID, now.Unix()), now.Add(i.TTL), nil
}

// UserRepo is an in-memory app.UserRepository. Set Failing to make every
// call return that error.
type UserRepo struct {
	mu      sync.Mutex
	byID    map[ids.ID]*domain.User
	byEmail map[string]*domain.User
	Failing error
}

// NewUserRepo returns an empty UserRepo.
func NewUserRepo() *UserRepo {
	return &UserRepo{byID: map[ids.ID]*domain.User{}, byEmail: map[string]*domain.User{}}
}

// Create implements app.UserRepository.
func (r *UserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return r.Failing
	}
	if _, taken := r.byEmail[u.Email]; taken {
		return domain.ErrEmailTaken
	}
	cp := *u
	r.byID[u.ID] = &cp
	r.byEmail[u.Email] = &cp
	return nil
}

// FindByEmail implements app.UserRepository.
func (r *UserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return nil, r.Failing
	}
	u, ok := r.byEmail[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

// FindByID implements app.UserRepository.
func (r *UserRepo) FindByID(_ context.Context, id ids.ID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return nil, r.Failing
	}
	u, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

// Update implements app.UserRepository.
func (r *UserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return r.Failing
	}
	if _, ok := r.byID[u.ID]; !ok {
		return domain.ErrUserNotFound
	}
	cp := *u
	r.byID[u.ID] = &cp
	r.byEmail[u.Email] = &cp
	return nil
}

// Delete removes a user outright. Not part of the app port; it lets tests
// model "the user behind this token no longer exists".
func (r *UserRepo) Delete(id ids.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id]; ok {
		delete(r.byEmail, u.Email)
		delete(r.byID, id)
	}
}

// TokenRepo is an in-memory app.RefreshTokenRepository. Set Failing to
// make every call return that error.
type TokenRepo struct {
	mu      sync.Mutex
	byHash  map[string]*domain.RefreshToken
	Failing error
}

// NewTokenRepo returns an empty TokenRepo.
func NewTokenRepo() *TokenRepo {
	return &TokenRepo{byHash: map[string]*domain.RefreshToken{}}
}

// Create implements app.RefreshTokenRepository.
func (r *TokenRepo) Create(_ context.Context, t *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return r.Failing
	}
	if _, dup := r.byHash[t.TokenHash]; dup {
		return errors.New("apptest: duplicate token hash")
	}
	cp := *t
	r.byHash[t.TokenHash] = &cp
	return nil
}

// FindByHash implements app.RefreshTokenRepository.
func (r *TokenRepo) FindByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return nil, r.Failing
	}
	t, ok := r.byHash[hash]
	if !ok {
		return nil, domain.ErrRefreshTokenNotFound
	}
	cp := *t
	return &cp, nil
}

// Update implements app.RefreshTokenRepository.
func (r *TokenRepo) Update(_ context.Context, t *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return r.Failing
	}
	if _, ok := r.byHash[t.TokenHash]; !ok {
		return domain.ErrRefreshTokenNotFound
	}
	cp := *t
	r.byHash[t.TokenHash] = &cp
	return nil
}

// RevokeAllForUser implements app.RefreshTokenRepository.
func (r *TokenRepo) RevokeAllForUser(_ context.Context, userID ids.ID, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Failing != nil {
		return r.Failing
	}
	for _, t := range r.byHash {
		if t.UserID == userID {
			t.Revoke(now)
		}
	}
	return nil
}

// ActiveCount returns how many stored tokens for userID are valid at now.
func (r *TokenRepo) ActiveCount(userID ids.ID, now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.byHash {
		if t.UserID == userID && t.Validate(now) == nil {
			n++
		}
	}
	return n
}
