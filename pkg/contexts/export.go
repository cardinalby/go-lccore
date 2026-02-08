package contexts

import (
	"context"

	"github.com/cardinalby/go-lccore/internal/contexts"
)

// GetCloseCtx retrieves the close context from the ctx passed to Runner.Run after it was canceled.
// If the context is not canceled yet, it returns context.Background().
// The close context should be used to perform cleanup operations after the main context has been canceled
func GetCloseCtx(ctx context.Context) context.Context {
	return contexts.GetCloseCtx(ctx)
}
