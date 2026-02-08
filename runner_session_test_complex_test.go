package lccore

import (
	"context"
	"testing"

	"github.com/cardinalby/go-lccore/internal/tests"
)

// 15.1: A and B both depend on C. Cancel → A and B close concurrently, then C.
//
//	A   B
//	 \ /
//	  C
func TestRunner_Complex_TwoRoots_SharedLeaf(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	c := s.Component("C")
	a := s.Component("A")
	b := s.Component("B")

	// C starts first (leaf)
	c.Start().WaitEntered(t)
	c.Start().Succeed()

	// A and B start concurrently
	rv := tests.NewRendezvous("A", "B")
	go func() {
		a.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()

	a.Start().Succeed()
	b.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)

	cancel()

	// A and B close concurrently, then C
	rv2 := tests.NewRendezvous("A", "B")
	go func() {
		a.Close().WaitEntered(t)
		rv2.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Close().WaitEntered(t)
		rv2.ArriveAndWait(t, "B")
	}()
	a.Close().Succeed()
	b.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Wait().Fail(context.Canceled)

	c.Close().WaitEntered(t)
	c.Close().Succeed()
	c.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	// C started before A and B
	s.RequireStartedBefore(t, "C", "A")
	s.RequireStartedBefore(t, "C", "B")
	// C closed after A and B
	s.RequireClosedBefore(t, "A", "C")
	s.RequireClosedBefore(t, "B", "C")
	s.AssertAll(t)
}

// 15.2: A→B→C→D→E (deep chain, all start+wait+close). Verify start and close order.
func TestRunner_Complex_DeepChain_FiveNodes(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().DependsOn("E").Done().
		Node("E").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	e := s.Component("E")
	d := s.Component("D")
	c := s.Component("C")
	b := s.Component("B")
	a := s.Component("A")

	// Start order: E, D, C, B, A
	e.Start().WaitEntered(t)
	e.Start().Succeed()
	d.Start().WaitEntered(t)
	d.Start().Succeed()
	c.Start().WaitEntered(t)
	c.Start().Succeed()
	b.Start().WaitEntered(t)
	b.Start().Succeed()
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)
	e.Wait().WaitEntered(t)

	cancel()

	// Close order: A, B, C, D, E
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	c.Close().WaitEntered(t)
	c.Close().Succeed()
	c.Wait().Fail(context.Canceled)
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)
	e.Close().WaitEntered(t)
	e.Close().Succeed()
	e.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireStartOrder(t, "E", "D", "C", "B", "A")
	s.RequireCloseOrder(t, "A", "B", "C", "D", "E")
	s.AssertAll(t)
}

// 15.3: Diamond with mixed hooks.
//
//	  A (start+wait+close)
//	 / \
//	B   C (start only)
//	 \ /
//	  D (start+wait+close)
func TestRunner_Complex_DiamondWithMixedHooks(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	b := s.Component("B")
	c := s.Component("C")
	a := s.Component("A")

	// D starts (leaf)
	d.Start().WaitEntered(t)
	d.Start().Succeed()

	// B and C start concurrently
	rv := tests.NewRendezvous("B", "C")
	go func() {
		b.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()
	go func() {
		c.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "C")
	}()

	b.Start().Succeed()
	b.Wait().WaitEntered(t)
	c.Start().Succeed()
	// C has no wait → C immediately done (no wait, no close)

	// A starts after B and C both ready
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	cancel()

	// A closes
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// B closes (has close, wait in progress)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	// D closes after A and B done
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	// C is NOT closed (has no close hook)
	s.RequireNotClosed(t, "C")
	s.Listener().RequireNoCloseEmitted(t, "C")
	s.Listener().RequireCloseEmitted(t, "A", "B", "D")
	s.AssertAll(t)
}

// 16.3: A→C, B→C→D topology. Roots: A, B.
//
//	A → C
//	B → C → D
func TestRunner_MultipleRoots_DifferentDepths(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("C").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	c := s.Component("C")
	a := s.Component("A")
	b := s.Component("B")

	// Start order: D, C, then A and B concurrently
	d.Start().WaitEntered(t)
	d.Start().Succeed()
	c.Start().WaitEntered(t)
	c.Start().Succeed()

	rv := tests.NewRendezvous("A", "B")
	go func() {
		a.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()
	a.Start().Succeed()
	b.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	cancel()

	// A and B close concurrently
	rv2 := tests.NewRendezvous("A", "B")
	go func() {
		a.Close().WaitEntered(t)
		rv2.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Close().WaitEntered(t)
		rv2.ArriveAndWait(t, "B")
	}()
	a.Close().Succeed()
	b.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Wait().Fail(context.Canceled)

	c.Close().WaitEntered(t)
	c.Close().Succeed()
	c.Wait().Fail(context.Canceled)

	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireStartedBefore(t, "D", "C")
	s.RequireStartedBefore(t, "C", "A")
	s.RequireStartedBefore(t, "C", "B")
	s.RequireClosedBefore(t, "A", "C")
	s.RequireClosedBefore(t, "B", "C")
	s.RequireClosedBefore(t, "C", "D")
	s.AssertAll(t)
}

// 16.4: A (start+wait, no close). Wait enters and is unblocked with nil immediately.
func TestRunner_Component_WithStartAndWait_WaitExitsNilImmediately(t *testing.T) {
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
	a.Wait().Succeed() // immediate nil

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 16.5: A→B→C, A→B has RequireAlive=true. Cancel → A closes, A done, B closes, B wait exits nil.
// A is already closed → no violation.
func TestRunner_RequireAlive_WaitAlreadyClosed_NoViolation_ComplexGraph(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().
		DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
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

	// A closes (root)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	// A is now done

	// B closes
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	// B's wait exits nil — A is already closed → RequireAlive check: A's close is done → no violation
	b.Wait().Succeed()

	// C closes
	c.Close().WaitEntered(t)
	c.Close().Succeed()
	c.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	// No additional error from RequireAlive violation
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}
