package lccore

import (
	"context"
	"time"
)

// Runner keeps and runs graph of components respecting their dependencies and constraints.
// It also handles shutdown signals and errors from components.
type Runner[C comparable] interface {
	// Run method starts the lifecycle of all components in the graph, respecting their dependencies and constraints.
	// - Each component's `OnStart` (if any) is called in dependency order (leaves first)
	// - All currently unblocked starts run concurrently.
	// - The `onReady` callback passed to `Run` is called once all `OnStart` hooks have returned `nil`.
	// - If any `OnStart` returns a non-nil error, shutdown is triggered immediately
	// - Each component's `OnWait` (if any) is called immediately after its `OnStart` returns `nil`
	//   (or skipping the start if the component doesn't have one).
	// - All waits run concurrently and independently
	// - If any `OnWait` returns a non-nil error, shutdown is triggered immediately.
	// - You can configure `RequireAlive` constraints on dependencies, which treat any exit (including `nil`) of a
	//   dependency's `OnWait` as a failure (causing shutdown) if the dependent is still alive.
	// - When shutdown is triggered, `OnClose` hooks are called in reverse dependency order (roots first).
	// - `OnClose` is only called for components that successfully started and whose wait hasn't yet returned
	// - `OnClose` receives a context with an optional timeout applied, independent of the main context
	//   (which is likely already canceled). If ShutdownTimeout is set, all OnClose hooks share a
	//   single context that is canceled after ShutdownTimeout elapses from the moment shutdown begins.
	// - `Run` returns the first shutdown-triggering error wrapped in a `ErrLifecycleHookFailed` if it came from a
	//    lifecycle hook, or `ctx.Err()` if it was an external cancellation.
	// - If there were no `RequireAlive` constraints and all hooks returned `nil`, `Run` returns `nil`.
	Run(ctx context.Context, onReady func()) error
}

// RunnerListener is an interface for listening to lifecycle events of components managed by a Runner
// All methods will be called by Runner in the same goroutine as Runner.Run.
type RunnerListener[C comparable] interface {
	// OnStart is called once for each component when its OnStart hook is entered, before the hook returns
	OnStart(component C)

	// OnReady is called once for each component when its OnStart hook returns nil, before any of
	// its dependents' OnStart hooks are entered
	OnReady(component C)

	// OnClose is called once for each component when its OnClose hook is entered, before the hook returns.
	OnClose(component C, cause error)

	// OnDone is called once for each component when it finished its lifecycle:
	// - OnWait hook returns
	// - OnStart hook fails
	// - The component started and has no OnWait or OnClose hooks
	OnDone(component C, result error)

	// OnShutdown is called once when the Runner is shutting down
	OnShutdown(cause error)
}

// RunnerOptions contains configuration options for a Runner
type RunnerOptions[C comparable] struct {
	// Listener is an optional RunnerListener that will receive lifecycle events of components.
	// If nil, no events are emitted
	Listener RunnerListener[C]

	// StartTimeout is the maximum duration allowed for the entire graph of components to start (defines timeout
	// of a context passed to OnStart hooks). After this timeout, Runner will trigger shutdown
	// and call OnClose hooks for all components that started successfully. Zero value means no timeout.
	StartTimeout time.Duration

	// ShutdownTimeout is the maximum duration allowed for the shutdown phase (from the moment shutdown
	// is triggered until all OnClose hooks return). It defines the timeout of a context passed to
	// OnClose hooks: once shutdown begins, all currently-running and future OnClose hooks share a
	// single context that is canceled after this duration. Runner still waits for all OnClose hooks
	// to return even after the context is canceled. Zero value means no timeout.
	// Components that close naturally before shutdown is triggered are not affected by this timeout.
	ShutdownTimeout time.Duration
}
