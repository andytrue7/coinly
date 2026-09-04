package httpx

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/andytrue7/coinly/pkg/jwtx"
)

// TokenVerifier is the slice of jwtx.Verifier the middleware needs.
type TokenVerifier interface {
	Verify(token string, now time.Time) (*jwtx.Claims, error)
}

type ctxKey struct{}

// Subject returns the authenticated subject (user ID) placed in ctx by
// RequireAuth, and false if the request was not authenticated.
func Subject(ctx context.Context) (string, bool) {
	sub, ok := ctx.Value(ctxKey{}).(string)
	return sub, ok && sub != ""
}

// RequireAuth returns middleware that rejects requests without a valid
// "Authorization: Bearer <jwt>" header with 401 and a Bearer challenge,
// and otherwise stores the token subject in the request context.
func RequireAuth(verifier TokenVerifier, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				unauthorized(w, "missing or malformed Authorization header")
				return
			}

			claims, err := verifier.Verify(token, now())
			if err != nil {
				// Deliberately vague: the detailed cause is for logs, not
				// for whoever is probing with a bad token.
				unauthorized(w, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKey{}, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the credentials from a Bearer authorization
// header. The scheme is matched case-insensitively per RFC 9110.
func bearerToken(header string) (string, bool) {
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(scheme):])
	return token, token != ""
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="coinly"`)
	WriteError(w, http.StatusUnauthorized, "unauthorized", message)
}
