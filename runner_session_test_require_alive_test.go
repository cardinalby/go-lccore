package lccore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 7.1: A→B with RequireAlive=true. B's wait exits nil while A is still alive → violation.
func TestRunner_RequireAlive_WaitExitsNil_TriggersShutdown(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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

	// B's wait exits nil → RequireAlive violation
	b.Wait().Succeed()

	// A closes (wait in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	uwrMatcher := matchUWR("A", matchNoError())
	lhfMatcher := matchLHF("B", LifecycleHookNameOnWait, uwrMatcher)
	handle.RequireDone(t, lhfMatcher)

	// B's close skipped (wait already exited)
	s.RequireNotClosed(t, "B")
	// OnDone(B, ErrUnexpectedWaitResult{...}) — unwrapped
	s.Listener().RequireDoneError(t, "B", uwrMatcher)
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 7.1: Verify error structure via errors.As
func TestRunner_RequireAlive_WaitExitsNil_ErrorStructure(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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
	b.Wait().Succeed()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	runErr := handle.WaitDone(t)

	var lhf ErrLifecycleHookFailed[testComponent]
	if !errors.As(runErr, &lhf) {
		t.Fatalf("expected ErrLifecycleHookFailed, got %T: %v", runErr, runErr)
	}
	if lhf.Component() != "B" {
		t.Errorf("expected Component()=B, got %v", lhf.Component())
	}
	if lhf.LifecycleHookName() != LifecycleHookNameOnWait {
		t.Errorf("expected LifecycleHookName()=OnWait, got %v", lhf.LifecycleHookName())
	}

	var uwr ErrUnexpectedWaitResult[testComponent]
	if !errors.As(runErr, &uwr) {
		t.Fatalf("expected ErrUnexpectedWaitResult, got %T: %v", runErr, runErr)
	}
	if uwr.Dependent != "A" {
		t.Errorf("expected Dependent=A, got %v", uwr.Dependent)
	}
	if uwr.WaitResult != nil {
		t.Errorf("expected WaitResult=nil, got %v", uwr.WaitResult)
	}
}

// 7.1 (error string): Verify ErrUnexpectedWaitResult.Error() format.
func TestRunner_RequireAlive_WaitExitsNil_ErrorString(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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
	b.Wait().Succeed()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	runErr := handle.WaitDone(t)

	// The outer error is ErrLifecycleHookFailed wrapping ErrUnexpectedWaitResult.
	// Unwrap once to get ErrUnexpectedWaitResult, then check its Error() string.
	var uwr ErrUnexpectedWaitResult[testComponent]
	if !errors.As(runErr, &uwr) {
		t.Fatalf("expected ErrUnexpectedWaitResult, got %T: %v", runErr, runErr)
	}
	errStr := uwr.Error()
	if errStr != "dependent A requires alive, but OnWait exited: <nil>" {
		t.Errorf("unexpected ErrUnexpectedWaitResult.Error() string: %q", errStr)
	}
}

// 7.2: A→B with RequireAlive=true. B's wait fails with waitErr → same as normal wait failure.
func TestRunner_RequireAlive_WaitExitsError_TriggersShutdown(t *testing.T) {
	waitErr := errors.New("wait error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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

	// B's wait fails with error — RequireAlive doesn't change behavior when wait errors
	b.Wait().Fail(waitErr)

	// A closes (wait in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// Normal wait failure — no ErrUnexpectedWaitResult
	lhfMatcher := matchLHF("B", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)
	s.Listener().RequireDoneError(t, "B", matchErrorIs(waitErr))
	s.AssertAll(t)
}

// 7.3: A→B with RequireAlive=true. Cancel ctx → A closes and is done → B's wait exits nil → no violation.
func TestRunner_RequireAlive_DependentAlreadyClosed_NoViolation(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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

	cancel()

	// A closes (root), A wait unblocked → A done
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// Wait for B's close to be triggered (requires A to be fully done first)
	// so that when B's wait exits nil, A is already registered as done → no violation
	b.Close().WaitEntered(t)

	// B's wait exits nil — A is already closed → no violation
	b.Wait().Succeed()
	b.Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 7.4: A→B with RequireAlive=false (default). B's wait exits nil → no violation.
func TestRunner_RequireAlive_False_WaitExitsNil_NoViolation(t *testing.T) {
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

	b.Wait().Succeed()
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 7.5: A→C (RequireAlive=true), B→C (RequireAlive=false). C's wait exits nil → A violation.
func TestRunner_RequireAlive_MultipleDependent_OneAlive(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("C", DependencyConstraints{RequireAlive: true}).Done().
		Node("B").HasStart().HasWait().HasClose().
		DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	c := s.Component("C")
	a := s.Component("A")
	b := s.Component("B")

	c.Start().WaitEntered(t)
	c.Start().Succeed()
	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)
	a.Start().Succeed()
	b.Start().Succeed()
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)

	// C's wait exits nil — A has RequireAlive=true and is not closed → violation
	c.Wait().Succeed()

	// A and B get closed
	a.Close().WaitEntered(t)
	b.Close().WaitEntered(t)
	a.Close().Succeed()
	b.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Wait().Fail(context.Canceled)

	uwrMatcher := matchUWR("A", matchNoError())
	lhfMatcher := matchLHF("C", LifecycleHookNameOnWait, uwrMatcher)
	handle.RequireDone(t, lhfMatcher)
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 7.6: A→B→C, A→B has RequireAlive=true, B→C has RequireAlive=false.
// C's wait exits nil → no violation. B's wait exits nil → violation for A.
func TestRunner_RequireAlive_TransitiveNotPropagated(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
		Node("B").HasStart().HasWait().HasClose().
		DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	// Set up done notifier for C before running so we can sync on it.
	cDoneNotifier := s.Listener().DoneNotifier("C")

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

	// C's wait exits nil — B has RequireAlive=false → no violation
	c.Wait().Succeed()
	// Wait for the runner to fully process C's wait result before triggering B's wait.
	// This ensures C.close is skipped (not triggered), giving us a deterministic test path.
	select {
	case <-cDoneNotifier:
	case <-time.After(hookCtrlTimeout):
		t.Fatal("timeout waiting for OnDone(C) to be emitted")
	}

	// B's wait exits nil — A has RequireAlive=true, A not closed → violation!
	b.Wait().Succeed()

	// A closes (wait in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	uwrMatcher := matchUWR("A", matchNoError())
	lhfMatcher := matchLHF("B", LifecycleHookNameOnWait, uwrMatcher)
	handle.RequireDone(t, lhfMatcher)

	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	// B and C were not closed (their waits already exited before shutdown)
	s.RequireNotClosed(t, "B", "C")
	s.AssertAll(t)
}

// 7.7: A→B→C, A→B has RequireAlive=true. B has start+close (NO wait). No violation because B has no OnWait.
func TestRunner_RequireAlive_DepHasNoWait_ConstraintIgnored(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
		Node("B").HasStart().HasClose().DependsOn("C").Done().
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
	c.Wait().WaitEntered(t)

	// C's wait exits nil — no RequireAlive from B (default) → no error
	c.Wait().Succeed()
	// A's wait exits nil → no error
	a.Wait().Succeed()

	// B's close triggers (A is done)
	b.Close().WaitEntered(t)
	b.Close().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	// No constraint violation — RequireAlive on A→B has no effect because B has no OnWait
	s.AssertAll(t)
}
