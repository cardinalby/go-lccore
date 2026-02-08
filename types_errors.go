package lccore

import (
	"context"
	"fmt"
	"time"
)

type ErrUnexpectedWaitResult[C comparable] struct {
	// Dependent is the component that has constraints regarding the dependency's OnWait result
	Dependent  C
	WaitResult error
}

func (e ErrUnexpectedWaitResult[L]) Error() string {
	return fmt.Sprintf("dependent %v requires alive, but OnWait exited: %v", e.Dependent, e.WaitResult)
}

// ErrCyclicDependency is returned when a cyclic dependency is detected for components with lifecycle behavior
type ErrCyclicDependency[L any] interface {
	error
	// Path is the dependency path leading to the cycle. Contains at least 2 items.
	// First item is the link that caused the first visit of a component, the last item is a link to the same component,
	// causing the cycle detection
	Path() []L
}

type LifecycleHookName string

const (
	LifecycleHookNameOnStart LifecycleHookName = "OnStart"
	LifecycleHookNameOnWait  LifecycleHookName = "OnWait"
	LifecycleHookNameOnClose LifecycleHookName = "OnClose"
)

// ErrLifecycleHookFailed is returned from Runner.Run if a lifecycle hook fails
type ErrLifecycleHookFailed[C comparable] interface {
	error
	// Component returns the component for which the hook failed
	Component() C

	// LifecycleHookName returns the name of the failed lifecycle hook
	LifecycleHookName() LifecycleHookName

	// Unwrap returns the original error returned from the lifecycle hook
	Unwrap() error
}

// OLD SENTINEL ERROR:
// ErrStartTimedOut is returned from Runner.Run and passed to RunnerListener.OnShutdown when
// RunnerOptions.StartTimeout is exceeded before all components finish starting.
// It wraps context.DeadlineExceeded, so both errors.Is(err, ErrStartTimedOut) and
// errors.Is(err, context.DeadlineExceeded) hold.
//var ErrStartTimedOut = fmt.Errorf("start timed out: %w", context.DeadlineExceeded)

type ErrTimedOut struct {
	LifecycleHookName       LifecycleHookName
	Timeout                 time.Duration
	IsRunnerStartTimeout    bool
	IsRunnerShutdownTimeout bool

	// improvement: add a pointer to the DependencyLink that caused the timeout
}

func (e ErrTimedOut) Error() string {
	return fmt.Sprintf("%s: %v timed out after %v", context.DeadlineExceeded.Error(), e.LifecycleHookName, e.Timeout)
}

func (e ErrTimedOut) Unwrap() error {
	return context.DeadlineExceeded
}
