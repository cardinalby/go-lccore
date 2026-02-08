package lccore

import (
	"context"
	"errors"
	"testing"
)

// 2.2: Chain A→B (A depends on B). Both have start+wait+close. Cancel ctx closes A first, then B.
func TestRunner_Chain_AB_CloseOrder(t *testing.T) {
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

	cancel()

	// A closes first (root), then B
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireCloseBefore(t, "A", "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 2.3: 3-node chain A→B→C. Start order: C, B, A. Close order: A, B, C.
func TestRunner_Chain_ABC_StartAndCloseOrder(t *testing.T) {
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

	// Close order: A, B, C
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
	s.RequireStartOrder(t, "C", "B", "A")
	s.RequireCloseOrder(t, "A", "B", "C")
	s.AssertAll(t)
}

// 2.4: Chain A→B. B starts, fails. A never starts. Runner returns errLHF{B, OnStart, startErr}.
func TestRunner_Chain_AB_LeafStartFails(t *testing.T) {
	startErr := errors.New("leaf start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	b.Start().WaitEntered(t)
	b.Start().Fail(startErr)

	lhfMatcher := matchLHF("B", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	// A was never started, never closed
	s.RequireNotStarted(t, "A")
	s.RequireNotClosed(t, "A")

	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireStartEmitted(t, "B")
	s.Listener().RequireDoneError(t, "B", lhfMatcher)
	// OnDone(A, lhfErr) — A skipped with shutdown cause
	s.Listener().RequireDoneError(t, "A", lhfMatcher)
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 2.5: Chain A→B. B starts ok. A starts, fails. B gets closed.
func TestRunner_Chain_AB_RootStartFails(t *testing.T) {
	startErr := errors.New("root start error")

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
	a.Start().Fail(startErr)

	// B should be closed (wait still in progress)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	// A was NOT closed (start failed)
	s.RequireNotClosed(t, "A")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 2.6: Chain A→B. B's wait fails. A gets closed; B's close is SKIPPED (wait already exited).
func TestRunner_Chain_AB_LeafWaitFails(t *testing.T) {
	waitErr := errors.New("leaf wait error")

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

	// B's wait fails → shutdown triggered
	b.Wait().Fail(waitErr)

	// A gets closed (root, wait still in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("B", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// B's close was SKIPPED (wait already exited)
	s.RequireNotClosed(t, "B")
	s.Listener().RequireNoCloseEmitted(t, "B")
	s.Listener().RequireCloseEmitted(t, "A")
	// OnDone(B, waitErr) — unwrapped
	s.Listener().RequireDoneError(t, "B", matchErrorIs(waitErr))
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 2.7: Chain A→B. A's wait fails. A not closed; B gets closed.
func TestRunner_Chain_AB_RootWaitFails(t *testing.T) {
	waitErr := errors.New("root wait error")

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

	// A's wait fails → shutdown triggered
	a.Wait().Fail(waitErr)

	// B gets closed (wait still running + has close hook)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("A", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// A NOT closed (wait already exited)
	s.RequireNotClosed(t, "A")
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "B")
	s.Listener().RequireDoneError(t, "A", matchErrorIs(waitErr))
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 2.8: Chain A→B. Both start+wait+close. Natural completion — both wait exit nil; close is skipped.
func TestRunner_Chain_AB_NaturalCompletion(t *testing.T) {
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

	// Natural completion: both waits exit nil
	b.Wait().Succeed()
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireDoneError(t, "B", matchNoError())
	s.AssertAll(t)
}
