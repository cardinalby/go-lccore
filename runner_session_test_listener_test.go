package lccore

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// errMatcher is a function that runs assertions against an error.
type errMatcher func(t testing.TB, err error, msgAndArgs ...any)

func matchErrorEqual(target error) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		require.Equal(t, target, err, msgAndArgs...)
	}
}

// matchErrorIs returns an errMatcher that asserts errors.Is(err, target) for all targets.
func matchErrorIs(targets ...error) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		for _, target := range targets {
			require.ErrorIs(t, err, target, msgAndArgs...)
		}
	}
}

// matchNoError returns an errMatcher that asserts err == nil.
func matchNoError() errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		require.NoError(t, err, msgAndArgs...)
	}
}

// matchAnyError returns an errMatcher that asserts err != nil.
func matchAnyError() errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		require.Error(t, err, msgAndArgs...)
	}
}

// matchErrorAs returns an errMatcher that asserts errors.As and stores the result.
func matchErrorAs[T error](target *T) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		require.ErrorAs(t, err, target, msgAndArgs...)
	}
}

// matchLHF returns an errMatcher that asserts ErrLifecycleHookFailed with the given component,
// hook name, and inner error.
func matchLHF(component testComponent, hookName LifecycleHookName, innerMatcher errMatcher) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		var lhf ErrLifecycleHookFailed[testComponent]
		require.ErrorAs(t, err, &lhf, msgAndArgs...)
		require.Equal(t, component, lhf.Component())
		require.Equal(t, hookName, lhf.LifecycleHookName())
		innerMatcher(t, lhf.Unwrap())
	}
}

// matchOneOf returns an errMatcher that passes if the error satisfies at least one of matchers.
// Use this for non-deterministic cases where either of several errors may occur.
func matchOneOf(matchers ...errMatcher) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		for _, m := range matchers {
			var tb tbCapture
			m(&tb, err)
			if !tb.failed {
				return
			}
		}
		t.Errorf("error %v did not match any of the expected matchers", err)
	}
}

// tbCapture wraps a real testing.TB and captures failures without stopping the test.
type tbCapture struct {
	testing.TB
	failed bool
}

func (tb *tbCapture) Helper()                           {}
func (tb *tbCapture) Errorf(format string, args ...any) { tb.failed = true }
func (tb *tbCapture) Fatalf(format string, args ...any) { tb.failed = true }
func (tb *tbCapture) FailNow()                          { tb.failed = true }
func (tb *tbCapture) Fail()                             { tb.failed = true }
func (tb *tbCapture) Log(args ...any)                   {}
func (tb *tbCapture) Logf(format string, args ...any)   {}
func (tb *tbCapture) Name() string                      { return "" }

// matchUWR returns an errMatcher that asserts ErrUnexpectedWaitResult with the given dependent
// and wait result.
func matchUWR(dependent testComponent, waitResultMatcher errMatcher) errMatcher {
	return func(t testing.TB, err error, msgAndArgs ...any) {
		t.Helper()
		var uwr ErrUnexpectedWaitResult[testComponent]
		require.ErrorAs(t, err, &uwr, msgAndArgs...)
		require.Equal(t, dependent, uwr.Dependent)
		waitResultMatcher(t, uwr.WaitResult)
	}
}

type listenerEventType string

const (
	listenerEventTypeOnStart    listenerEventType = "OnStart"
	listenerEventTypeOnReady    listenerEventType = "OnReady"
	listenerEventTypeOnClose    listenerEventType = "OnClose"
	listenerEventTypeOnDone     listenerEventType = "OnDone"
	listenerEventTypeOnShutdown listenerEventType = "OnShutdown"
)

type listenerEvent struct {
	eventType listenerEventType
	component testComponent // zero value for OnShutdown
	err       error         // OnClose = cause, OnDone = result, OnShutdown = cause
}

type mockListener struct {
	events        []listenerEvent
	doneNotifiers map[testComponent]chan struct{}
}

func newMockListener() *mockListener {
	return &mockListener{
		doneNotifiers: make(map[testComponent]chan struct{}),
	}
}

// DoneNotifier returns a channel that is closed when OnDone is emitted for component.
// Must be called before the runner starts (or at least before OnDone is expected to fire).
func (l *mockListener) DoneNotifier(component testComponent) <-chan struct{} {
	ch, ok := l.doneNotifiers[component]
	if !ok {
		ch = make(chan struct{})
		l.doneNotifiers[component] = ch
	}
	return ch
}

// WaitForOnDone blocks until OnDone is emitted for the given component.
func (l *mockListener) WaitForOnDone(t testing.TB, component testComponent) {
	t.Helper()
	ch := l.DoneNotifier(component)
	select {
	case <-ch:
	case <-time.After(hookCtrlTimeout):
		t.Fatalf("timeout waiting for OnDone for %q", component)
	}
}

func (l *mockListener) OnStart(component testComponent) {
	l.events = append(l.events, listenerEvent{
		eventType: listenerEventTypeOnStart,
		component: component,
	})
}

func (l *mockListener) OnReady(component testComponent) {
	l.events = append(l.events, listenerEvent{
		eventType: listenerEventTypeOnReady,
		component: component,
	})
}

func (l *mockListener) OnClose(component testComponent, cause error) {
	l.events = append(l.events, listenerEvent{
		eventType: listenerEventTypeOnClose,
		component: component,
		err:       cause,
	})
}

func (l *mockListener) OnDone(component testComponent, result error) {
	l.events = append(l.events, listenerEvent{
		eventType: listenerEventTypeOnDone,
		component: component,
		err:       result,
	})
	if ch, ok := l.doneNotifiers[component]; ok {
		select {
		case <-ch: // already closed (shouldn't happen)
		default:
			close(ch)
		}
	}
}

func (l *mockListener) OnShutdown(cause error) {
	l.events = append(l.events, listenerEvent{
		eventType: listenerEventTypeOnShutdown,
		err:       cause,
	})
}

// eventsOf returns all events of the given type.
func (l *mockListener) eventsOf(eventType listenerEventType) []listenerEvent {
	var result []listenerEvent
	for _, e := range l.events {
		if e.eventType == eventType {
			result = append(result, e)
		}
	}
	return result
}

// componentEvents returns all events for the given component in emission order.
func (l *mockListener) componentEvents(component testComponent) []listenerEvent {
	var result []listenerEvent
	for _, e := range l.events {
		if e.component == component {
			result = append(result, e)
		}
	}
	return result
}

// eventIndex returns the index of the first event matching the type and component, or -1.
func (l *mockListener) eventIndex(eventType listenerEventType, component testComponent) int {
	for i, e := range l.events {
		if e.eventType == eventType && e.component == component {
			return i
		}
	}
	return -1
}

// RequireStartEmitted asserts OnStart was emitted for each given component.
func (l *mockListener) RequireStartEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnStart, c)
		require.Equal(t, 1, count, "expected exactly one OnStart for %q, got %d", c, count)
	}
}

// RequireReadyEmitted asserts OnReady was emitted for each given component.
func (l *mockListener) RequireReadyEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnReady, c)
		require.Equal(t, 1, count, "expected exactly one OnReady for %q, got %d", c, count)
	}
}

// RequireCloseEmitted asserts OnClose was emitted for each given component.
func (l *mockListener) RequireCloseEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnClose, c)
		require.Equal(t, 1, count, "expected exactly one OnClose for %q, got %d", c, count)
	}
}

// RequireDoneEmitted asserts OnDone was emitted for each given component.
func (l *mockListener) RequireDoneEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnDone, c)
		require.Equal(t, 1, count, "expected exactly one OnDone for %q, got %d", c, count)
	}
}

// RequireShutdownEmitted asserts OnShutdown was emitted exactly once with matching error.
func (l *mockListener) RequireShutdownEmitted(t testing.TB, matcher errMatcher) {
	t.Helper()
	shutdowns := l.eventsOf(listenerEventTypeOnShutdown)
	require.Len(t, shutdowns, 1, "expected exactly one OnShutdown event")
	matcher(t, shutdowns[0].err, "OnShutdown cause")
}

// RequireNoStartEmitted asserts OnStart was NOT emitted for any of the given components.
func (l *mockListener) RequireNoStartEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnStart, c)
		require.Equal(t, 0, count, "expected no OnStart for %q, got %d", c, count)
	}
}

// RequireStartBefore asserts that OnStart for `a` was emitted strictly before OnStart for `b`.
func (l *mockListener) RequireStartBefore(t testing.TB, a, b testComponent) {
	t.Helper()
	idxA := l.eventIndex(listenerEventTypeOnStart, a)
	idxB := l.eventIndex(listenerEventTypeOnStart, b)
	require.NotEqual(t, -1, idxA, "OnStart for %q not found", a)
	require.NotEqual(t, -1, idxB, "OnStart for %q not found", b)
	require.Less(t, idxA, idxB, "expected OnStart(%q) before OnStart(%q)", a, b)
}

// RequireCloseBefore asserts that OnClose for `a` was emitted strictly before OnClose for `b`.
func (l *mockListener) RequireCloseBefore(t testing.TB, a, b testComponent) {
	t.Helper()
	idxA := l.eventIndex(listenerEventTypeOnClose, a)
	idxB := l.eventIndex(listenerEventTypeOnClose, b)
	require.NotEqual(t, -1, idxA, "OnClose for %q not found", a)
	require.NotEqual(t, -1, idxB, "OnClose for %q not found", b)
	require.Less(t, idxA, idxB, "expected OnClose(%q) before OnClose(%q)", a, b)
}

// RequireDoneError asserts that OnDone for `component` was called with an error satisfying matcher.
func (l *mockListener) RequireDoneError(t testing.TB, component testComponent, matcher errMatcher) {
	t.Helper()
	for _, e := range l.events {
		if e.eventType == listenerEventTypeOnDone && e.component == component {
			matcher(t, e.err, "OnDone error for %q", component)
			return
		}
	}
	t.Fatalf("no OnDone event found for %q", component)
}

// RequireShutdownNotEmitted asserts OnShutdown was never emitted.
func (l *mockListener) RequireShutdownNotEmitted(t testing.TB) {
	t.Helper()
	shutdowns := l.eventsOf(listenerEventTypeOnShutdown)
	require.Empty(t, shutdowns, "expected no OnShutdown events, got %d", len(shutdowns))
}

// RequireCloseEmittedWith asserts OnClose was emitted for the component with cause matching matcher.
func (l *mockListener) RequireCloseEmittedWith(t testing.TB, component testComponent, matcher errMatcher) {
	t.Helper()
	for _, e := range l.events {
		if e.eventType == listenerEventTypeOnClose && e.component == component {
			matcher(t, e.err, "OnClose cause for %q", component)
			return
		}
	}
	t.Fatalf("no OnClose event found for %q", component)
}

// RequireNoCloseEmitted asserts OnClose was NOT emitted for any of the given components.
func (l *mockListener) RequireNoCloseEmitted(t testing.TB, components ...testComponent) {
	t.Helper()
	for _, c := range components {
		count := l.countEvents(listenerEventTypeOnClose, c)
		require.Equal(t, 0, count, "expected no OnClose for %q, got %d", c, count)
	}
}

func (l *mockListener) countEvents(eventType listenerEventType, component testComponent) int {
	count := 0
	for _, e := range l.events {
		if e.eventType == eventType && e.component == component {
			count++
		}
	}
	return count
}

// hasEvent returns true if any event matches the type and component.
func (l *mockListener) hasEvent(eventType listenerEventType, component testComponent) bool {
	return l.countEvents(eventType, component) > 0
}

// String returns a human-readable representation for debugging.
func (l *mockListener) String() string {
	s := fmt.Sprintf("mockListener (%d events):\n", len(l.events))
	for i, e := range l.events {
		if e.component != "" {
			s += fmt.Sprintf("  [%d] %s(%s", i, e.eventType, e.component)
		} else {
			s += fmt.Sprintf("  [%d] %s(", i, e.eventType)
		}
		if e.err != nil {
			s += fmt.Sprintf(", err=%v", e.err)
		}
		s += ")\n"
	}
	return s
}

// verify interface compliance
var _ RunnerListener[testComponent] = (*mockListener)(nil)

// verify errMatcher constructors compile
var (
	_ = matchErrorIs(errors.New("test"))
	_ = matchNoError()
	_ = matchAnyError()
)
