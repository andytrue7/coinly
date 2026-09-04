// Package httpx holds the small HTTP plumbing every Coinly service's REST
// adapter shares: JSON encoding/decoding with a consistent error body,
// and bearer-token authentication backed by pkg/jwtx.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrBadRequest wraps every DecodeJSON failure so handlers can map the
// whole class to 400 without inspecting the cause.
var ErrBadRequest = errors.New("httpx: bad request")

// ErrorBody is the JSON shape of every error response:
//
//	{"error": {"code": "email_taken", "message": "email already registered"}}
//
// code is a stable machine-readable identifier; message is for humans.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the payload inside ErrorBody.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON encodes v as the response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encoding failures after the header is written can't be reported to
	// the client; the only recourse is the connection error they'll see.
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an ErrorBody with the given status.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// DecodeJSON reads exactly one JSON object from r into dst, rejecting
// unknown fields, trailing data, and bodies over maxBytes. Every failure
// wraps ErrBadRequest.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %w", ErrBadRequest, describeDecodeError(err))
	}
	// A second Decode must hit EOF; anything else is trailing garbage.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain a single JSON object", ErrBadRequest)
	}
	return nil
}

func describeDecodeError(err error) error {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		maxErr    *http.MaxBytesError
	)
	switch {
	case errors.Is(err, io.EOF):
		return errors.New("empty body")
	case errors.As(err, &syntaxErr):
		return fmt.Errorf("malformed JSON at offset %d", syntaxErr.Offset)
	case errors.As(err, &typeErr):
		return fmt.Errorf("invalid value for field %q", typeErr.Field)
	case errors.As(err, &maxErr):
		return fmt.Errorf("body exceeds %d bytes", maxErr.Limit)
	default:
		return err
	}
}
