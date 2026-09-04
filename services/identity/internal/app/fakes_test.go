package app_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// --- clock -----------------------------------------------------------------

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// --- password hasher ---------------------------------------------------------

// fakeHasher is reversible and counts calls so tests can assert the KDF
// ran (e.g. on the unknown-email login path).
type fakeHasher struct {
	mu          sync.Mutex
	hashCalls   int
	verifyCalls int
}

func (h *fakeHasher) Hash(password string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hashCalls++
	return "fake:" + password, nil
}

func (h *fakeHasher) Verify(password, hash string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.verifyCalls++
	return hash == "fake:"+password, nil
}

func (h *fakeHasher) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hashCalls + h.verifyCalls
}

// --- access token issuer ----------------------------------------------------

type fakeIssuer struct {
	ttl time.Duration
	err error
}

func (i *fakeIssuer) Issue(user *domain.User, now time.Time) (string, time.Time, error) {
	if i.err != nil {
		return "", time.Time{}, i.err
	}
	return fmt.Sprintf("access:%s:%d", user.ID, now.Unix()), now.Add(i.ttl), nil
}

// --- user repository ----------------------------------------------------------

type fakeUserRepo struct {
	mu      sync.Mutex
	byID    map[ids.ID]*domain.User
	byEmail map[string]*domain.User
	failing error // if set, every call returns this
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: map[ids.ID]*domain.User{}, byEmail: map[string]*domain.User{}}
}

func (r *fakeUserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return r.failing
	}
	if _, taken := r.byEmail[u.Email]; taken {
		return domain.ErrEmailTaken
	}
	cp := *u
	r.byID[u.ID] = &cp
	r.byEmail[u.Email] = &cp
	return nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return nil, r.failing
	}
	u, ok := r.byEmail[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeUserRepo) FindByID(_ context.Context, id ids.ID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return nil, r.failing
	}
	u, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeUserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return r.failing
	}
	if _, ok := r.byID[u.ID]; !ok {
		return domain.ErrUserNotFound
	}
	cp := *u
	r.byID[u.ID] = &cp
	r.byEmail[u.Email] = &cp
	return nil
}

// --- refresh token repository -------------------------------------------------

type fakeTokenRepo struct {
	mu      sync.Mutex
	byHash  map[string]*domain.RefreshToken
	failing error
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{byHash: map[string]*domain.RefreshToken{}}
}

func (r *fakeTokenRepo) Create(_ context.Context, t *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return r.failing
	}
	if _, dup := r.byHash[t.TokenHash]; dup {
		return errors.New("duplicate token hash")
	}
	cp := *t
	r.byHash[t.TokenHash] = &cp
	return nil
}

func (r *fakeTokenRepo) FindByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return nil, r.failing
	}
	t, ok := r.byHash[hash]
	if !ok {
		return nil, domain.ErrRefreshTokenNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTokenRepo) Update(_ context.Context, t *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return r.failing
	}
	if _, ok := r.byHash[t.TokenHash]; !ok {
		return domain.ErrRefreshTokenNotFound
	}
	cp := *t
	r.byHash[t.TokenHash] = &cp
	return nil
}

func (r *fakeTokenRepo) RevokeAllForUser(_ context.Context, userID ids.ID, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing != nil {
		return r.failing
	}
	for _, t := range r.byHash {
		if t.UserID == userID {
			t.Revoke(now)
		}
	}
	return nil
}

// activeCount returns how many stored tokens for userID are still valid at now.
func (r *fakeTokenRepo) activeCount(userID ids.ID, now time.Time) int {
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

// --- harness ------------------------------------------------------------------

type harness struct {
	svc    *app.AuthService
	clock  *fakeClock
	hasher *fakeHasher
	issuer *fakeIssuer
	users  *fakeUserRepo
	tokens *fakeTokenRepo
}

const (
	accessTTL  = 15 * time.Minute
	refreshTTL = 30 * 24 * time.Hour
)

var t0 = time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)

func newHarness() *harness {
	h := &harness{
		clock:  &fakeClock{t: t0},
		hasher: &fakeHasher{},
		issuer: &fakeIssuer{ttl: accessTTL},
		users:  newFakeUserRepo(),
		tokens: newFakeTokenRepo(),
	}
	h.svc = app.NewAuthService(app.AuthDeps{
		Users:  h.users,
		Tokens: h.tokens,
		Hasher: h.hasher,
		Issuer: h.issuer,
		Clock:  h.clock,
	}, app.AuthConfig{RefreshTokenTTL: refreshTTL})
	return h
}
