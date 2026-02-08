package lccore

import (
	"context"
	"errors"
	"testing"
)

// 1.1: Single node, start only. Runner should return nil immediately.
func TestRunner_SingleNode_StartOnly(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireStartEmitted(t, "A")
	s.Listener().RequireReadyEmitted(t, "A")
	s.Listener().RequireDoneEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.2: Single node, start+wait (no close) — edge case. Cancel ctx; wait returns a custom error.
// Components without OnClose cannot be notified by the runner to stop; they must observe the context themselves.
// The runner result is the first shutdown cause (context.Canceled from the ctx cancel);
// the node's OnDone receives the raw wait error, distinct from context.Canceled.
func TestRunner_SingleNode_StartWait_NoClose(t *testing.T) {
	waitErr := errors.New("custom wait exit")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
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
	// Without OnClose the component must detect cancellation via context and exit itself.
	// We return a custom error (not context.Canceled) to verify it is visible via OnDone.
	a.Wait().Fail(waitErr)

	// Runner result is the first shutdown cause: context.Canceled (from ctx cancel).
	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	// OnDone receives the raw wait error (distinct from context.Canceled)
	s.Listener().RequireDoneError(t, "A", matchErrorIs(waitErr))
	s.AssertAll(t)
}

// 1.3: Single node, start + close (no wait). Close triggers immediately since no dependents.
func TestRunner_SingleNode_StartClose(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	// Close triggers immediately (no wait, no dependents)
	a.Close().WaitEntered(t)
	a.Close().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireStartEmitted(t, "A")
	s.Listener().RequireReadyEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.5: Single node, start fails. Runner returns errLHF wrapping the start error.
func TestRunner_SingleNode_StartFails(t *testing.T) {
	startErr := errors.New("start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Fail(startErr)

	lhfMatcher := matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	// errors.Is should work through Unwrap chain - verified above via lhfMatcher
	s.Listener().RequireDoneError(t, "A", lhfMatcher)
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	// Close never entered (start failed)
	s.RequireNotClosed(t, "A")
	s.AssertAll(t)
}

// 1.5 (error structure): Verify errors.Is/As semantics on start failure error.
func TestRunner_SingleNode_StartFails_ErrorStructure(t *testing.T) {
	startErr := errors.New("start error")

	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Fail(startErr)

	runErr := handle.WaitDone(t)

	// errors.As -> ErrLifecycleHookFailed
	var lhf ErrLifecycleHookFailed[testComponent]
	if !errors.As(runErr, &lhf) {
		t.Fatalf("expected ErrLifecycleHookFailed, got %T: %v", runErr, runErr)
	}
	if lhf.Component() != "A" {
		t.Errorf("expected Component()=A, got %v", lhf.Component())
	}
	if lhf.LifecycleHookName() != LifecycleHookNameOnStart {
		t.Errorf("expected LifecycleHookName()=OnStart, got %v", lhf.LifecycleHookName())
	}
	// errors.Is should find the original error
	if !errors.Is(runErr, startErr) {
		t.Errorf("expected errors.Is(runErr, startErr) == true")
	}
}

// 1.5 (error string): Verify ErrLifecycleHookFailed.Error() format: "<hook> of component <name> failed: <cause>".
func TestRunner_SingleNode_StartFails_ErrorString(t *testing.T) {
	startErr := errors.New("start error")

	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Fail(startErr)

	runErr := handle.WaitDone(t)

	errStr := runErr.Error()
	if errStr != "OnStart of component A failed: start error" {
		t.Errorf("unexpected Error() string: %q", errStr)
	}
}

// 1.6: Single node, wait fails (no close) — edge case. Runner returns errLHF; OnDone gets unwrapped error.
// Tests that a node with no OnClose whose wait exits with an unexpected error is handled correctly.
func TestRunner_SingleNode_WaitFails_NoClose(t *testing.T) {
	waitErr := errors.New("wait error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	a.Wait().Fail(waitErr)

	lhfMatcher := matchLHF("A", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// OnDone gets the UNWRAPPED waitErr (wrapping happens after OnDone)
	s.Listener().RequireDoneError(t, "A", matchErrorIs(waitErr))
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 1.7: Single node, wait exits nil, no RequireAlive. Runner returns nil, no shutdown.
func TestRunner_SingleNode_WaitExitsNil_NoConstraint(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireStartEmitted(t, "A")
	s.Listener().RequireReadyEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.8: Single node, wait only (no start, no close) — edge case. Cancel ctx triggers shutdown.
// Without OnClose the component must self-terminate by observing context cancellation.
func TestRunner_SingleNode_WaitOnly(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	// Wait enters immediately (no start required)
	a.Wait().WaitEntered(t)
	handle.RequireNotDone(t)

	cancel()
	// A has no close, so we must unblock wait manually
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	// No OnStart, OnReady, OnClose for A (no start/close hooks)
	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchErrorIs(context.Canceled))
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 1.10: Single node, wait+close (no start). Cancel ctx → close enters while wait is in progress.
// This is a component with no async startup but a long-running phase and a dedicated stop signal.
func TestRunner_SingleNode_WaitClose_CtxCancel(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	// Wait enters immediately (no start required)
	a.Wait().WaitEntered(t)
	handle.RequireNotDone(t)

	cancel()

	// Close enters (wait still in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "A")
	// OnDone error is either nil (close wins the race) or context.Canceled (wait wins) — not asserted.
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 1.10b: Single node, wait+close (no start). Wait exits nil naturally → close is skipped.
func TestRunner_SingleNode_WaitClose_NaturalCompletion(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Wait().WaitEntered(t)
	// Natural exit: wait returns nil → close skipped
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.9: Single node, close only (no start, no wait). Close triggers immediately.
func TestRunner_SingleNode_CloseOnly(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	// Close triggers immediately: no start, no wait, no dependents
	a.Close().WaitEntered(t)
	a.Close().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "A")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.11: Close fails. Runner returns nil; close error is only visible via OnDone.
func TestRunner_SingleNode_CloseFails(t *testing.T) {
	closeErr := errors.New("close error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	// Close triggers immediately (no wait, no dependents)
	a.Close().WaitEntered(t)
	a.Close().Fail(closeErr)

	// Runner returns nil even though close failed!
	handle.RequireDone(t, matchNoError())
	// OnDone carries the close error
	s.Listener().RequireDoneError(t, "A", matchErrorIs(closeErr))
	// No OnShutdown
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 1.12: Empty graph. Runner calls onReady and returns nil immediately.
func TestRunner_EmptyGraph(t *testing.T) {
	s := newScenarioBuilder().
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onReadyCalled := false
	handle := s.Run(ctx, func() { onReadyCalled = true })

	handle.RequireDone(t, matchNoError())
	if !onReadyCalled {
		t.Error("expected onReady to be called for empty graph")
	}
}

// 1.13: Already-canceled ctx. Runner returns ctx.Err() immediately.
func TestRunner_AlreadyCanceledCtx(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	handle := s.Run(ctx, nil)
	handle.RequireDone(t, matchErrorIs(context.Canceled))
}
