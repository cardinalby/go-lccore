package lccore

import (
	"context"
	"errors"
	"testing"

	"github.com/cardinalby/go-lccore/internal/tests"
)

// Fan-out topology:
//
//	  A
//	 /|\
//	B C D
//
// A depends on B, C, D.

// 4.1: Fan-out. B, C, D are leaves — all start concurrently.
func TestRunner_FanOut_ConcurrentLeafStart(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").DependsOn("D").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Node("C").HasStart().HasWait().HasClose().Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	c := s.Component("C")
	d := s.Component("D")
	a := s.Component("A")

	// All three leaves start concurrently — verify all entered before any is unblocked
	rv := tests.NewRendezvous("B", "C", "D")
	go func() {
		b.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()
	go func() {
		c.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "C")
	}()
	go func() {
		d.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "D")
	}()

	b.Start().Succeed()
	c.Start().Succeed()
	d.Start().Succeed()

	// After all leaves succeed, A starts
	a.Start().WaitEntered(t)
	a.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	cancel()

	// A closes first, then B, C, D concurrently
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// Wait for all three to enter close before unblocking any
	b.Close().WaitEntered(t)
	c.Close().WaitEntered(t)
	d.Close().WaitEntered(t)
	b.Close().Succeed()
	c.Close().Succeed()
	d.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	c.Wait().Fail(context.Canceled)
	d.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireStartedBefore(t, "B", "A")
	s.RequireStartedBefore(t, "C", "A")
	s.RequireStartedBefore(t, "D", "A")
	s.RequireClosedBefore(t, "A", "B")
	s.RequireClosedBefore(t, "A", "C")
	s.RequireClosedBefore(t, "A", "D")
	s.AssertAll(t)
}

// 4.2: Fan-out. B succeeds, C fails to start, D succeeds. A never starts; B and D closed.
func TestRunner_FanOut_OneLeafFails_OthersClose(t *testing.T) {
	startErr := errors.New("C start error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").DependsOn("C").DependsOn("D").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Node("C").HasStart().HasWait().Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	c := s.Component("C")
	d := s.Component("D")

	b.Start().WaitEntered(t)
	c.Start().WaitEntered(t)
	d.Start().WaitEntered(t)

	b.Start().Succeed()
	b.Wait().WaitEntered(t)
	d.Start().Succeed()
	d.Wait().WaitEntered(t)
	c.Start().Fail(startErr)

	// A never starts
	s.RequireNotStarted(t, "A")

	// B and D are closed
	b.Close().WaitEntered(t)
	d.Close().WaitEntered(t)
	b.Close().Succeed()
	d.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	d.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("C", LifecycleHookNameOnStart, matchErrorIs(startErr))
	handle.RequireDone(t, lhfMatcher)

	// C is NOT closed (failed to start)
	s.RequireNotClosed(t, "A", "C")
	s.Listener().RequireCloseEmitted(t, "B", "D")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}

// Fan-in topology:
//
// A   B   C
//  \  |  /
//     D
//
// A, B, C all depend on D.

// 5.1: Fan-in. D starts first, then A, B, C concurrently. Cancel → A, B, C close concurrently, D last.
func TestRunner_FanIn_ConcurrentRootClose(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A", "B", "C").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	a := s.Component("A")
	b := s.Component("B")
	c := s.Component("C")

	// D starts first
	d.Start().WaitEntered(t)
	d.Start().Succeed()

	// A, B, C start concurrently after D
	rv := tests.NewRendezvous("A", "B", "C")
	go func() {
		a.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "A")
	}()
	go func() {
		b.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "B")
	}()
	go func() {
		c.Start().WaitEntered(t)
		rv.ArriveAndWait(t, "C")
	}()

	a.Start().Succeed()
	b.Start().Succeed()
	c.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	cancel()

	// A, B, C close concurrently — wait for all to enter before unblocking any
	a.Close().WaitEntered(t)
	b.Close().WaitEntered(t)
	c.Close().WaitEntered(t)
	a.Close().Succeed()
	b.Close().Succeed()
	c.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Wait().Fail(context.Canceled)
	c.Wait().Fail(context.Canceled)

	// D closes last
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.RequireStartedBefore(t, "D", "A")
	s.RequireStartedBefore(t, "D", "B")
	s.RequireStartedBefore(t, "D", "C")
	s.RequireClosedBefore(t, "A", "D")
	s.RequireClosedBefore(t, "B", "D")
	s.RequireClosedBefore(t, "C", "D")
	s.AssertAll(t)
}

// 5.2: Fan-in. A's wait fails. A's close skipped; B, C close; D closes after B, C.
func TestRunner_FanIn_OneRootWaitFails(t *testing.T) {
	waitErr := errors.New("A wait error")

	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("B").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("C").HasStart().HasWait().HasClose().DependsOn("D").Done().
		Node("D").HasStart().HasWait().HasClose().Done().
		Roots("A", "B", "C").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	d := s.Component("D")
	a := s.Component("A")
	b := s.Component("B")
	c := s.Component("C")

	d.Start().WaitEntered(t)
	d.Start().Succeed()

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)
	c.Start().WaitEntered(t)
	a.Start().Succeed()
	b.Start().Succeed()
	c.Start().Succeed()

	a.Wait().WaitEntered(t)
	b.Wait().WaitEntered(t)
	c.Wait().WaitEntered(t)
	d.Wait().WaitEntered(t)

	// A's wait fails
	a.Wait().Fail(waitErr)

	// B, C close (wait still running)
	b.Close().WaitEntered(t)
	c.Close().WaitEntered(t)
	b.Close().Succeed()
	c.Close().Succeed()
	b.Wait().Fail(context.Canceled)
	c.Wait().Fail(context.Canceled)

	// D closes after B and C
	d.Close().WaitEntered(t)
	d.Close().Succeed()
	d.Wait().Fail(context.Canceled)

	lhfMatcher := matchLHF("A", LifecycleHookNameOnWait, matchErrorIs(waitErr))
	handle.RequireDone(t, lhfMatcher)

	// A's close was SKIPPED (wait already exited)
	s.RequireNotClosed(t, "A")
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "B", "C", "D")
	s.Listener().RequireShutdownEmitted(t, lhfMatcher)
	s.AssertAll(t)
}
