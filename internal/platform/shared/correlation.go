package shared

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type correlationIDContextKey struct{}

func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationIDContextKey{}, correlationID)
}

func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(correlationIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

func NewCorrelationID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "corr-fallback"
	}
	return "corr-" + hex.EncodeToString(raw[:])
}
