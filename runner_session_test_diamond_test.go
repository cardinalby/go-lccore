package lccore

import (
	"context"
	"errors"
	"testing"

	"github.com/cardinalby/go-lccore/internal/tests"
)

// Diamond topology:
//
//	    A
//	   / \
//	  B   C
//	   \ /
//	    D
//
// A depends on B and C; B and C both depend on D.

// 3.1: Diamond, all start+wait+close. Verify start and close ordering with concurrency.
func TestRunner_Diamond_StartAndCloseOrder(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
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

	// D starts first (leaf)
	d.Start().WaitEntered(t)
	d.Start().Succeed()

	// B and C start concurrently after D
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
	c.Start().Succeed()

	// A starts after B and C
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	cancel()

	// A closes first
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// B and C close concurrently — wait for both to enter before unblocking either
	b.Close().WaitEntered(t)
	c.Close().WaitEntered(t)
	b.Close().Succeed()
	c.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	c.Wait().Fail(context.Canceled)

	// D closes last
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	// D started before B and C; B and C started before A
	s.RequireStartedBefore(t, "D", "B")
	s.RequireStartedBefore(t, "D", "C")
	s.RequireStartedBefore(t, "B", "A")
	s.RequireStartedBefore(t, "C", "A")

	// A closed before B and C; B and C closed before D
	s.RequireClosedBefore(t, "A", "B")
	s.RequireClosedBefore(t, "A", "C")
	s.RequireClosedBefore(t, "B", "D")
	s.RequireClosedBefore(t, "C", "D")

	s.AssertAll(t)
}

// 3.2: Diamond. D fails to start. B, C, A never start.
func TestRunner_Diamond_LeafStartFails(t *testing.T) {
	startErr := errors.New("D start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("D").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	d.Start().WaitEntered(t)
	d.Start().Fail(startErr)

	lhfMatcher := matchLHF("D", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	s.RequireNotStarted(t, "A", "B", "C")
	s.RequireNotClosed(t, "A", "B", "C")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// 3.3: Diamond. D starts ok, B starts ok, C fails. A never starts; B is closed; D is closed.
func TestRunner_Diamond_MiddleNodeStartFails(t *testing.T) {
	startErr := errors.New("C start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	b := s.Component("B")
	c := s.Component("C")

	d.Start().WaitEntered(t)
	d.Start().Succeed()

	// B and C start concurrently — wait for both, then fail C
	b.Start().WaitEntered(t)
	c.Start().WaitEntered(t)

	b.Start().Succeed()
	b.Wait().WaitEntered(t)
	c.Start().Fail(startErr)

	// A never starts
	s.RequireNotStarted(t, "A")

	// B is closed (started successfully, wait in progress)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	// D is closed after B
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("C", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.Listener().RequireCloseEmitted(t, "B")
	s.Listener().RequireCloseEmitted(t, "D")
	s.RequireNotClosed(t, "A", "C")
	s.AssertAll(t)
}

// 3.4: Diamond, all started. D's wait fails. Close order: A, B and C, then D skipped.
func TestRunner_Diamond_LeafWaitFails(t *testing.T) {
	waitErr := errors.New("D wait error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
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

	d.Start().WaitEntered(t)
	d.Start().Succeed()

	b.Start().WaitEntered(t)
	c.Start().WaitEntered(t)
	b.Start().Succeed()
	c.Start().Succeed()

	a.Start().WaitEntered(t)
	a.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	// D's wait fails → shutdown
	d.Wait().Fail(waitErr)

	// Close order: A first, then B and C, D skipped (wait already exited)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	c.Close().WaitEntered(t)
	b.Close().Succeed()
	c.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	c.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("D", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// D's close was SKIPPED (wait already exited)
	s.RequireNotClosed(t, "D")
	s.Listener().RequireNoCloseEmitted(t, "D")
	s.Listener().RequireCloseEmitted(t, "A", "B", "C")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}
