package lccore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper types
type testComponent string

type testLink struct {
	dep         testComponent
	constraints DependencyConstraints
	extraDeps   []testLink
}

func (l testLink) Dependency() testComponent {
	return l.dep
}

func (l testLink) Constraints() DependencyConstraints {
	return l.constraints
}

func (l testLink) TreeExtraDependencies() []testLink {
	return l.extraDeps
}

type testComponentProps struct {
	onStart        func(ctx context.Context) error
	onWait         func() error
	onClose        func(ctx context.Context) error
	ownConstraints ComponentOwnConstraints
	dependsOn      []testLink
}

func (p testComponentProps) OnStart() func(ctx context.Context) error {
	return p.onStart
}

func (p testComponentProps) OnWait() func() error {
	return p.onWait
}

func (p testComponentProps) OnClose() func(ctx context.Context) error {
	return p.onClose
}

func (p testComponentProps) OwnConstraints() ComponentOwnConstraints {
	return p.ownConstraints
}

func (p testComponentProps) DependsOn() []testLink {
	return p.dependsOn
}

// Helper functions
func link(comp testComponent) testLink {
	return testLink{dep: comp}
}

func linkWithConstraints(comp testComponent, constraints DependencyConstraints) testLink {
	return testLink{dep: comp, constraints: constraints}
}

func linkWithExtraDeps(comp testComponent, extraDeps ...testLink) testLink {
	return testLink{dep: comp, extraDeps: extraDeps}
}

func noopStart(ctx context.Context) error { return nil }
func noopWait() error                     { return nil }
func noopClose(ctx context.Context) error { return nil }

type testComponentsCollection map[testComponent]testComponentProps

type nodeOwnRequirement struct {
	c              testComponent
	ownConstraints ComponentOwnConstraints
}

func (n nodeOwnRequirement) testAgainst(t *testing.T, context string, actual *gNode[testComponent]) {
	require.Equal(t, n.c, actual.component,
		"%s: expected component does not match actual", context)
	require.Equal(t, n.ownConstraints, actual.ownConstraints,
		"%s: expected own constraints do not match actual", context)
}

type nodeOwnRequirements []nodeOwnRequirement // order does not matter

func (n nodeOwnRequirements) testAgainst(t *testing.T, context string, actual []*gNode[testComponent]) {
	actualMap := make(map[testComponent]*gNode[testComponent])
	for _, node := range actual {
		actualMap[node.component] = node
	}

	for _, expected := range n {
		node, exists := actualMap[expected.c]
		require.True(t, exists, "%s: expected component %v not found", context, expected.c)
		expected.testAgainst(t, context+" "+string(expected.c), node)
		delete(actualMap, expected.c)
	}
	require.Empty(t, actualMap, "%s: unexpected components found: %v", context, actualMap)
}

type edgeRequirement struct {
	dep         testComponent
	constraints DependencyConstraints
}

type edgeRequirements []edgeRequirement // order does not matter

func (e edgeRequirements) testAgainst(t *testing.T, context string, actual []nodeEdge[testComponent]) {
	actualMap := make(map[testComponent]DependencyConstraints)
	for _, edge := range actual {
		actualMap[edge.Node().Component()] = edge.Constraints()
	}

	// check that all expected dependencies are present with correct constraints and no unexpected dependencies are present
	for _, expected := range e {
		actualConstraints, exists := actualMap[expected.dep]
		require.True(t, exists, "%s: expected dependency %v not found", context, expected.dep)
		require.Equal(t, expected.constraints, actualConstraints,
			"%s: constraints for dependency %v do not match", context, expected.dep)
		delete(actualMap, expected.dep)
	}
	require.Empty(t, actualMap, "%s: unexpected dependencies found: %v", context, actualMap)
}

type nodeLinksRequirement struct {
	dependsOn  edgeRequirements
	dependents edgeRequirements
}

func (n nodeLinksRequirement) testAgainst(t *testing.T, context string, actual *gNode[testComponent]) {
	n.dependsOn.testAgainst(t, context+" dependsOn", actual.dependsOn)
	n.dependents.testAgainst(t, context+" dependents", actual.dependents)
}

type nodeLinksRequirements map[testComponent]nodeLinksRequirement

func (n nodeLinksRequirements) testAgainst(t *testing.T, allActualNodes map[*gNode[testComponent]]struct{}) {
	// check that all expected nodes are present with correct dependencies and no unexpected nodes are present
	perComponentMap := make(map[testComponent]gNode[testComponent])
	for node := range allActualNodes {
		// check only one node per component is present
		_, exists := perComponentMap[node.component]
		require.False(t, exists, "multiple nodes found for component %v", node.component)
		perComponentMap[node.component] = *node
	}

	for expectedComponent, expectedLinks := range n {
		actualNode, exists := perComponentMap[expectedComponent]
		require.True(t, exists, "expected node for component %v not found", expectedComponent)
		expectedLinks.testAgainst(t, "node "+string(expectedComponent), &actualNode)
		delete(perComponentMap, expectedComponent)
	}
	require.Empty(t, perComponentMap, "unexpected nodes found for components: %v", perComponentMap)
}

// is used to describe the required structure of the graph for testing purposes
type graphRequirements struct {
	total  int
	roots  edgeRequirements
	leaves nodeOwnRequirements
	all    nodeLinksRequirements
}

func (r graphRequirements) testAgainst(t *testing.T, grIface Graph[testComponent]) {
	gr, ok := grIface.(*graph[testComponent])
	require.True(t, ok, "graph is not of expected type")
	require.Equal(t, r.total, gr.total, "total nodes count does not match")

	// Check roots, use testAgainst helpers
	r.roots.testAgainst(t, "roots", gr.roots)
	// Check leaves
	r.leaves.testAgainst(t, "leaves", gr.leaves)
	// Check all nodes and their dependencies. Collect all nodes from the graph into a map for easy lookup
	allNodes := make(map[*gNode[testComponent]]struct{})
	for _, rootEdge := range gr.roots {
		var collectNodes func(node *gNode[testComponent])
		collectNodes = func(node *gNode[testComponent]) {
			if _, exists := allNodes[node]; exists {
				return
			}
			allNodes[node] = struct{}{}
			for _, depEdge := range node.dependsOn {
				collectNodes(depEdge.node)
			}
		}
		collectNodes(rootEdge.node)
	}
	r.all.testAgainst(t, allNodes)
}

func (c testComponentsCollection) getProps(component testComponent) ComponentProps[testComponent, testLink] {
	return c[component]
}

func TestBuildGraph_SimpleLinear(t *testing.T) {
	// A -> B -> C, with RequireAlive constraints on every link
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA)}},
		"B": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("C", constrRA)}},
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{linkWithConstraints("A", constrRA)}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  3,
		roots:  edgeRequirements{{dep: "A", constraints: constrRA}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "B", constraints: constrRA}}},
			"B": {dependsOn: edgeRequirements{{dep: "C", constraints: constrRA}}, dependents: edgeRequirements{{dep: "A", constraints: constrRA}}},
			"C": {dependents: edgeRequirements{{dep: "B", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_Diamond(t *testing.T) {
	// A depends on B and C, both B and C depend on D.
	// B -> D has RequireAlive=true, C -> D has RequireAlive=false.
	// D's dependents edge from B carries true, from C carries false (stored per dependent, not merged).
	//    A
	//   / \
	//  B   C
	//   \ /
	//    D
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B"), link("C")}},
		"B": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("D", constrRA)}},
		"C": {onStart: noopStart, dependsOn: []testLink{link("D")}},
		"D": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  4,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "D"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "B"}, {dep: "C"}}},
			"B": {dependsOn: edgeRequirements{{dep: "D", constraints: constrRA}}, dependents: edgeRequirements{{dep: "A"}}},
			"C": {dependsOn: edgeRequirements{{dep: "D"}}, dependents: edgeRequirements{{dep: "A"}}},
			"D": {dependents: edgeRequirements{{dep: "B", constraints: constrRA}, {dep: "C"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_MultipleRoots(t *testing.T) {
	// A -> C, B -> C (two independent roots depending on same component)
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"B": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A"), link("B")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  3,
		roots:  edgeRequirements{{dep: "A"}, {dep: "B"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C"}}},
			"B": {dependsOn: edgeRequirements{{dep: "C"}}},
			"C": {dependents: edgeRequirements{{dep: "A"}, {dep: "B"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_ComponentWithoutLcBehavior(t *testing.T) {
	// A -> B -> C, where B has no lifecycle behavior (should be skipped in graph).
	// Both links carry RequireAlive=true: A -> C synthetic edge gets true AND true = true.
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA)}},
		"B": {dependsOn: []testLink{linkWithConstraints("C", constrRA)}}, // No lc behavior
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C", constraints: constrRA}}},
			"C": {dependents: edgeRequirements{{dep: "A", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_CycleWithLcBehavior(t *testing.T) {
	// A -> B -> C -> A (all have lc behavior)
	components := testComponentsCollection{
		"R": {onStart: noopStart, dependsOn: []testLink{link("A")}},
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"C": {onStart: noopStart, dependsOn: []testLink{link("A")}},
	}

	gr, err := BuildGraph([]testLink{link("R")}, components.getProps)

	require.Error(t, err)
	assert.Nil(t, gr)

	var cyclicErr ErrCyclicDependency[testLink]
	require.ErrorAs(t, err, &cyclicErr)

	// Verify the path contains the cycle information
	expectedPath := []testLink{link("A"), link("B"), link("C"), link("A")}
	require.Equal(t, expectedPath, cyclicErr.Path())
}

func TestBuildGraph_CycleWithoutOwnLcBehavior(t *testing.T) {
	// A -> B -> C -> B, where B has no lc behavior (cycle allowed)
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {dependsOn: []testLink{link("C")}}, // No lc behavior
		"C": {onStart: noopStart, dependsOn: []testLink{link("B")}},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)
	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C"}}},
			"C": {dependents: edgeRequirements{{dep: "A"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_CycleWithoutLcBehaviorInThePath(t *testing.T) {
	// A -> B -> C -> B, where C has no lc behavior (cycle allowed)
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"C": {dependsOn: []testLink{link("B")}}, // No lc behavior
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)
	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "B"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "B"}}},
			"B": {dependents: edgeRequirements{{dep: "A"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_TreeExtraDependencies(t *testing.T) {
	// A -> B with extra dep on D (RequireAlive=true)
	// B -> C
	// Expected: A -> B -> (C, D), C -> D.
	// The RequireAlive=true constraint on the extra dep link is applied directly to B->D and C->D edges.
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {
			onStart: noopStart,
			dependsOn: []testLink{
				linkWithExtraDeps("B", linkWithConstraints("D", constrRA)),
			},
		},
		"B": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"C": {onStart: noopStart},
		"D": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  4,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "D"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "B"}}},
			"B": {dependsOn: edgeRequirements{{dep: "C"}, {dep: "D", constraints: constrRA}}, dependents: edgeRequirements{{dep: "A"}}},
			"C": {dependsOn: edgeRequirements{{dep: "D", constraints: constrRA}}, dependents: edgeRequirements{{dep: "B"}}},
			"D": {dependents: edgeRequirements{{dep: "B", constraints: constrRA}, {dep: "C", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_NestedTreeExtraDependencies(t *testing.T) {
	// A -> B with extra dep on D that in turn has extra dep on E
	// B -> C
	// Expected: A -> B -> (C, D), C -> D -> E
	components := testComponentsCollection{
		"A": {
			onStart: noopStart,
			dependsOn: []testLink{
				linkWithExtraDeps("B", linkWithExtraDeps("D", link("E"))),
			},
		},
		"B": {onStart: noopStart, dependsOn: []testLink{link("C")}},
		"C": {onStart: noopStart},
		"D": {onStart: noopStart},
		"E": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  5,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "E"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "B"}}},
			"B": {dependsOn: edgeRequirements{{dep: "C"}, {dep: "D"}}, dependents: edgeRequirements{{dep: "A"}}},
			"C": {dependsOn: edgeRequirements{{dep: "D"}}, dependents: edgeRequirements{{dep: "B"}}},
			"D": {dependsOn: edgeRequirements{{dep: "E"}}, dependents: edgeRequirements{{dep: "B"}, {dep: "C"}}},
			"E": {dependents: edgeRequirements{{dep: "D"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_ConstraintsSiblingMerge(t *testing.T) {
	// A depends on D directly (RequireAlive=false) and also transitively via B (no lc).
	// A->B is RequireAlive=true, B->D is RequireAlive=true:
	//   transitive A->D constraint = true AND true = true
	// Direct A->D constraint = false.
	// The two A->D edges are siblings merged with andSibling (OR): false OR true = true.
	//    A
	//   / \
	//  B   |
	//  |   |
	//  D<--+
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA), link("D")}},
		"B": {dependsOn: []testLink{linkWithConstraints("D", constrRA)}}, // No lc behavior
		"D": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "D"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "D", constraints: constrRA}}},
			"D": {dependents: edgeRequirements{{dep: "A", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_ConstraintsPassThroughBothTrue(t *testing.T) {
	// A -> B (RequireAlive=true) -> C (RequireAlive=true), B has no lc behavior.
	// Synthetic A->C edge: true AND true = true.
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA)}},
		"B": {dependsOn: []testLink{linkWithConstraints("C", constrRA)}}, // No lc behavior
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C", constraints: constrRA}}},
			"C": {dependents: edgeRequirements{{dep: "A", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_ConstraintsPassThroughDependentFalse(t *testing.T) {
	// A -> B (RequireAlive=false) -> C (RequireAlive=true), B has no lc behavior.
	// Synthetic A->C edge: true AND false = false.
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {dependsOn: []testLink{linkWithConstraints("C", constrRA)}}, // No lc behavior
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C"}}},
			"C": {dependents: edgeRequirements{{dep: "A"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_ConstraintsPassThroughDepFalse(t *testing.T) {
	// A -> B (RequireAlive=true) -> C (RequireAlive=false), B has no lc behavior.
	// Synthetic A->C edge: false AND true = false.
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA)}},
		"B": {dependsOn: []testLink{link("C")}}, // No lc behavior
		"C": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "C"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "C"}}},
			"C": {dependents: edgeRequirements{{dep: "A"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_EmptyRoots(t *testing.T) {
	gr, err := BuildGraph([]testLink{}, func(c testComponent) ComponentProps[testComponent, testLink] {
		return testComponentProps{}
	})

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  0,
		roots:  edgeRequirements{},
		leaves: nodeOwnRequirements{},
		all:    nodeLinksRequirements{},
	}.testAgainst(t, gr)
}

func TestBuildGraph_RootWithNoLcBehaviorAndNoDeps(t *testing.T) {
	// Root component has no lc behavior and no deps — should be skipped entirely
	components := testComponentsCollection{
		"A": {},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  0,
		roots:  edgeRequirements{},
		leaves: nodeOwnRequirements{},
		all:    nodeLinksRequirements{},
	}.testAgainst(t, gr)
}

func TestBuildGraph_RootWithNoLcBehaviorButWithTransitiveLcDep(t *testing.T) {
	// Root A has no lc behavior but depends on B which does. B becomes the root.
	components := testComponentsCollection{
		"A": {dependsOn: []testLink{link("B")}},
		"B": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  1,
		roots:  edgeRequirements{{dep: "B"}},
		leaves: nodeOwnRequirements{{c: "B"}},
		all: nodeLinksRequirements{
			"B": {},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_MultipleRootsOneNoOp(t *testing.T) {
	// A has lc behavior, B has no lc behavior and no deps — B is skipped in roots
	components := testComponentsCollection{
		"A": {onStart: noopStart},
		"B": {},
	}

	gr, err := BuildGraph([]testLink{link("A"), link("B")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  1,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "A"}},
		all: nodeLinksRequirements{
			"A": {},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_OnlyOnWait(t *testing.T) {
	// Component with only OnWait hook — counts as having lc behavior
	components := testComponentsCollection{
		"A": {onWait: noopWait},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  1,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "A"}},
		all: nodeLinksRequirements{
			"A": {},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_OnlyOnClose(t *testing.T) {
	// Component with only OnClose hook — counts as having lc behavior
	components := testComponentsCollection{
		"A": {onClose: noopClose},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  1,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "A"}},
		all: nodeLinksRequirements{
			"A": {},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_OwnConstraintsPropagated(t *testing.T) {
	ownConstr := ComponentOwnConstraints{}
	components := testComponentsCollection{
		"A": {onStart: noopStart, ownConstraints: ownConstr},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  1,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "A", ownConstraints: ownConstr}},
		all: nodeLinksRequirements{
			"A": {},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_GraphRootsPublicAPI(t *testing.T) {
	// Verify the public Graph.Roots() and GraphNode.DependsOn() interfaces work correctly
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA)}},
		"B": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)

	var g = gr
	roots := g.Roots()
	require.Len(t, roots, 1)
	require.Equal(t, testComponent("A"), roots[0].Node().Component())
	require.Equal(t, DependencyConstraints{}, roots[0].Constraints())

	aDeps := roots[0].Node().DependsOn()
	require.Len(t, aDeps, 1)
	require.Equal(t, testComponent("B"), aDeps[0].Node().Component())
	require.Equal(t, constrRA, aDeps[0].Constraints())
}

func TestBuildGraph_CyclicDependencyErrorMessage(t *testing.T) {
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {onStart: noopStart, dependsOn: []testLink{link("A")}},
	}

	_, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.Error(t, err)
	require.Contains(t, err.Error(), "cyclic dependency")
}

func TestBuildGraph_CycleTwoNodes(t *testing.T) {
	// Direct two-node cycle A -> B -> A (both have lc behavior)
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{link("B")}},
		"B": {onStart: noopStart, dependsOn: []testLink{link("A")}},
	}

	_, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.Error(t, err)
	var cyclicErr ErrCyclicDependency[testLink]
	require.ErrorAs(t, err, &cyclicErr)
	// path: [A, B, A] — first entry is where the cycle started, last is the re-visit
	require.Len(t, cyclicErr.Path(), 3)
	require.Equal(t, testComponent("A"), cyclicErr.Path()[0].Dependency())
	require.Equal(t, testComponent("B"), cyclicErr.Path()[1].Dependency())
	require.Equal(t, testComponent("A"), cyclicErr.Path()[2].Dependency())
}

func TestBuildGraph_SharedDepReachedViaMultipleNoLcPaths(t *testing.T) {
	// A -> B (no lc, RA=true), A -> C (no lc, RA=false)
	// B -> D (lc, RA=false), C -> D (lc, RA=true)
	// transitive A->D via B: false AND true(A->B) = false
	// transitive A->D via C: true AND false(A->C) = false
	// sibling merge: false OR false = false
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {onStart: noopStart, dependsOn: []testLink{linkWithConstraints("B", constrRA), link("C")}},
		"B": {dependsOn: []testLink{link("D")}},
		"C": {dependsOn: []testLink{linkWithConstraints("D", constrRA)}},
		"D": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  2,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "D"}},
		all: nodeLinksRequirements{
			"A": {dependsOn: edgeRequirements{{dep: "D"}}},
			"D": {dependents: edgeRequirements{{dep: "A"}}},
		},
	}.testAgainst(t, gr)
}

func TestBuildGraph_TreeExtraDepsOnNoLcComponent(t *testing.T) {
	// A -> B (extra dep E with RA=true); B has no lc behavior, B -> C (lc)
	// E is injected into B's subtree: C depends on E, A depends on both C and E
	constrRA := DependencyConstraints{RequireAlive: true}
	components := testComponentsCollection{
		"A": {
			onStart: noopStart,
			dependsOn: []testLink{
				linkWithExtraDeps("B", linkWithConstraints("E", constrRA)),
			},
		},
		"B": {dependsOn: []testLink{link("C")}}, // no lc behavior
		"C": {onStart: noopStart},
		"E": {onStart: noopStart},
	}

	gr, err := BuildGraph([]testLink{link("A")}, components.getProps)

	require.NoError(t, err)
	require.NotNil(t, gr)
	graphRequirements{
		total:  3,
		roots:  edgeRequirements{{dep: "A"}},
		leaves: nodeOwnRequirements{{c: "E"}},
		all: nodeLinksRequirements{
			// A->B has RA=false (default), so A->E = E(RA=true).andDependent(A->B RA=false) = false
			// C->E: C gets extra dep E with RA=true directly, C has lc behavior => C->E = RA=true
			"A": {dependsOn: edgeRequirements{{dep: "C"}, {dep: "E"}}},
			"C": {dependsOn: edgeRequirements{{dep: "E", constraints: constrRA}}, dependents: edgeRequirements{{dep: "A"}}},
			"E": {dependents: edgeRequirements{{dep: "A"}, {dep: "C", constraints: constrRA}}},
		},
	}.testAgainst(t, gr)
}
