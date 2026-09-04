// Package httpapi is the identity service's REST adapter. It translates
// HTTP requests into app.AuthService calls and domain errors into status
// codes; no business rules live here.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/andytrue7/coinly/pkg/httpx"
	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/pkg/jwtx"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// maxBodyBytes bounds every JSON request body; auth payloads are tiny.
const maxBodyBytes = 8 << 10

// jwksMaxAge is how long clients may cache the JWKS. Key rotation must
// publish the new key at least this long before signing with it.
const jwksMaxAge = 5 * time.Minute

// Deps are the collaborators the handler needs.
type Deps struct {
	Auth     *app.AuthService
	JWKS     jwtx.JWKS
	Verifier httpx.TokenVerifier
	Clock    app.Clock
	Logger   *slog.Logger
}

type handler struct {
	auth  *app.AuthService
	jwks  jwtx.JWKS
	clock app.Clock
	log   *slog.Logger
}

// NewHandler builds the service's HTTP routes.
func NewHandler(d Deps) http.Handler {
	h := &handler{auth: d.Auth, jwks: d.JWKS, clock: d.Clock, log: d.Logger}
	if h.log == nil {
		h.log = slog.Default()
	}
	requireAuth := httpx.RequireAuth(d.Verifier, d.Clock.Now)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/register", h.register)
	mux.HandleFunc("POST /v1/auth/login", h.login)
	mux.HandleFunc("POST /v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /v1/auth/logout", h.logout)
	mux.Handle("GET /v1/users/me", requireAuth(http.HandlerFunc(h.me)))
	mux.HandleFunc("GET /.well-known/jwks.json", h.jwksHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

// --- request / response shapes ------------------------------------------------------

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// UserResponse is the public view of a user.
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// TokensResponse carries an access/refresh token pair.
type TokensResponse struct {
	TokenType             string    `json:"token_type"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

// AuthResponse is returned by register and login.
type AuthResponse struct {
	User   UserResponse   `json:"user"`
	Tokens TokensResponse `json:"tokens"`
}

// RefreshResponse is returned by refresh.
type RefreshResponse struct {
	Tokens TokensResponse `json:"tokens"`
}

func toUser(u *domain.User) UserResponse {
	return UserResponse{ID: u.ID.String(), Email: u.Email, Status: u.Status.String(), CreatedAt: u.CreatedAt}
}

func toTokens(tp app.TokenPair) TokensResponse {
	return TokensResponse{
		TokenType:             "Bearer",
		AccessToken:           tp.AccessToken,
		AccessTokenExpiresAt:  tp.AccessExpiresAt,
		RefreshToken:          tp.RefreshToken,
		RefreshTokenExpiresAt: tp.RefreshExpiresAt,
	}
}

// --- handlers ---------------------------------------------------------------------------

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := httpx.DecodeJSON(w, r, &req, maxBodyBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	res, err := h.auth.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AuthResponse{User: toUser(res.User), Tokens: toTokens(res.Tokens)})
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := httpx.DecodeJSON(w, r, &req, maxBodyBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	res, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AuthResponse{User: toUser(res.User), Tokens: toTokens(res.Tokens)})
}

func (h *handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req, maxBodyBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.RefreshToken == "" {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "refresh_token is required")
		return
	}

	tokens, err := h.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, RefreshResponse{Tokens: toTokens(tokens)})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req, maxBodyBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	sub, _ := httpx.Subject(r.Context())
	id, err := ids.Parse(sub)
	if err != nil {
		// A token we signed with a non-UUID subject is our bug, but to the
		// client it's simply not a usable credential.
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid token subject")
		return
	}

	u, err := h.auth.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "user no longer exists")
			return
		}
		h.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUser(u))
}

func (h *handler) jwksHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(w, http.StatusOK, h.jwks)
}

// --- error mapping ---------------------------------------------------------------------

// writeError maps domain sentinels to HTTP responses. Anything unmapped
// is an infrastructure failure: logged in full, returned as an opaque 500.
func (h *handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	type mapping struct {
		target error
		status int
		code   string
	}
	mappings := []mapping{
		{domain.ErrInvalidEmail, http.StatusBadRequest, "invalid_email"},
		{domain.ErrWeakPassword, http.StatusBadRequest, "weak_password"},
		{domain.ErrPasswordTooLong, http.StatusBadRequest, "password_too_long"},
		{domain.ErrEmailTaken, http.StatusConflict, "email_taken"},
		{domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{domain.ErrUserSuspended, http.StatusForbidden, "user_suspended"},
		{domain.ErrRefreshTokenNotFound, http.StatusUnauthorized, "invalid_refresh_token"},
		{domain.ErrRefreshTokenExpired, http.StatusUnauthorized, "invalid_refresh_token"},
		{domain.ErrRefreshTokenRevoked, http.StatusUnauthorized, "invalid_refresh_token"},
		{domain.ErrUserNotFound, http.StatusNotFound, "user_not_found"},
	}
	for _, m := range mappings {
		if errors.Is(err, m.target) {
			httpx.WriteError(w, m.status, m.code, m.target.Error())
			return
		}
	}

	h.log.ErrorContext(r.Context(), "unhandled error",
		"method", r.Method, "path", r.URL.Path, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
}
