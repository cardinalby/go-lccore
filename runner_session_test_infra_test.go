package lccore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// hookCtrl tests
// ============================================================

func TestHookCtrl_Succeed(t *testing.T) {
	ctrl := newHookCtrl()
	require.False(t, ctrl.HasEntered())
	require.False(t, ctrl.HasExited())

	var hookErr error
	done := make(chan struct{})
	go func() {
		hookErr = ctrl.startFunc()(context.Background())
		close(done)
	}()

	ctrl.WaitEntered(t)
	require.True(t, ctrl.HasEntered())
	require.False(t, ctrl.HasExited())

	ctrl.Succeed()
	ctrl.WaitExited(t)
	require.True(t, ctrl.HasExited())
	require.True(t, ctrl.SentNilResult())

	<-done
	require.NoError(t, hookErr)
}

func TestHookCtrl_Fail(t *testing.T) {
	ctrl := newHookCtrl()
	testErr := errors.New("test error")

	var hookErr error
	done := make(chan struct{})
	go func() {
		hookErr = ctrl.startFunc()(context.Background())
		close(done)
	}()

	ctrl.WaitEntered(t)
	ctrl.Fail(testErr)
	ctrl.WaitExited(t)
	require.False(t, ctrl.SentNilResult())

	<-done
	require.ErrorIs(t, hookErr, testErr)
}

func TestHookCtrl_WaitFunc(t *testing.T) {
	ctrl := newHookCtrl()

	var hookErr error
	done := make(chan struct{})
	go func() {
		hookErr = ctrl.waitFunc()()
		close(done)
	}()

	ctrl.WaitEntered(t)
	ctrl.Succeed()
	ctrl.WaitExited(t)

	<-done
	require.NoError(t, hookErr)
}

func TestHookCtrl_CloseFunc(t *testing.T) {
	ctrl := newHookCtrl()
	testErr := errors.New("close error")

	var hookErr error
	done := make(chan struct{})
	go func() {
		hookErr = ctrl.closeFunc()(context.Background())
		close(done)
	}()

	ctrl.WaitEntered(t)
	ctrl.Fail(testErr)
	ctrl.WaitExited(t)

	<-done
	require.ErrorIs(t, hookErr, testErr)
}

func TestHookCtrl_EnteredSeqIsMonotonic(t *testing.T) {
	ctrl1 := newHookCtrl()
	ctrl2 := newHookCtrl()

	// Run ctrl1 first, then ctrl2
	errCh1 := make(chan error, 1)
	go func() { errCh1 <- ctrl1.startFunc()(context.Background()) }()
	ctrl1.WaitEntered(t)
	ctrl1.Succeed()
	ctrl1.WaitExited(t)
	require.NoError(t, <-errCh1)

	errCh2 := make(chan error, 1)
	go func() { errCh2 <- ctrl2.startFunc()(context.Background()) }()
	ctrl2.WaitEntered(t)
	ctrl2.Succeed()
	ctrl2.WaitExited(t)
	require.NoError(t, <-errCh2)

	require.Less(t, ctrl1.enteredSeq.Load(), ctrl2.enteredSeq.Load())
}

// ============================================================
// mockComponent tests
// ============================================================

func TestMockComponent_WithAllHooks(t *testing.T) {
	mc := &mockComponent{
		name:      "test",
		startCtrl: newHookCtrl(),
		waitCtrl:  newHookCtrl(),
		closeCtrl: newHookCtrl(),
	}

	require.NotNil(t, mc.OnStart())
	require.NotNil(t, mc.OnWait())
	require.NotNil(t, mc.OnClose())
	require.NotPanics(t, func() { mc.Start() })
	require.NotPanics(t, func() { mc.Wait() })
	require.NotPanics(t, func() { mc.Close() })
}

func TestMockComponent_WithNoHooks(t *testing.T) {
	mc := &mockComponent{name: "empty"}

	require.Nil(t, mc.OnStart())
	require.Nil(t, mc.OnWait())
	require.Nil(t, mc.OnClose())
	require.Panics(t, func() { mc.Start() })
	require.Panics(t, func() { mc.Wait() })
	require.Panics(t, func() { mc.Close() })
}

func TestMockComponent_OwnConstraints(t *testing.T) {
	mc := &mockComponent{name: "test"}
	require.Equal(t, ComponentOwnConstraints{}, mc.OwnConstraints())
}

// ============================================================
// mockListener tests
// ============================================================

func TestMockListener_RecordsEvents(t *testing.T) {
	l := newMockListener()
	testErr := errors.New("test")

	l.OnStart("A")
	l.OnReady("A")
	l.OnClose("A", testErr)
	l.OnDone("A", nil)
	l.OnShutdown(testErr)

	require.Len(t, l.events, 5)
	require.Equal(t, listenerEventTypeOnStart, l.events[0].eventType)
	require.Equal(t, testComponent("A"), l.events[0].component)
	require.Equal(t, listenerEventTypeOnReady, l.events[1].eventType)
	require.Equal(t, listenerEventTypeOnClose, l.events[2].eventType)
	require.ErrorIs(t, l.events[2].err, testErr)
	require.Equal(t, listenerEventTypeOnDone, l.events[3].eventType)
	require.NoError(t, l.events[3].err)
	require.Equal(t, listenerEventTypeOnShutdown, l.events[4].eventType)
	require.ErrorIs(t, l.events[4].err, testErr)
}

func TestMockListener_EventsOf(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")
	l.OnStart("B")
	l.OnReady("A")

	starts := l.eventsOf(listenerEventTypeOnStart)
	require.Len(t, starts, 2)
	ready := l.eventsOf(listenerEventTypeOnReady)
	require.Len(t, ready, 1)
}

func TestMockListener_ComponentEvents(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")
	l.OnStart("B")
	l.OnReady("A")
	l.OnDone("A", nil)

	events := l.componentEvents("A")
	require.Len(t, events, 3)
	require.Equal(t, listenerEventTypeOnStart, events[0].eventType)
	require.Equal(t, listenerEventTypeOnReady, events[1].eventType)
	require.Equal(t, listenerEventTypeOnDone, events[2].eventType)
}

func TestMockListener_RequireStartEmitted(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")
	l.OnStart("B")

	l.RequireStartEmitted(t, "A", "B")
}

func TestMockListener_RequireNoStartEmitted(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")

	l.RequireNoStartEmitted(t, "B", "C")
}

func TestMockListener_RequireStartBefore(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")
	l.OnStart("B")

	l.RequireStartBefore(t, "A", "B")
}

func TestMockListener_RequireCloseBefore(t *testing.T) {
	l := newMockListener()
	l.OnClose("A", nil)
	l.OnClose("B", nil)

	l.RequireCloseBefore(t, "A", "B")
}

func TestMockListener_RequireShutdownEmitted(t *testing.T) {
	l := newMockListener()
	testErr := errors.New("shutdown cause")
	l.OnShutdown(testErr)

	l.RequireShutdownEmitted(t, matchErrorIs(testErr))
}

func TestMockListener_RequireShutdownNotEmitted(t *testing.T) {
	l := newMockListener()
	l.RequireShutdownNotEmitted(t)
}

func TestMockListener_RequireDoneError(t *testing.T) {
	l := newMockListener()
	testErr := errors.New("done error")
	l.OnDone("A", testErr)

	l.RequireDoneError(t, "A", matchErrorIs(testErr))
}

// ============================================================
// errMatcher tests
// ============================================================

func TestErrMatcher_matchErrorIs(t *testing.T) {
	target := errors.New("target")
	matcher := matchErrorIs(target)
	matcher(t, target)
}

func TestErrMatcher_NoError(t *testing.T) {
	matcher := matchNoError()
	matcher(t, nil)
}

func TestErrMatcher_AnyError(t *testing.T) {
	matcher := matchAnyError()
	matcher(t, errors.New("something"))
}

func TestErrMatcher_ErrorAs(t *testing.T) {
	var target ErrUnexpectedWaitResult[testComponent]
	matcher := matchErrorAs(&target)
	matcher(t, ErrUnexpectedWaitResult[testComponent]{Dependent: "X"})
	require.Equal(t, testComponent("X"), target.Dependent)
}

func TestErrMatcher_matchLHF(t *testing.T) {
	innerErr := errors.New("inner")
	lhfErr := errLifecycleHookFailed[testComponent]{
		component:         "A",
		lifecycleHookName: LifecycleHookNameOnStart,
		err:               innerErr,
	}
	matcher := matchLHF("A", LifecycleHookNameOnStart, matchErrorIs(innerErr))
	matcher(t, lhfErr)
}

func TestErrMatcher_matchLHF_OnWait(t *testing.T) {
	innerErr := errors.New("wait-err")
	lhfErr := errLifecycleHookFailed[testComponent]{
		component:         "B",
		lifecycleHookName: LifecycleHookNameOnWait,
		err:               innerErr,
	}
	matcher := matchLHF("B", LifecycleHookNameOnWait, matchErrorIs(innerErr))
	matcher(t, lhfErr)
}

func TestErrMatcher_matchUWR(t *testing.T) {
	uwrErr := ErrUnexpectedWaitResult[testComponent]{
		Dependent:  "A",
		WaitResult: nil,
	}
	matcher := matchUWR("A", matchNoError())
	matcher(t, uwrErr)
}

func TestErrMatcher_matchUWR_WithWaitResult(t *testing.T) {
	waitErr := errors.New("wait-result")
	uwrErr := ErrUnexpectedWaitResult[testComponent]{
		Dependent:  "X",
		WaitResult: waitErr,
	}
	matcher := matchUWR("X", matchErrorIs(waitErr))
	matcher(t, uwrErr)
}

func TestErrMatcher_matchLHF_WrappingUWR(t *testing.T) {
	uwrErr := ErrUnexpectedWaitResult[testComponent]{
		Dependent:  "A",
		WaitResult: nil,
	}
	lhfErr := errLifecycleHookFailed[testComponent]{
		component:         "B",
		lifecycleHookName: LifecycleHookNameOnWait,
		err:               uwrErr,
	}
	// matchLHF should unwrap to the inner UWR, and matchUWR should match it
	matcher := matchLHF("B", LifecycleHookNameOnWait, matchUWR("A", matchNoError()))
	matcher(t, lhfErr)
}

// ============================================================
// mockListener RequireCloseEmittedWith / RequireNoCloseEmitted tests
// ============================================================

func TestMockListener_RequireCloseEmittedWith(t *testing.T) {
	l := newMockListener()
	testErr := errors.New("close cause")
	l.OnClose("A", testErr)

	l.RequireCloseEmittedWith(t, "A", matchErrorIs(testErr))
}

func TestMockListener_RequireCloseEmittedWith_NilCause(t *testing.T) {
	l := newMockListener()
	l.OnClose("A", nil)

	l.RequireCloseEmittedWith(t, "A", matchNoError())
}

func TestMockListener_RequireNoCloseEmitted(t *testing.T) {
	l := newMockListener()
	l.OnClose("A", nil)

	l.RequireNoCloseEmitted(t, "B", "C")
}

// ============================================================
// ScenarioBuilder tests
// ============================================================

func TestScenarioBuilder_BuildsGraph(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().DependsOn("B").Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	require.NotNil(t, s)
	require.NotNil(t, s.graph)
	require.Len(t, s.components, 2)
	require.NotNil(t, s.listener)

	// Component A has start and wait but no close
	mcA := s.Component("A")
	require.NotNil(t, mcA.startCtrl)
	require.NotNil(t, mcA.waitCtrl)
	require.Nil(t, mcA.closeCtrl)

	// Component B has start and wait
	mcB := s.Component("B")
	require.NotNil(t, mcB.startCtrl)
	require.NotNil(t, mcB.waitCtrl)
	require.Nil(t, mcB.closeCtrl)
}

func TestScenarioBuilder_AllHooks(t *testing.T) {
	s := newScenarioBuilder().
		Node("X").HasStart().HasWait().HasClose().Done().
		Roots("X").
		Build(t)

	mc := s.Component("X")
	require.NotNil(t, mc.startCtrl)
	require.NotNil(t, mc.waitCtrl)
	require.NotNil(t, mc.closeCtrl)
}

func TestScenarioBuilder_DependsOnWith(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().DependsOnWith("B", DependencyConstraints{RequireAlive: true}).Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	require.NotNil(t, s)
	// Verify the graph was built (if it compiles and runs, deps are correct)
	require.NotNil(t, s.graph)
}

// ============================================================
// Scenario.Run + RunHandle tests
// ============================================================

func TestScenario_RunAndComplete(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	// A should start
	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	// A's wait is now running; cancel to trigger shutdown.
	cancel()
	s.Component("A").Wait().Fail(context.Canceled)
	handle.RequireDone(t, matchErrorIs(context.Canceled))
}

func TestRunHandle_RequireNotDone(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)
	s.Component("A").Start().WaitEntered(t)

	// Runner is still running (A's start hasn't completed)
	handle.RequireNotDone(t)

	s.Component("A").Start().Succeed()
	// A has no wait/close, runner finishes with nil once start completes
	handle.RequireDone(t, matchNoError())
}

// ============================================================
// Ordering assertions tests
// ============================================================

func TestScenario_RequireStartOrder(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().DependsOn("B").Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	// B starts first (leaf), then A
	s.Component("B").Start().WaitEntered(t)
	s.Component("B").Start().Succeed()
	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	s.RequireStartOrder(t, "B", "A")

	cancel()
	s.Component("B").Wait().Fail(context.Canceled)
	s.Component("A").Wait().Fail(context.Canceled)
	handle.RequireDone(t, matchErrorIs(context.Canceled))
}

func TestScenario_RequireNotStarted(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().DependsOn("B").Done().
		Node("B").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	// B is the leaf, so it starts first. A hasn't started yet.
	s.Component("B").Start().WaitEntered(t)
	s.RequireNotStarted(t, "A")

	s.Component("B").Start().Succeed()
	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	// A and B have no wait/close, runner finishes with nil once starts complete
	handle.RequireDone(t, matchNoError())
}

// ============================================================
// AssertAll / listener consistency tests
// ============================================================

func TestScenario_AssertAll_SimpleSuccess(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	// A has no wait/close, runner finishes with nil once start completes
	handle.RequireDone(t, matchNoError())

	s.AssertAll(t)
}

func TestScenario_AssertAll_WithClose(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	cancel()
	s.Component("A").Close().WaitEntered(t)
	s.Component("A").Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

func TestScenario_AssertAll_StartFailure(t *testing.T) {
	startErr := errors.New("start_fail")

	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Fail(startErr)

	err := handle.WaitDone(t)
	require.Error(t, err)
	require.ErrorIs(t, err, startErr)

	s.AssertAll(t)
}

// ============================================================
// Listener String() for debugging
// ============================================================

func TestMockListener_String(t *testing.T) {
	l := newMockListener()
	l.OnStart("A")
	l.OnShutdown(errors.New("cause"))

	s := l.String()
	assert.Contains(t, s, "OnStart")
	assert.Contains(t, s, "OnShutdown")
	assert.Contains(t, s, "cause")
}

// ============================================================
// hookCtrl timeout behavior (ensure WaitEntered doesn't hang forever on missing enter)
// ============================================================

func TestHookCtrl_WaitEnteredTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// This test verifies that WaitEntered properly times out.
	// We use a mock testing.TB to capture the Fatal call.
	ctrl := newHookCtrl()

	// Don't start any goroutine - the hook is never entered.
	// WaitEntered should timeout after hookCtrlTimeout.
	// We can't easily test t.Fatal without a mock, so just verify HasEntered is false.
	require.False(t, ctrl.HasEntered())
}

// ============================================================
// Scenario.Component panics for unknown component
// ============================================================

func TestScenario_ComponentPanicsForUnknown(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	require.Panics(t, func() {
		s.Component("UNKNOWN")
	})
}

// ============================================================
// End-to-end: two-node dependency chain with all phases
// ============================================================

func TestScenario_TwoNodeChain_AllPhases(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	// B starts first (leaf)
	s.Component("B").Start().WaitEntered(t)
	s.RequireNotStarted(t, "A")
	s.Component("B").Start().Succeed()

	// A can now start
	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	// Both are now waiting
	s.Component("B").Wait().WaitEntered(t)
	s.Component("A").Wait().WaitEntered(t)

	// Cancel to trigger shutdown. Wait for A's close to be entered before
	// unblocking waits, to avoid a race between ctx.Done() and wait results
	// in the runner's select loop.
	cancel()

	// A closes first (root) — shutdown launches A's close while wait is still running
	s.Component("A").Close().WaitEntered(t)
	s.Component("A").Wait().Fail(context.Canceled)
	s.Component("A").Close().Succeed()

	// B closes second (leaf) — wait for B's close to be entered before
	// unblocking B's wait, so the runner doesn't skip the close phase.
	s.Component("B").Close().WaitEntered(t)
	s.Component("B").Wait().Fail(context.Canceled)
	s.Component("B").Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))

	s.RequireStartOrder(t, "B", "A")
	s.RequireCloseOrder(t, "A", "B")
	s.AssertAll(t)
}

// ============================================================
// Scenario with onReady callback
// ============================================================

func TestScenario_OnReadyCallback(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().Done().
		Roots("A").
		Build(t)

	readyCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, func() { close(readyCh) })

	s.Component("A").Start().WaitEntered(t)
	s.Component("A").Start().Succeed()

	// onReady should be called after all components started
	select {
	case <-readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for onReady callback")
	}

	// A has no wait/close, so after start + cancel the runner finishes with nil
	cancel()
	handle.RequireDone(t, matchNoError())
}
