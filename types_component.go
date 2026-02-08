package lccore

import (
	"context"
)

// DependencyLink represents a dependency link from one component to another with
// configuration for the dependency relationship
type DependencyLink[C comparable, L any] interface {
	// Dependency is the component that the dependent component depends on
	Dependency() C

	// Constraints sets the properties of the dependency link. The resulting
	// effective behavior for the dependency is calculated as the most restrictive among all
	// links from dependents
	Constraints() DependencyConstraints

	// TreeExtraDependencies specifies extra dependencies that apply to the entire dependency subtree
	// rooted at Dependency component. This advanced feature lets you conditionally inject dependencies
	// into a subtree when it's reached through this particular link, without modifying individual components.
	// Effectively all components in the subtree will depend on TreeExtraDependencies, but the actual
	// implementation may change to reduce the number of edges in the graph
	TreeExtraDependencies() []L
}

// ComponentProps contains the properties of a component returned by GetComponentProps function
type ComponentProps[
	C comparable,
	L DependencyLink[C, L],
] interface {
	// OnStart returns a hook for the first lc phase. If nil, Start is considered immediately successful.
	// The hook blocks Start of all direct/transitive dependent components until it returns.
	// This behavior is typical for:
	// - DB migrations
	// - one-time initialization tasks (check DB schema, kafka topics configs, etc...),
	// - configuration providers
	// - connections to DBs/queues that need to be established (normally have corresponding Close behavior)
	// If Start was not successful, dependent components won't be started and Wait/Close phases won't be called
	// for this component. It should:
	// - block until the component is ready to be used and its dependents can Start
	// - return a non-nil error if the component can't be started (ctx.Err() if ctx is done)
	OnStart() func(ctx context.Context) error

	// OnWait returns a hook for the middle lc phase. The hook:
	// - is called for a component that successfully started (see OnStart)
	// - blocks until the component finishes its job or fails
	// - returns nil error if the component finishes successfully and doesn't want to trigger shutdown,
	//   or if there is no active job to wait for, and it can be closed once it has no active dependents
	//   (see RequireAlive)
	// - returns non-nil error if waiting failed and shutdown needs to be triggered
	// Long running Wait behavior is typical for:
	// - servers that serve requests until they are stopped
	// - message consumers that process jobs until they are stopped
	// If nil, the component is considered to have no active job to wait for and can be closed immediately after Start
	// if it has no direct or transitive "waiting" dependents. If no components in a graph have OnWait hooks,
	// Runner will initiate close phase immediately after starting all components and calling onReady callback.
	OnWait() func() error

	// OnClose returns a hook for the last lc phase. If nil, Close phase is skipped (considered successful).
	// The hook is called for components in case of shutdown and blocks OnClose of dependencies
	// until it returns. This behavior is typical for:
	// - buffering message queue producers/caches/repos that should flush their buffers
	// - connection pools with lazy connections (sql.DB)
	// - externally started timers, opened files, etc...
	// Close is called only if OnStart was successful and OnWait is nil or still running. It should:
	// - block until the component is stopped
	// - return a non-nil error if the component can't be closed (ctx.Err() if `ctx` is done)
	// `ctx` should be used to perform any context-aware cleanup operations
	OnClose() func(ctx context.Context) error

	// OwnConstraints contains constraints defined at the component level.
	// They are not propagated to the dependencies.
	OwnConstraints() ComponentOwnConstraints

	// DependsOn specifies direct dependencies of the component
	DependsOn() []L
}

// GetComponentPropsFunc is used to get ComponentProps for a generic component
type GetComponentPropsFunc[C comparable, L DependencyLink[C, L]] func(C) ComponentProps[C, L]

// DependencyConstraints specifies the properties of a dependency link.
type DependencyConstraints struct {
	// RequireAlive declares that this dependent requires (until closed) the dependency's OnWait
	// to keep running. If the dependency's OnWait exits while this dependent is
	// not closed yet, shutdown is triggered. Similar to systemd's BindsTo.
	// - true: bind lifecycle to dependency staying alive
	// - false (default): only care about Start/Close ordering
	// It's not propagated to transitive dependencies. If the dependency has no OnWait hook,
	// this constraint has no effect.
	RequireAlive bool

	// Improvement: add tree start/close timeouts
}

// combines constraints from multiple links to the same dependency from the same dependent.
// RequireAlive uses OR: if any link requires the dependency to stay alive, the combined constraint does too.
func (c DependencyConstraints) andSibling(sibling DependencyConstraints) DependencyConstraints {
	return DependencyConstraints{
		RequireAlive: c.RequireAlive || sibling.RequireAlive,
	}
}

// combines B -> C constraints with A -> B constraints in A -> B -> C chain where B doesn't have its own lifecycle
// behavior. RequireAlive uses AND: it is only propagated if both the B->C and A->B links require it.
func (c DependencyConstraints) andDependent(parent DependencyConstraints) DependencyConstraints {
	return DependencyConstraints{
		RequireAlive: c.RequireAlive && parent.RequireAlive,
	}
}

type ComponentOwnConstraints struct {
	// Improvement: add start/close timeouts for own phases
}

func isComponentLcBehaviorSet[C comparable, L DependencyLink[C, L]](c ComponentProps[C, L]) bool {
	return c.OnStart() != nil || c.OnWait() != nil || c.OnClose() != nil
}
