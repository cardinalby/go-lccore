package lccore

import (
	"context"
	"testing"
)

// 9.1: A→B, both start+wait+close. onReady called only after both starts complete.
func TestRunner_OnReady_CalledAfterAllStarts(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onReadyCalled := false
	handle := s.Run(ctx, func() { onReadyCalled = true })

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()

	// onReady should NOT be called yet (A hasn't started)
	a.Start().WaitEntered(t)
	if onReadyCalled {
		t.Error("onReady called before A started")
	}

	a.Start().Succeed()

	// Wait until onReady is called — both waits are in progress
	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)

	if !onReadyCalled {
		t.Error("expected onReady to be called after all starts")
	}

	cancel()

	// Close order: A (root) first, then B
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 9.2: A→B, both start+wait+close. B fails → A never starts, onReady never called.
func TestRunner_OnReady_NotCalledOnStartFailure(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onReadyCalled := false
	handle := s.Run(ctx, func() { onReadyCalled = true })

	b := s.Component("B")
	b.Start().WaitEntered(t)
	b.Start().Fail(context.Canceled)

	handle.RequireDone(t, matchLHF("B", LifecycleHookNameOnStart, matchErrorIs(context.Canceled)))

	if onReadyCalled {
		t.Error("expected onReady NOT to be called when start fails")
	}
	s.AssertAll(t)
}

// 9.3: Single A (start only). Pass nil as onReady. Runner completes without panic.
func TestRunner_OnReady_NilCallback(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil) // nil onReady

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	handle.RequireDone(t, matchNoError())
	s.AssertAll(t)
}

// 9.4: Single A (close only). onReady should be called (no start components → immediately ready).
func TestRunner_OnReady_WithNoStartComponents(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onReadyCalled := false
	handle := s.Run(ctx, func() { onReadyCalled = true })

	a := s.Component("A")
	a.Close().WaitEntered(t)
	a.Close().Succeed()

	handle.RequireDone(t, matchNoError())
	if !onReadyCalled {
		t.Error("expected onReady to be called for no-start graph")
	}
	s.AssertAll(t)
}
