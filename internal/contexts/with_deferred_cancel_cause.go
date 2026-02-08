package contexts

import (
	"context"
	"sync"
	"time"
)

// WithDeferredCancelCause returns a context, a cancelAfter function, and a cancelAnyway function.
// cancelAfter can be called multiple times, but only the first call will have an effect. It cancels
// the context after the specified duration with the provided cause. If d <= 0, it cancels immediately.
// If the context is canceled by other means (e.g., parent context cancellation), the pending timer
// goroutine is stopped.
// cancelAnyway cancels the context immediately regardless of whether cancelAfter was already called.
// It also stops any pending timer goroutine started by cancelAfter. It should be deferred to ensure
// the context is always cleaned up.
func WithDeferredCancelCause(
	parentCtx context.Context,
) (
	ctx context.Context,
	cancelAfter func(d time.Duration, cause error),
	cancelAnyway func(cause error),
) {
	ctx, cancel := context.WithCancelCause(parentCtx)

	var once sync.Once
	cancelAfter = func(d time.Duration, cause error) {
		once.Do(func() {
			if d <= 0 {
				cancel(cause)
				return
			}
			go func() {
				timer := time.NewTimer(d)
				defer timer.Stop()
				select {
				case <-timer.C:
					cancel(cause)
				case <-ctx.Done():
					// context was canceled by other means, stop the timer and return
				}
			}()
		})
	}

	return ctx, cancelAfter, func(cause error) {
		// even if the context has already been canceled by cancelAfter, we want to allow cancelAnyway
		// to override the cause immediately stopping the timer if it's still running
		// (case <-ctx.Done() will be picked up)
		cancel(cause)
	}
}
