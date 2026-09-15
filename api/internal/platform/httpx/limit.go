package httpx

import (
	"io"
	"net/http"
)

// limitedBody is a request body capped by LimitBody. It keeps the original
// body so OverrideBodyLimit can replace the cap instead of nesting inside it
// (nested http.MaxBytesReaders enforce the smallest limit).
type limitedBody struct {
	io.ReadCloser
	original io.ReadCloser
}

// LimitBody returns middleware that caps how much of a request body handlers
// can read. Reads past the limit fail with *http.MaxBytesError, which
// DecodeJSON reports as 413 payload_too_large.
func LimitBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = &limitedBody{ReadCloser: http.MaxBytesReader(w, r.Body, limit), original: r.Body}
			next.ServeHTTP(w, r)
		})
	}
}

// OverrideBodyLimit returns route-level middleware that replaces the limit
// set by LimitBody. Use it only on routes that need a different limit (such as
// bulk imports), and only after authentication so anonymous clients never get
// the larger allowance. It must run before anything reads the body.
func OverrideBodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := r.Body
			if lb, ok := body.(*limitedBody); ok {
				body = lb.original
			}
			r.Body = &limitedBody{ReadCloser: http.MaxBytesReader(w, body, limit), original: body}
			next.ServeHTTP(w, r)
		})
	}
}
