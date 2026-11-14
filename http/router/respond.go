package router

import (
	"encoding/json"
	"errors"
	"mime"
	nethttp "net/http"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/logger"
)

// MaxBody caps JSON request bodies.
const MaxBody = 64 << 10

// ErrUnauthenticated is the 401 for routes that need a principal.
var ErrUnauthenticated = apperr.New(nethttp.StatusUnauthorized, "unauthenticated", "sign in with a boson account or send a Network key")

// ErrForbidden is the 403 for a missing permission.
func ErrForbidden(perm string) *apperr.Error {
	return apperr.New(nethttp.StatusForbidden, "forbidden", "this needs the "+perm+" permission")
}

// ErrorBody is every error response: {"error": code, "message": text, "code": code}.
type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// WriteJSON writes v with status (no body for nil / 204).
func WriteJSON(w nethttp.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil && status != nethttp.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError renders apperr errors as-is and anything else as a logged 500.
func WriteError(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	if e, ok := apperr.As(err); ok {
		WriteJSON(w, e.Status, ErrorBody{Error: e.Code, Message: e.Message, Code: e.Code})
		return
	}
	logger.FromCtx(r.Context()).Error().Err(err).Str("path", r.URL.Path).Msg("internal error")
	WriteJSON(w, nethttp.StatusInternalServerError, ErrorBody{Error: "internal_error", Message: "internal error", Code: "internal_error"})
}

var errUnsupportedMedia = apperr.New(nethttp.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")

// Decode reads a JSON body (Content-Type application/json required, which
// also keeps plain cross-site HTML forms out).
func Decode(w nethttp.ResponseWriter, r *nethttp.Request, dst any) error {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		return errUnsupportedMedia
	}
	r.Body = nethttp.MaxBytesReader(w, r.Body, MaxBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var mbe *nethttp.MaxBytesError
		if errors.As(err, &mbe) {
			return apperr.New(nethttp.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		}
		return apperr.Invalid("malformed JSON body")
	}
	return nil
}

// DecodeOptional is Decode for bodies that may be empty.
func DecodeOptional(w nethttp.ResponseWriter, r *nethttp.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return Decode(w, r, dst)
}
