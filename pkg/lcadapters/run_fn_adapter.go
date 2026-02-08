package lcadapters

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/cardinalby/go-lccore/internal/contexts"
)

var ErrAlreadyRunning = errors.New("runFn is already running")

type runFnNodeBehaviorAdapter struct {
	runFn         func(ctx context.Context, onReady func()) (err error)
	runCtx        context.Context
	cancelRunCtx  func(cause error, closeContext context.Context)
	runDoneSignal chan struct{}
	runResult     error
	isRunning     atomic.Bool
}

// NewRunFnAdapter creates a new NodeBehavior adapter from a Run function.
// runFn function is an alternative blocking syntax for lifecycle behavior definition.
// runFn implements Start, Wait and Close phases with a single function.
//   - Once called, runFn starts the node's initialization (respecting passed ctx) and should call onReady callback
//     when the node is ready (dependents can start).
//   - If initialization fails, runFn should return an error.
//   - After onReady is called, runFn should block until it's job is done or ctx is canceled.
//   - if the node finishes its job it returns nil or an error in case of failure (it triggers shutdown).
//   - if ctx is done, runFn performs cleanup using contexts.GetCloseCtx(ctx) and returns any error that occurred
//     during cleanup or ctx.Err().
func NewRunFnAdapter(
	runFn func(ctx context.Context, onReady func()) (err error),
) Adapter {
	return &runFnNodeBehaviorAdapter{
		runFn: runFn,
	}
}

func (a *runFnNodeBehaviorAdapter) OnStart() func(ctx context.Context) error {
	return a.start
}

func (a *runFnNodeBehaviorAdapter) OnWait() func() error {
	return a.wait
}

func (a *runFnNodeBehaviorAdapter) OnClose() func(ctx context.Context) error {
	return a.close
}

// start implements NodeBehavior's start hook by calling runFn in a goroutine. Start blocks until either:
// - runFn calls onReady callback, in which case Start returns nil
// - runFn returns an error before calling onReady, in which case Start returns that error
// - ctx is done before onReady is called, in which case Start cancels runFn and returns ctx's cause
// Start returns ErrAlreadyRunning if called while already running.
func (a *runFnNodeBehaviorAdapter) start(ctx context.Context) error {
	if a.isRunning.Swap(true) {
		return ErrAlreadyRunning
	}
	a.runCtx, a.cancelRunCtx = contexts.BgWithCloseCtxCancelCause()
	a.runDoneSignal = make(chan struct{})
	readySignal := make(chan struct{})

	var readyOnce sync.Once
	go func() {
		a.runResult = a.runFn(a.runCtx, func() {
			readyOnce.Do(func() { close(readySignal) })
		})
		close(a.runDoneSignal)
		a.isRunning.Store(false)
	}()
	select {
	case <-a.runDoneSignal:
		return a.runResult
	case <-readySignal:
		return nil
	case <-ctx.Done():
		// improvement: pass close timed out close context as a second arg?
		// now we use the same done ctx and the cleanup will fail immediately
		a.cancelRunCtx(context.Cause(ctx), ctx)
		<-a.runDoneSignal
		if errors.Is(a.runResult, a.runCtx.Err()) {
			return context.Cause(ctx)
		}
		return a.runResult
	}
}

// wait implements NodeBehavior's wait hook: it blocks until runFn returns and
// returns the result of runFn
func (a *runFnNodeBehaviorAdapter) wait() error {
	<-a.runDoneSignal
	return a.runResult
}

// close implements NodeBehavior's close hook: it cancels runFn and waits for it to finish.
// It returns nil if runFn returned ctx.Err() as a result of cancellation,
// otherwise it returns the result of runFn.
func (a *runFnNodeBehaviorAdapter) close(ctx context.Context) error {
	a.cancelRunCtx(context.Canceled, ctx)
	<-a.runDoneSignal
	if errors.Is(a.runResult, a.runCtx.Err()) {
		return nil
	}
	return a.runResult
}
