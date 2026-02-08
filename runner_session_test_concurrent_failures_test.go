package lccore

import (
	"context"
	"errors"
	"testing"
)

// 8.1: Two independent roots A and B (start only) both fail concurrently. Runner returns first failure.
func TestRunner_TwoStartFailsConcurrently(t *testing.T) {
	errA := errors.New("A start error")
	errB := errors.New("B start error")

	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Node("B").HasStart().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	b := s.Component("B")

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)

	a.Start().Fail(errA)
	b.Start().Fail(errB)

	runErr := handle.WaitDone(t)
	// Runner returns the FIRST failure (either errA or errB)
	if !errors.Is(runErr, errA) && !errors.Is(runErr, errB) {
		t.Errorf("expected runner error to be errA or errB, got %v", runErr)
	}

	// Exactly one OnShutdown, with cause matching whichever start failure arrived first
	s.Listener().RequireShutdownEmitted(t, matchOneOf(
		matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(errA)),
		matchLHF("B", LifecycleHookNameOnStart, matchErrorIs(errB)),
	))

	// Both get OnDone
	s.Listener().RequireDoneEmitted(t, "A")
	s.Listener().RequireDoneEmitted(t, "B")

	s.AssertAll(t)
}

// 8.2: A and B start concurrently. A fails while B is still starting. B's start context gets canceled.
func TestRunner_StartFailsWhileSiblingStillStarting(t *testing.T) {
	startErr := errors.New("A start error")

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

	// A fails → startsCtx canceled → B's start context canceled
	a.Start().Fail(startErr)
	// Wait for A's start to fully exit before unblocking B, ensuring A's failure is the first event
	a.Start().WaitExited(t)

	// B receives canceled context — unblock B's start with context error
	b.Start().Fail(context.Canceled)

	runErr := handle.WaitDone(t)
	// Runner returns the first failure (A's)
	lhfMatcher := matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(startErr))
	lhfMatcher(t, runErr, "runner result")

	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 8.3: A→B→C. C starts, wait enters. B starts, wait enters. A starts (in progress).
// C's wait fails → shutdown → A's start context canceled. B's close is called; C's close skipped.
func TestRunner_WaitFailsWhileAnotherNodeIsStarting(t *testing.T) {
	waitErr := errors.New("C wait error")

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
	c.Wait().WaitEntered(t)

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	b.Wait().WaitEntered(t)

	a.Start().WaitEntered(t)

	// C's wait fails → shutdown → startsCtx canceled
	c.Wait().Fail(waitErr)
	// Wait for C's wait to fully exit, ensuring its error is the first event in the runner
	c.Wait().WaitExited(t)

	// A's start context is canceled → A's start should fail
	a.Start().Fail(context.Canceled)

	// B's close is called (wait in progress), then B's wait is unblocked
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("C", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// C's close was SKIPPED (wait already exited)
	s.RequireNotClosed(t, "C")
	s.Listener().RequireNoCloseEmitted(t, "C")
	s.Listener().RequireCloseEmitted(t, "B")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 8.4: A→B. B starts+wait. A starts (in progress). Cancel ctx → startsCtx canceled.
func TestRunner_CtxCancelWhileStartInProgress(t *testing.T) {
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
	b.Wait().WaitEntered(t)

	a.Start().WaitEntered(t)

	// Cancel ctx → shutdown triggered → startsCtx canceled
	cancel()

	// A's start context is canceled → A's start fails
	a.Start().Fail(context.Canceled)

	// B is closed
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	// Runner returns context.Canceled (original trigger)
	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 8.5: A and B are independent roots with {S,W,C}. B's start fails → shutdown triggered.
// A's start was in-flight and succeeds AFTER shutdown. handleNodeStartResult sees shutdownCause != nil
// → closes A immediately (exercises the shutdown-already-in-progress branch in handleNodeStartResult).
func TestRunner_StartSucceedsAfterShutdownTriggered(t *testing.T) {
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

	// B fails → shutdown triggered; A's start is still in-flight
	b.Start().Fail(startErr)
	// Wait for B's start to fully exit so that shutdown is recorded before A's result
	b.Start().WaitExited(t)

	// A's start succeeds — now shutdownCause is already set → handleNodeStartResult
	// calls handleNodeIsReady (starts A's wait), then calls tryCloseNode(A) because shutdownCause != nil.
	// With wait in-progress and no remaining dependents, tryCloseNode proceeds to start A's close.
	a.Start().Succeed()

	// A's wait and close both enter (wait was started by handleNodeIsReady, close by tryCloseNode)
	a.Wait().WaitEntered(t)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("B", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	// A was closed (start succeeded, then immediately closed due to shutdown)
	s.Listener().RequireCloseEmitted(t, "A")
	s.AssertAll(t)
}

// 8.6: Ctx is canceled while A's start is in-flight (no wait/close on A, single node).
// ctx.Done fires → tryShutDown returns false (A start in-progress) → inner drain loop entered.
// A's start result arrives in the inner loop → runner completes.
// This explicitly exercises the inner-loop path in loop() (lines 206-210).
func TestRunner_CtxCancelWhileStartInProgress_InnerDrainLoop(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)

	// Cancel ctx while A's start is in-flight.
	// The loop() goroutine will pick up ctx.Done(), call tryShutDown (which returns false because
	// A's start is inProgress), then fall into the inner for-loop draining lcHookResults.
	cancel()

	// A's start fails due to canceled context → arrives in inner drain loop
	a.Start().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}
