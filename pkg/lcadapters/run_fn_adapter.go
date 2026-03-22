package lcadapters

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/cardinalby/go-lccore/internal/contexts"
)

// NewRunFnAdapter creates a new Adapter from `runFn` function.
// runFn function is an alternative simplified blocking syntax for lifecycle behavior definition.
// runFn implements Start, Wait and Close phases with a single function.
// - Once called, runFn should block until it's job is done or ctx is canceled
// - The node is considered immediately ready (dependents can start) once runFn is called (unlike
// NewReadinessRunFnAdapter) and unblocks starts of dependents.
// - if the node finishes its job it returns nil or an error in case of failure (it triggers shutdown).
// - if ctx is done, runFn performs cleanup using contexts.GetCloseCtx(ctx) and returns any error that occurred
// during cleanup or ctx.Err().
func NewRunFnAdapter(
	runFn func(ctx context.Context) (err error),
) Adapter {
	return &runFnAdapter{
		runFn: runFn,
	}
}

type runFnAdapter struct {
	runFn         func(ctx context.Context) (err error)
	runCtx        context.Context
	cancelRunCtx  func(cause error, closeContext context.Context)
	runDoneSignal chan struct{}
	runResult     error
	isRunning     atomic.Bool
}

func (a *runFnAdapter) OnStart() func(ctx context.Context) error {
	return a.start
}

func (a *runFnAdapter) OnWait() func() error {
	return a.wait
}

func (a *runFnAdapter) OnClose() func(ctx context.Context) error {
	return a.close
}

func (a *runFnAdapter) start(_ context.Context) error {
	if a.isRunning.Swap(true) {
		return ErrAlreadyRunning
	}
	a.runCtx, a.cancelRunCtx = contexts.BgWithCloseCtxCancelCause()
	a.runDoneSignal = make(chan struct{})

	go func() {
		a.runResult = a.runFn(a.runCtx)
		close(a.runDoneSignal)
		a.isRunning.Store(false)
	}()
	return nil
}

func (a *runFnAdapter) wait() error {
	<-a.runDoneSignal
	return a.runResult
}

func (a *runFnAdapter) close(ctx context.Context) error {
	a.cancelRunCtx(context.Canceled, ctx)
	<-a.runDoneSignal
	if errors.Is(a.runResult, a.runCtx.Err()) {
		return nil
	}
	return a.runResult
}
