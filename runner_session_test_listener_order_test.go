package lccore

import (
	"context"
	"errors"
	"testing"
)

// 14.1: A (start+wait+close). Verify per-component event order: OnStart→OnReady→OnClose→OnDone.
func TestRunner_ListenerEventOrder_PerComponent(t *testing.T) {
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

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	listener := s.Listener()
	// All events for A must appear in order: OnStart → OnReady → OnClose → OnDone
	events := listener.componentEvents("A")
	if len(events) < 4 {
		t.Fatalf("expected 4 events for A, got %d: %v", len(events), events)
	}
	order := []listenerEventType{
		listenerEventTypeOnStart,
		listenerEventTypeOnReady,
		listenerEventTypeOnClose,
		listenerEventTypeOnDone,
	}
	for i, expectedType := range order {
		if events[i].eventType != expectedType {
			t.Errorf("event[%d]: expected %s, got %s", i, expectedType, events[i].eventType)
		}
	}
	s.AssertAll(t)
}

// 14.2: A (start+close). Start fails. Event order: OnStart → OnDone (no OnReady, no OnClose).
func TestRunner_ListenerEventOrder_StartFailure(t *testing.T) {
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

	listener := s.Listener()
	events := listener.componentEvents("A")
	if len(events) != 2 {
		t.Fatalf("expected 2 events for A, got %d", len(events))
	}
	if events[0].eventType != listenerEventTypeOnStart {
		t.Errorf("event[0]: expected OnStart, got %s", events[0].eventType)
	}
	if events[1].eventType != listenerEventTypeOnDone {
		t.Errorf("event[1]: expected OnDone, got %s", events[1].eventType)
	}
	s.AssertAll(t)
}

// 14.3: A→B, both start+wait+close. Verify cross-component event ordering.
func TestRunner_ListenerEventOrder_CrossComponent(t *testing.T) {
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

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	listener := s.Listener()
	// OnStart(B) before OnStart(A)
	listener.RequireStartBefore(t, "B", "A")
	// OnClose(A) before OnClose(B)
	listener.RequireCloseBefore(t, "A", "B")
	// OnShutdown before any OnClose
	shutdownIdx := listener.eventIndex(listenerEventTypeOnShutdown, "")
	closeAIdx := listener.eventIndex(listenerEventTypeOnClose, "A")
	if shutdownIdx >= closeAIdx {
		t.Errorf("expected OnShutdown before OnClose(A): shutdown=%d, closeA=%d", shutdownIdx, closeAIdx)
	}
	// OnReady before OnShutdown
	readyAIdx := listener.eventIndex(listenerEventTypeOnReady, "A")
	if readyAIdx >= shutdownIdx {
		t.Errorf("expected OnReady(A) before OnShutdown: readyA=%d, shutdown=%d", readyAIdx, shutdownIdx)
	}
	s.AssertAll(t)
}

// 14.4: A→B→C, all start+wait+close. Verify exactly one OnDone for each.
func TestRunner_Listener_OnDoneForEveryComponent(t *testing.T) {
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
	s.Listener().RequireDoneEmitted(t, "A", "B", "C")
	s.AssertAll(t)
}

// 14.5: A and B independent (start+wait). A's wait fails then B's wait fails. OnShutdown exactly once.
func TestRunner_Listener_OnShutdownExactlyOnce(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Node("B").HasStart().HasWait().Done().
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

	waitErrA := errors.New("A wait error")
	waitErrB := errors.New("B wait error")

	// A's wait fails
	a.Wait().Fail(waitErrA)
	// B's wait fails too
	b.Wait().Fail(waitErrB)

	handle.RequireDone(t, matchOneOf(
		matchLHF("A", LifecycleHookNameOnWait, matchErrorIs(waitErrA)),
		matchLHF("B", LifecycleHookNameOnWait, matchErrorIs(waitErrB)),
	))

	// Exactly one OnShutdown
	shutdowns := s.Listener().eventsOf(listenerEventTypeOnShutdown)
	if len(shutdowns) != 1 {
		t.Errorf("expected exactly one OnShutdown, got %d", len(shutdowns))
	}
	s.AssertAll(t)
}
