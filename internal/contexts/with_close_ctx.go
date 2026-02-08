package contexts

import (
	"context"
	"sync"
)

type closeCtxKey struct{}

type closeCtxValue struct {
	mu  sync.RWMutex
	ctx context.Context
}

// BgWithCloseCtxCancelCause returns a `res` context derived from context.Background. The returned `cancel`
// function can be used to cancel the context with a specific cause and close context.
// When `cancel` is called, it sets up a close context that can be retrieved using `GetCloseCtx(res)`
func BgWithCloseCtxCancelCause() (
	res context.Context,
	cancel func(cause error, closeContext context.Context),
) {
	valPtr := new(closeCtxValue)
	res = context.WithValue(context.Background(), closeCtxKey{}, valPtr)
	res, cancelCause := context.WithCancelCause(res)

	cancel = func(cause error, closeContext context.Context) {
		valPtr.mu.Lock()
		if valPtr.ctx == nil {
			valPtr.ctx = closeContext
		}
		valPtr.mu.Unlock()
		// cancel the main context after setting up the close context
		cancelCause(cause)
	}
	return res, cancel
}

// GetCloseCtx retrieves the close context from the provided context after it was canceled.
// If it's not a context created by BgWithCloseCtxCancelCause or the context is not canceled yet, it returns
// context.Background().
// The close context should be used to perform cleanup operations after the main context has been canceled.
func GetCloseCtx(ctx context.Context) context.Context {
	closeCtxValPtr, ok := ctx.Value(closeCtxKey{}).(*closeCtxValue)
	if !ok {
		return context.Background()
	}
	closeCtxValPtr.mu.RLock()
	defer closeCtxValPtr.mu.RUnlock()
	if closeCtxValPtr.ctx == nil {
		return context.Background()
	}
	return closeCtxValPtr.ctx
}
