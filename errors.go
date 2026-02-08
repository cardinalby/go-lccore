package lccore

import "fmt"

type errCyclicDependency[L any] struct {
	path []L
}

func (e errCyclicDependency[L]) Error() string {
	return "cyclic dependency"
}

func (e errCyclicDependency[L]) Path() []L {
	return e.path
}

type errLifecycleHookFailed[C comparable] struct {
	component         C
	lifecycleHookName LifecycleHookName
	err               error
}

func (e errLifecycleHookFailed[C]) Error() string {
	return fmt.Sprintf("%s of component %v failed: %v", e.lifecycleHookName, e.component, e.err)
}

func (e errLifecycleHookFailed[C]) Component() C {
	return e.component
}

func (e errLifecycleHookFailed[C]) LifecycleHookName() LifecycleHookName {
	return e.lifecycleHookName
}

func (e errLifecycleHookFailed[C]) Unwrap() error {
	return e.err
}
