package lccore

import (
	"testing"
)

type depSpec struct {
	dep         testComponent
	constraints DependencyConstraints
}

type nodeSpec struct {
	name      testComponent
	hasStart  bool
	hasWait   bool
	hasClose  bool
	dependsOn []depSpec
}

type nodeBuilder struct {
	spec   *nodeSpec
	parent *scenarioBuilder
}

func (n *nodeBuilder) HasStart() *nodeBuilder {
	n.spec.hasStart = true
	return n
}

func (n *nodeBuilder) HasWait() *nodeBuilder {
	n.spec.hasWait = true
	return n
}

func (n *nodeBuilder) HasClose() *nodeBuilder {
	n.spec.hasClose = true
	return n
}

// DependsOn declares dependency with default (zero) constraints.
func (n *nodeBuilder) DependsOn(dep testComponent) *nodeBuilder {
	n.spec.dependsOn = append(n.spec.dependsOn, depSpec{dep: dep})
	return n
}

// DependsOnWith declares dependency with explicit constraints.
func (n *nodeBuilder) DependsOnWith(dep testComponent, c DependencyConstraints) *nodeBuilder {
	n.spec.dependsOn = append(n.spec.dependsOn, depSpec{dep: dep, constraints: c})
	return n
}

// Done returns to the parent builder for chaining.
func (n *nodeBuilder) Done() *scenarioBuilder {
	return n.parent
}

// scenarioBuilder provides a fluent API for declaring the graph shape.
type scenarioBuilder struct {
	nodes   map[testComponent]*nodeSpec
	roots   []testComponent
	options RunnerOptions[testComponent]
}

func newScenarioBuilder() *scenarioBuilder {
	return &scenarioBuilder{
		nodes: make(map[testComponent]*nodeSpec),
	}
}

// Node starts or retrieves the spec for a node.
func (b *scenarioBuilder) Node(name testComponent) *nodeBuilder {
	spec, ok := b.nodes[name]
	if !ok {
		spec = &nodeSpec{name: name}
		b.nodes[name] = spec
	}
	return &nodeBuilder{spec: spec, parent: b}
}

// Roots declares which components are graph roots.
func (b *scenarioBuilder) Roots(names ...testComponent) *scenarioBuilder {
	b.roots = names
	return b
}

// WithOptions sets RunnerOptions overrides.
func (b *scenarioBuilder) WithOptions(opts RunnerOptions[testComponent]) *scenarioBuilder {
	b.options = opts
	return b
}

// Build materialises the graph and returns the scenario.
func (b *scenarioBuilder) Build(t testing.TB) *scenario {
	t.Helper()

	// Create mockComponents for each node spec
	components := make(map[testComponent]*mockComponent, len(b.nodes))
	for name, spec := range b.nodes {
		mc := &mockComponent{name: name}
		if spec.hasStart {
			mc.startCtrl = newHookCtrl()
		}
		if spec.hasWait {
			mc.waitCtrl = newHookCtrl()
		}
		if spec.hasClose {
			mc.closeCtrl = newHookCtrl()
		}
		// Build dependency links
		for _, dep := range spec.dependsOn {
			mc.deps = append(mc.deps, testLink{
				dep:         dep.dep,
				constraints: dep.constraints,
			})
		}
		components[name] = mc
	}

	// Build the props collection: map each component to its ComponentProps
	propsCollection := make(testComponentsCollection, len(components))
	for name, mc := range components {
		propsCollection[name] = testComponentProps{
			onStart:   mc.OnStart(),
			onWait:    mc.OnWait(),
			onClose:   mc.OnClose(),
			dependsOn: mc.deps,
		}
	}

	// Build root links
	rootLinks := make([]testLink, len(b.roots))
	for i, r := range b.roots {
		rootLinks[i] = testLink{dep: r}
	}

	// Build the graph
	gr, err := BuildGraph(rootLinks, propsCollection.getProps)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	// Create the listener
	listener := newMockListener()

	// Set listener in options
	opts := b.options
	opts.Listener = listener

	return &scenario{
		t:          t,
		graph:      gr,
		components: components,
		listener:   listener,
		options:    opts,
	}
}
