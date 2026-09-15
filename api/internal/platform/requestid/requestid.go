// Package requestid carries the per-request correlation ID through a context.
package requestid

import "context"

type contextKey struct{}

// With returns a copy of ctx carrying id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request ID stored in ctx, or "" if there is none.
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
