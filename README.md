[![list](https://github.com/cardinalby/go-lccore/actions/workflows/list.yml/badge.svg)](https://github.com/cardinalby/go-lccore/actions/workflows/list.yml)
[![test](https://github.com/cardinalby/go-lccore/actions/workflows/test.yml/badge.svg)](https://github.com/cardinalby/go-lccore/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cardinalby/go-lccore.svg)](https://pkg.go.dev/github.com/cardinalby/go-lccore)

# go-lccore

`go-lccore` is a Go library that provides the **core machinery for dependency-aware component lifecycle management**. It is intended as a building block for higher-level frameworks rather than as a direct application dependency — it handles graph construction, dependency-ordered hook execution, and observer callbacks, leaving the component model and configuration format entirely to the consumer.

```
go get github.com/cardinalby/go-lccore
```

---

## Usage

1. Prepare components and their properties (see `ComponentProps`) that describe their lifecycle hooks and dependencies.
2. Pass them to `BuildGraph` to construct a `Graph`
3. Create a `Runner` with `NewRunner`, passing the graph and options.
4. Call `Runner.Run` that will go through "Start", "Wait", "Close" lifecycle phases

### Runner behaviour

- Each component's `OnStart` (if any) is called in dependency order (leaves first)
- All currently unblocked starts run concurrently. 
- The `onReady` callback passed to `Run` is called once all `OnStart` hooks have returned `nil`.
- If any `OnStart` returns a non-nil error, shutdown is triggered immediately
- Each component's `OnWait` (if any) is called immediately after its `OnStart` returns `nil` (or skipping the start if
the component doesn't have one). 
- All waits run concurrently and independently
- If any `OnWait` returns a non-nil error, shutdown is triggered immediately.
- You can configure `RequireAlive` constraints on dependencies, which treat any exit (including `nil`) of a 
dependency's `OnWait` as a failure (causing shutdown) if the dependent is still alive.
- When shutdown is triggered, `OnClose` hooks are called in reverse dependency order (roots first). 
- `OnClose` is only called for components that successfully started and whose wait hasn't yet returned
- `OnClose` receives a context with an optional timeout applied, independent of the main context 
(which is likely already canceled).
- `Run` returns the first shutdown-triggering error wrapped in a `ErrLifecycleHookFailed` if it came from a 
lifecycle hook, or `ctx.Err()` if it was an external cancellation.
- If there were no `RequireAlive` constraints and all hooks returned `nil`, `Run` returns `nil`.

### ComponentProps

Read godoc for `ComponentProps` for the full contract

Lifecycle behavior of a component is described via 3 lifecycle hook functions (`OnStart`, `OnWait`, `OnClose`).
Components that don't have any lifecycle behavior can simply return `nil` for all hooks and don't participate in the 
lifecycle at all (they are "transparent" in the graph, with their dependencies flattened through them).

It's the most flexible way to express it since you, but you can use [`NewRunFnAdapter`](./pkg/lcadapters/run_fn_adapter.go) 
if your component uses 
```go
Run(ctx context.Context, onReady func()) error
``` 

pattern, which is common for long-running services. 

Common combinations are:
- `OnStart` only for components that need to be initialized before their dependents can start,
but don't have an active job to wait for or any cleanup behavior
- `OnClose` only for components that need to be cleaned up after their dependents are stopped, but don't have any
initialization or active job behavior
- `OnStart` + `OnClose` for components that need to be initialized and cleaned up but don't have an active 
job to wait for
- `OnWait` + `OnClose` for components that start an active job immediately and need to stop it on shutdown, 
but don't have any initialization behavior (e.g. servers with no warmup)

### Lifecycle hooks

#### 🔻 `OnStart() func(ctx context.Context) error`

`OnStart` returns a hook for the first LC phase. If **nil**, **Start** is considered immediately successful.
The hook blocks **Start** of all direct/transitive dependent components until it returns.
This behavior is typical for:
- DB migrations
- one-time initialization tasks (check DB schema, kafka topics configs, etc...),
- configuration providers
- connections to DBs/queues that need to be established (normally have corresponding Close behavior)

If **Start** was not successful, dependent components won't be started and **Wait/Close** phases won't be called
for this component. It should:
- block until the component is ready to be used and its dependents can **Start**
- return a non-nil error if the component can't be started (`ctx.Err()` if `ctx` is done)

#### 🔻 `OnWait() func() error`

`OnWait` returns a hook for the middle LC phase. The hook:
- is called for a component that successfully **started**
- blocks until the component finishes its job or fails
- returns nil error if the component finishes successfully and doesn't want to trigger shutdown,
  or if there is no active job to wait for, and it can be closed once it has no active dependents
  (see `RequireAlive`)
- returns non-nil error if waiting failed and shutdown needs to be triggered

Long-running **Wait** behavior is typical for:
- servers that serve requests until they are stopped
- message consumers that process jobs until they are stopped

Normally, components with **Wait** **should also have** **Close** behavior to stop the job and unblock the wait 
when shutdown is triggered.

If **nil**, the component is considered to have no active job to wait for and can be closed immediately after **Start**
if it has no direct or transitive "waiting" dependents. 
- If no components in a graph have OnWait hooks, Runner will initiate **Close** phase immediately after 
starting all components and calling **onReady** callback.

#### 🔻 `OnClose() func(ctx context.Context) error`

`OnClose` returns a hook for the last LC phase. If **nil**, **Close** phase is skipped (considered successful).
The hook is called for components in case of shutdown and blocks **OnClose** of dependencies
until it returns. 

This behavior is typical for:
- buffering message queue producers/caches/repos that should flush their buffers
- connection pools with lazy connections (`sql.DB`)
- externally started timers, opened files, etc...

**Close** is called only if **Start** was successful and (**Wait** is still running or there is no **Wait** hook). It should:
- block until the component is stopped
- return a non-nil error if the component can't be closed (ctx.Err() if `ctx` is done)
- `ctx` should be used to perform any context-aware cleanup operations

### Shutdown triggers

Shutdown begins when any of the following occurs:

1. The `ctx` passed to `Runner.Run` is canceled.
2. Any `OnStart` returns a non-nil error.
3. Any `OnWait` returns a non-nil error.
4. A `RequireAlive` constraint is violated.
5. No components in a graph have running OnWait hooks. 
In this case `Run` returns nil after closing all started components.

### Return value of `Runner.Run`

`Run` returns the **first** shutdown-triggering error:

| Situation | Return value |
|-----------|-------------|
| Clean exit (no errors, no cancellation) | `nil` |
| `ctx` canceled externally | `ctx.Err()` |
| `OnStart` or `OnWait` hook failed | `ErrLifecycleHookFailed[C]` wrapping the hook's error |
| `RequireAlive` constraint violated | `ErrUnexpectedWaitResult[C]` |
| `Run` called while already running | `ErrAlreadyRunning` |

## Utils in `pkg` package

- [`pkg/lcadapters`](./pkg/lcadapters) contains `RunFnAdapter` for components that have a single `Run` method instead 
of separate hooks
- [`pkg/contexts`](./pkg/contexts) contains `NewShutdownContext` (similar to `signal.NotifyContext`)