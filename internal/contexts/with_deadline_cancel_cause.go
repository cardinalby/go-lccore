package contexts

import (
	"context"
	"time"
)

func WithDeadlineCancelCause(
	parentCtx context.Context,
	deadline time.Time,
	deadlineCause error,
) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(parentCtx)
	// no need to explicitly cancel timeoutCtx if it's canceled by parent via cancel(), it would be no-op
	// (internal timer is already stopped, and it has been removed from the parent context)
	//goland:noinspection GoVetLostCancel
	if deadlineCause != nil {
		ctx, _ = context.WithDeadlineCause(ctx, deadline, deadlineCause)
	} else {
		ctx, _ = context.WithDeadline(ctx, deadline)
	}
	return ctx, cancel
}
