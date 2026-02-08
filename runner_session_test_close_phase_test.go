package lccore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cardinalby/go-lccore/internal/tests"
	"github.com/stretchr/testify/require"
)

// 10.1: Start fails → close NEVER entered.
func TestRunner_Close_NotCalledForFailedStart(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	startErr := errors.New("start error")
	a.Start().Fail(startErr)

	handle.RequireDone(t, matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(startErr)))
	s.RequireNotClosed(t, "A")
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.AssertAll(t)
}

// 10.2: B's wait exits nil (no RequireAlive) → B's close is SKIPPED.
func TestRunner_Close_SkippedWhenWaitAlreadyDone(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	// B's wait exits nil (no RequireAlive → no error)
	b.Wait().Succeed()
	// A's wait exits nil → runner returns nil
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())

	// B's close was SKIPPED (wait exited before shutdown)
	s.RequireNotClosed(t, "B")
	s.Listener().RequireNoCloseEmitted(t, "B")
	s.AssertAll(t)
}

// 10.3: A with start+wait+close. Cancel → close enters while wait still in progress.
func TestRunner_Close_RunsWhenWaitStillInProgress(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// Close enters because wait is still in progress
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireClosed(t, "A")
	s.Listener().RequireCloseEmitted(t, "A")
	s.AssertAll(t)
}

// 10.4: A→B, both start+wait+close. Cancel with specific cause. Verify OnClose receives the cause.
func TestRunner_Close_ReceivesShutdownCause(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancelCause(context.Background())
	shutdownCause := errors.New("custom shutdown cause")
	defer cancel(nil)

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel(shutdownCause)

	// A's close enters with shutdownCause
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	s.Listener().RequireCloseEmittedWith(t, "A", matchErrorIs(shutdownCause))
	s.Listener().RequireCloseEmittedWith(t, "B", matchErrorIs(shutdownCause))
	s.AssertAll(t)
}

// 10.5: A starts ok + wait. B starts, fails. Shutdown triggered. A's close receives the start failure cause.
func TestRunner_Close_CauseIsStartError(t *testing.T) {
	startErr := errors.New("B start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	b := s.Component("B")

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	b.Start().Fail(startErr)

	// A's close enters with the B start failure cause
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("B", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	s.Listener().RequireCloseEmittedWith(t, "A", lhfMatcher)
	s.AssertAll(t)
}

// 10.6: A start+wait+close, B start+close. Cancel → A close fails. Runner returns context.Canceled, not closeErr.
func TestRunner_Close_ErrorDoesNotChangeRunnerResult(t *testing.T) {
	closeErr := errors.New("close error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A close enters, fails
	a.Close().WaitEntered(t)
	a.Close().Fail(closeErr)
	a.Wait().Fail(context.Canceled)

	// A done → B close triggers
	b.Close().WaitEntered(t)
	b.Close().Succeed()

	// Runner returns context.Canceled (NOT closeErr)
	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 10.7: A→B→C, all start+wait+close. Cancel → strict close order A, B, C.
func TestRunner_Close_OrderRespectsDepChain_ThreeNodes(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	c := s.Component("C")
	b := s.Component("B")
	a := s.Component("A")

	c.Start().WaitEntered(t)
	c.Start().Succeed()
	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)

	cancel()

	// Close order must be A, B, C
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	c.Close().WaitEntered(t)
	c.Close().Succeed()
	c.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireCloseOrder(t, "A", "B", "C")
	s.AssertAll(t)
}

// 10.8: A and B (independent roots, start+wait+close). Cancel → A and B close concurrently.
func TestRunner_Close_IndependentNodesCloseConcurrently(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	b := s.Component("B")

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)
	a.Start().Succeed()
	b.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel()

	// Verify both close hooks entered before either completes
	rv := tests.NewRendezvous("A", "B")
	go func() {
		a.Close().WaitEntered(t)
		rv.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Close().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()

	a.Close().Succeed()
	b.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 11.1: StartTimeout exceeded. The runner triggers shutdown via startsCtx.Done() and returns
// ErrStartTimedOut (which wraps context.DeadlineExceeded) directly, not wrapped in
// ErrLifecycleHookFailed. The listener OnShutdown also receives ErrStartTimedOut.
func TestRunner_StartTimeout_Exceeded(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			StartTimeout: 50 * time.Millisecond,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)

	// Shutdown is triggered by startsCtx.Done() firing (StartTimeout), before the hook returns.
	// Unblock the start hook afterward to simulate a well-behaved component returning ctx.Err().
	time.Sleep(100 * time.Millisecond)
	a.Start().Fail(context.DeadlineExceeded)

	timedOutErrMatcher := matchErrorEqual(ErrTimedOut{
		LifecycleHookName:    LifecycleHookNameOnStart,
		Timeout:              50 * time.Millisecond,
		IsRunnerStartTimeout: true,
	})
	handle.RequireDone(t, timedOutErrMatcher)
	s.Listener().RequireShutdownEmitted(t, timedOutErrMatcher)
	s.AssertAll(t)
}

// 11.2: StartTimeout not exceeded. Start completes normally.
func TestRunner_StartTimeout_NotExceeded(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			StartTimeout: 5 * time.Second,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 12.1: ShutdownTimeout set. Close hook receives a non-nil context; timer fires and cancels
// it with ErrTimedOut cause while the hook is still blocked.
func TestRunner_ShutdownTimeout_ContextCanceledAfterTimeout(t *testing.T) {
	const shutdownTimeout = 60 * time.Millisecond

	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			ShutdownTimeout: shutdownTimeout,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	// Trigger shutdown
	cancel()

	// Close hook is entered (shutdown triggers close immediately for a node with no wait)
	a.Close().WaitEntered(t)
	aCtx := a.Close().ReceivedCtx()
	require.NotNil(t, aCtx)

	// The context must not be canceled yet (timer hasn't fired)
	require.NoError(t, aCtx.Err(), "close ctx must be live immediately after shutdown")

	// Wait for ShutdownTimeout to elapse
	time.Sleep(shutdownTimeout + 40*time.Millisecond)

	// Now the context must be canceled with ErrTimedOut as cause
	require.Error(t, aCtx.Err(), "close ctx must be canceled after ShutdownTimeout")
	cause := context.Cause(aCtx)
	expectedCause := ErrTimedOut{
		LifecycleHookName:       LifecycleHookNameOnClose,
		Timeout:                 shutdownTimeout,
		IsRunnerShutdownTimeout: true,
	}
	require.Equal(t, expectedCause, cause,
		"context cause must be ErrTimedOut with LifecycleHookNameOnClose")

	a.Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 12.2: ShutdownTimeout is global: the remaining budget is shared across all nodes.
// A→B chain. ShutdownTimeout=200ms. After shutdown, A's close blocks for 120ms.
// When B's close hook is entered, the context must be canceled within the remaining budget
// (less than shutdownTimeout - aCloseDelay + slack), not after a fresh full shutdownTimeout.
func TestRunner_ShutdownTimeout_IsGlobal_DeadlineShared(t *testing.T) {
	const shutdownTimeout = 200 * time.Millisecond
	const aCloseDelay = 120 * time.Millisecond
	const maxRemainingForB = shutdownTimeout - aCloseDelay + 20*time.Millisecond

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			ShutdownTimeout: shutdownTimeout,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel()

	// A's close runs first; block it for aCloseDelay to consume most of the global budget.
	a.Close().WaitEntered(t)
	time.Sleep(aCloseDelay)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// B's close runs next. The context must be canceled within the remaining global budget,
	// not after a fresh full shutdownTimeout starting now.
	b.Close().WaitEntered(t)
	bCtx := b.Close().ReceivedCtx()
	require.NotNil(t, bCtx)

	bEnteredAt := time.Now()
	select {
	case <-bCtx.Done():
		elapsed := time.Since(bEnteredAt)
		require.Less(t, elapsed, maxRemainingForB,
			"B's close context was canceled after %v from hook entry, but global budget "+
				"should have had less than %v remaining (shutdownTimeout=%v aCloseDelay=%v)",
			elapsed, maxRemainingForB, shutdownTimeout, aCloseDelay)
	case <-time.After(shutdownTimeout + 50*time.Millisecond):
		t.Fatalf("B's close context was not canceled within shutdownTimeout (%v) + slack, "+
			"suggesting the global budget is not shared", shutdownTimeout)
	}

	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 12.3: ShutdownTimeout expired: nodes whose close starts after the timeout is exhausted
// receive an already-canceled context.
// A→B chain. ShutdownTimeout=80ms. A's close blocks for 150ms (past the budget).
// When B's close hook enters, its context must already be canceled.
func TestRunner_ShutdownTimeout_IsGlobal_ExpiredForLaterNodes(t *testing.T) {
	const shutdownTimeout = 80 * time.Millisecond
	const aCloseDelay = 150 * time.Millisecond

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			ShutdownTimeout: shutdownTimeout,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel()

	// A's close blocks past the entire ShutdownTimeout.
	a.Close().WaitEntered(t)
	time.Sleep(aCloseDelay)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// B's close runs next. Its context must already be canceled.
	b.Close().WaitEntered(t)
	bCtx := b.Close().ReceivedCtx()
	require.NotNil(t, bCtx)

	require.Error(t, bCtx.Err(),
		"B's close context must already be canceled: ShutdownTimeout (%v) was exhausted "+
			"during A's close (%v)", shutdownTimeout, aCloseDelay)
	require.Equal(t, ErrTimedOut{
		LifecycleHookName:       LifecycleHookNameOnClose,
		Timeout:                 shutdownTimeout,
		IsRunnerShutdownTimeout: true,
	}, context.Cause(bCtx))

	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 12.4: A node whose OnClose is already in flight when shutdown fires must also have its
// context canceled by ShutdownTimeout. Natural close starts before shutdown (A's wait exits
// cleanly and all its dependents are done), then external cancel triggers shutdown, then
// ShutdownTimeout fires and cancels A's still-running close context.
func TestRunner_ShutdownTimeout_AppliesToInFlightNaturalClose(t *testing.T) {
	const shutdownTimeout = 80 * time.Millisecond

	// A is a root (no dependents), no wait. Its close will start naturally once it starts.
	// B is independent. We cancel the ctx after A's close is in flight to trigger shutdown.
	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A", "B").
		WithOptions(RunnerOptions[testComponent]{
			ShutdownTimeout: shutdownTimeout,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	b := s.Component("B")

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)
	a.Start().Succeed()
	b.Start().Succeed()
	b.Wait().WaitEntered(t)

	// A's close enters naturally (no wait, no dependents).
	a.Close().WaitEntered(t)
	aCtx := a.Close().ReceivedCtx()
	require.NotNil(t, aCtx)
	require.NoError(t, aCtx.Err(), "A's close ctx must be live before shutdown")

	// Now trigger shutdown — ShutdownTimeout timer starts.
	cancel()
	b.Wait().Fail(context.Canceled)

	// Wait for ShutdownTimeout to elapse.
	time.Sleep(shutdownTimeout + 40*time.Millisecond)

	// A's close context must now be canceled by the timer.
	require.Error(t, aCtx.Err(),
		"A's in-flight close ctx must be canceled by ShutdownTimeout after shutdown")
	require.Equal(t, ErrTimedOut{
		LifecycleHookName:       LifecycleHookNameOnClose,
		Timeout:                 shutdownTimeout,
		IsRunnerShutdownTimeout: true,
	}, context.Cause(aCtx))

	a.Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 12.4b: ShutdownTimeout set but all closes finish before it fires. After Run returns,
// the close context must be canceled (cleaned up) even though the timer never fired.
func TestRunner_ShutdownTimeout_CloseCtxCanceledAfterRun(t *testing.T) {
	const shutdownTimeout = 5 * time.Second

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			ShutdownTimeout: shutdownTimeout,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel()

	// A closes first (root), then B
	a.Close().WaitEntered(t)
	aCtx := a.Close().ReceivedCtx()
	require.NotNil(t, aCtx)
	require.NoError(t, aCtx.Err(), "close ctx must be live while close hooks are running")
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	bCtx := b.Close().ReceivedCtx()
	require.NotNil(t, bCtx)
	require.NoError(t, bCtx.Err(), "close ctx must be live while close hooks are running")
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	// After Run returns, the close context must be canceled (cleaned up by the deferred cancelAnyway)
	// even though ShutdownTimeout (5s) has not elapsed.
	require.Error(t, aCtx.Err(), "close ctx must be canceled after Run returns")
	require.Error(t, bCtx.Err(), "close ctx must be canceled after Run returns")
	s.AssertAll(t)
}

// 12.5: No ShutdownTimeout (zero). Close context is never canceled by the runner.
func TestRunner_ShutdownTimeout_ZeroMeansNoTimeout(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t) // no ShutdownTimeout option

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	a.Close().WaitEntered(t)
	aCtx := a.Close().ReceivedCtx()
	require.NotNil(t, aCtx)

	// Sleep well past any reasonable timeout to confirm context stays live.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, aCtx.Err(), "close ctx must never be canceled when ShutdownTimeout is zero")

	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 11.3: StartTimeout triggers shutdown via the loop (not via a hook error). In a chain A→B,
// B's start is blocked when the timeout fires. The loop detects startsCtx.Done() and calls
// tryShutDown before B returns. A, which hasn't started yet, is skipped entirely.
// Run returns ErrStartTimedOut (wrapping context.DeadlineExceeded).
func TestRunner_StartTimeout_TriggersShutdownBeforeHookReturns(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		WithOptions(RunnerOptions[testComponent]{
			StartTimeout: 50 * time.Millisecond,
		}).
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	b.Start().WaitEntered(t)

	// A depends on B, so A has not started yet.
	s.RequireNotStarted(t, "A")

	// Let the StartTimeout fire. The loop detects startsCtx.Done() and triggers shutdown
	// before B's start hook returns.
	time.Sleep(100 * time.Millisecond)

	// Unblock B's start (simulating a well-behaved component returning ctx.Err()).
	b.Start().Fail(context.DeadlineExceeded)

	timedOutErrMatcher := matchErrorEqual(ErrTimedOut{
		LifecycleHookName:    LifecycleHookNameOnStart,
		Timeout:              50 * time.Millisecond,
		IsRunnerStartTimeout: true,
	})
	handle.RequireDone(t, timedOutErrMatcher)
	// A was never started because shutdown was triggered before B became ready.
	s.RequireNotStarted(t, "A")
	s.Listener().RequireShutdownEmitted(t, timedOutErrMatcher)
	s.AssertAll(t)
}

// 13.1: Call Run twice concurrently on the same runner. Second call returns ErrAlreadyRunning.
func TestRunner_ErrAlreadyRunning(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the runner manually so we can call Run on the same instance twice.
	theRunner := NewRunner[testComponent](s.graph, s.options)
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- theRunner.Run(ctx, nil)
	}()
	handle := &runHandle{doneCh: doneCh}

	a := s.Component("A")
	a.Start().WaitEntered(t)

	// Call Run again on the SAME runner — should return ErrAlreadyRunning immediately.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	runErr := theRunner.Run(ctx2, nil)
	if !errors.Is(runErr, ErrAlreadyRunning) {
		t.Errorf("expected ErrAlreadyRunning, got %v", runErr)
	}

	// Clean up first run
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	cancel()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}
