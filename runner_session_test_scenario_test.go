package lccore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const scenarioTimeout = 5 * time.Second

// scenario holds the assembled graph, all mock components, and the listener.
type scenario struct {
	t          testing.TB
	graph      Graph[testComponent]
	components map[testComponent]*mockComponent
	listener   *mockListener
	options    RunnerOptions[testComponent]
}

// Component returns the mock for the given node. Panics if name not in scenario.
func (s *scenario) Component(name testComponent) *mockComponent {
	mc, ok := s.components[name]
	if !ok {
		panic(fmt.Sprintf("component %q not found in scenario", name))
	}
	return mc
}

// Listener returns the mock listener for direct assertions.
func (s *scenario) Listener() *mockListener {
	return s.listener
}

// Run starts Runner.Run in a background goroutine and returns a runHandle.
func (s *scenario) Run(ctx context.Context, onReady func()) *runHandle {
	h := &runHandle{doneCh: make(chan error, 1)}
	runner := NewRunner[testComponent](s.graph, s.options)
	go func() {
		h.doneCh <- runner.Run(ctx, onReady)
	}()
	return h
}

// RequireStartedBefore asserts that component `a`'s OnStart entered before `b`'s.
func (s *scenario) RequireStartedBefore(t testing.TB, a, b testComponent) {
	t.Helper()
	ctrlA := s.Component(a).startCtrl
	ctrlB := s.Component(b).startCtrl
	require.NotNil(t, ctrlA, "component %q has no OnStart hook", a)
	require.NotNil(t, ctrlB, "component %q has no OnStart hook", b)
	require.True(t, ctrlA.HasEntered(), "component %q OnStart was never entered", a)
	require.True(t, ctrlB.HasEntered(), "component %q OnStart was never entered", b)
	require.Less(t, ctrlA.enteredSeq.Load(), ctrlB.enteredSeq.Load(),
		"expected OnStart(%q) [seq=%d] before OnStart(%q) [seq=%d]",
		a, ctrlA.enteredSeq.Load(), b, ctrlB.enteredSeq.Load())
}

// RequireStartOrder asserts the complete start order (left to right = first to last).
func (s *scenario) RequireStartOrder(t testing.TB, names ...testComponent) {
	t.Helper()
	for i := 1; i < len(names); i++ {
		s.RequireStartedBefore(t, names[i-1], names[i])
	}
}

// RequireClosedBefore asserts that component `a`'s OnClose entered before `b`'s.
func (s *scenario) RequireClosedBefore(t testing.TB, a, b testComponent) {
	t.Helper()
	ctrlA := s.Component(a).closeCtrl
	ctrlB := s.Component(b).closeCtrl
	require.NotNil(t, ctrlA, "component %q has no OnClose hook", a)
	require.NotNil(t, ctrlB, "component %q has no OnClose hook", b)
	require.True(t, ctrlA.HasEntered(), "component %q OnClose was never entered", a)
	require.True(t, ctrlB.HasEntered(), "component %q OnClose was never entered", b)
	require.Less(t, ctrlA.enteredSeq.Load(), ctrlB.enteredSeq.Load(),
		"expected OnClose(%q) [seq=%d] before OnClose(%q) [seq=%d]",
		a, ctrlA.enteredSeq.Load(), b, ctrlB.enteredSeq.Load())
}

// RequireCloseOrder asserts the complete close order.
func (s *scenario) RequireCloseOrder(t testing.TB, names ...testComponent) {
	t.Helper()
	for i := 1; i < len(names); i++ {
		s.RequireClosedBefore(t, names[i-1], names[i])
	}
}

// RequireNotStarted asserts the given components' OnStart was never entered.
func (s *scenario) RequireNotStarted(t testing.TB, names ...testComponent) {
	t.Helper()
	for _, name := range names {
		mc := s.Component(name)
		if mc.startCtrl != nil {
			require.False(t, mc.startCtrl.HasEntered(),
				"expected component %q OnStart to not be entered", name)
		}
	}
}

// RequireNotClosed asserts the given components' OnClose was never entered.
func (s *scenario) RequireNotClosed(t testing.TB, names ...testComponent) {
	t.Helper()
	for _, name := range names {
		mc := s.Component(name)
		if mc.closeCtrl != nil {
			require.False(t, mc.closeCtrl.HasEntered(),
				"expected component %q OnClose to not be entered", name)
		}
	}
}

// RequireStarted asserts the given components' OnStart was entered and exited.
func (s *scenario) RequireStarted(t testing.TB, names ...testComponent) {
	t.Helper()
	for _, name := range names {
		mc := s.Component(name)
		require.NotNil(t, mc.startCtrl, "component %q has no OnStart hook", name)
		require.True(t, mc.startCtrl.HasEntered(),
			"expected component %q OnStart to be entered", name)
		require.True(t, mc.startCtrl.HasExited(),
			"expected component %q OnStart to have exited", name)
	}
}

// RequireClosed asserts the given components' OnClose was entered and exited.
func (s *scenario) RequireClosed(t testing.TB, names ...testComponent) {
	t.Helper()
	for _, name := range names {
		mc := s.Component(name)
		require.NotNil(t, mc.closeCtrl, "component %q has no OnClose hook", name)
		require.True(t, mc.closeCtrl.HasEntered(),
			"expected component %q OnClose to be entered", name)
		require.True(t, mc.closeCtrl.HasExited(),
			"expected component %q OnClose to have exited", name)
	}
}

// AssertAll runs automatic post-run consistency checks.
func (s *scenario) AssertAll(t testing.TB) {
	t.Helper()
	requireListenerConsistency(t, s.components, s.listener)
}

// runHandle provides control over a running Runner.Run.
type runHandle struct {
	doneCh chan error
}

// WaitDone blocks until Runner.Run returns and returns the error.
func (h *runHandle) WaitDone(t testing.TB) error {
	t.Helper()
	select {
	case err := <-h.doneCh:
		return err
	case <-time.After(scenarioTimeout):
		t.Fatal("timeout waiting for Runner.Run to complete")
		return nil
	}
}

// RequireDone asserts WaitDone and checks the returned error with the given matcher.
func (h *runHandle) RequireDone(t testing.TB, matcher errMatcher) {
	t.Helper()
	err := h.WaitDone(t)
	matcher(t, err, "Runner.Run result")
}

// RequireNotDone asserts Runner.Run has not yet returned (non-blocking).
func (h *runHandle) RequireNotDone(t testing.TB) {
	t.Helper()
	select {
	case err := <-h.doneCh:
		t.Fatalf("expected Runner.Run to still be running, but it returned: %v", err)
	default:
	}
}

// requireListenerConsistency cross-checks listener events against hookCtrl states.
func requireListenerConsistency(t testing.TB, components map[testComponent]*mockComponent, listener *mockListener) {
	t.Helper()
	for name, mc := range components {
		// OnStart
		startEntered := mc.startCtrl != nil && mc.startCtrl.HasEntered()
		if startEntered {
			require.Equal(t, 1, listener.countEvents(listenerEventTypeOnStart, name),
				"consistency: expected exactly one OnStart for %q", name)
		} else if mc.startCtrl != nil {
			require.Equal(t, 0, listener.countEvents(listenerEventTypeOnStart, name),
				"consistency: expected no OnStart for %q (hook never entered)", name)
		}

		// OnReady
		startSucceeded := startEntered && mc.startCtrl.HasExited() && mc.startCtrl.SentNilResult()
		if startSucceeded {
			require.Equal(t, 1, listener.countEvents(listenerEventTypeOnReady, name),
				"consistency: expected exactly one OnReady for %q", name)
			// OnReady must come after OnStart
			startIdx := listener.eventIndex(listenerEventTypeOnStart, name)
			readyIdx := listener.eventIndex(listenerEventTypeOnReady, name)
			require.Less(t, startIdx, readyIdx,
				"consistency: OnReady for %q must come after OnStart", name)
		} else if mc.startCtrl != nil {
			require.Equal(t, 0, listener.countEvents(listenerEventTypeOnReady, name),
				"consistency: expected no OnReady for %q (start did not succeed)", name)
		}

		// OnClose
		closeEntered := mc.closeCtrl != nil && mc.closeCtrl.HasEntered()
		if closeEntered {
			require.Equal(t, 1, listener.countEvents(listenerEventTypeOnClose, name),
				"consistency: expected exactly one OnClose for %q", name)
		} else if mc.closeCtrl != nil {
			require.Equal(t, 0, listener.countEvents(listenerEventTypeOnClose, name),
				"consistency: expected no OnClose for %q (hook never entered)", name)
		}

		// OnDone: every component in the graph must have exactly one OnDone
		require.Equal(t, 1, listener.countEvents(listenerEventTypeOnDone, name),
			"consistency: expected exactly one OnDone for %q", name)
	}
}
