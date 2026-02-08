package lccore

import (
	"context"
	"testing"
)

// Test 1: Single component with all hooks, clean shutdown via ctx cancel.
func TestRunner_SingleComponent_CtxCancel(t *testing.T) {
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
	handle.RequireNotDone(t)

	cancel()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// Test 2: Two nodes A→B (A depends on B). B must start before A.
func TestRunner_StartOrderRespectsDependency(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	// B is the leaf — it must start first
	b := s.Component("B")
	b.Start().WaitEntered(t)

	// A depends on B, so A must NOT be able to start while B's start is blocking.
	// TryWaitEntered gives it time — if A could start, it would.
	a := s.Component("A")
	a.Start().AssertWaitNotEntered(t)

	b.Start().Succeed()

	// Now A can start
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	// Both waiting
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	cancel()

	// Close order: A first (root), then B
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// Test 3: Two independent roots must start concurrently.
func TestRunner_IndependentNodesStartConcurrently(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	s.Component("A").Start().WaitEntered(t)
	s.Component("B").Start().WaitEntered(t)

	s.Component("A").Start().Succeed()
	s.Component("B").Start().Succeed()

	s.Component("A").Wait().WaitEntered(t)
	s.Component("B").Wait().WaitEntered(t)

	cancel()

	// A and B close concurrently
	s.Component("A").Close().WaitEntered(t)
	s.Component("B").Close().WaitEntered(t)
	s.Component("A").Close().Succeed()
	s.Component("B").Close().Succeed()
	s.Component("A").Wait().Fail(context.Canceled)
	s.Component("B").Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}
