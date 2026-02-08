package lccore

import (
	"context"
)

type nodeEdgesMap[C any] map[*gNode[C]]DependencyConstraints

func (edges nodeEdgesMap[C]) add(edge nodeEdge[C]) {
	existing, exists := edges[edge.node]
	if !exists {
		edges[edge.node] = edge.constraints
		return
	}
	edges[edge.node] = existing.andSibling(edge.constraints)
}

func (edges nodeEdgesMap[C]) addVisitResult(
	linkConstraints DependencyConstraints,
	depOwnNode *gNode[C],
	depTransitiveNodes nodeEdgesMap[C],
) {
	if depOwnNode != nil {
		edges.add(nodeEdge[C]{
			node:        depOwnNode,
			constraints: linkConstraints,
		})
	}
	for node, constraints := range depTransitiveNodes {
		edges.add(nodeEdge[C]{
			node:        node,
			constraints: constraints.andDependent(linkConstraints),
		})
	}
}

func (edges nodeEdgesMap[C]) toSlice() []nodeEdge[C] {
	if len(edges) == 0 {
		return nil
	}
	res := make([]nodeEdge[C], 0, len(edges))
	for node, constraints := range edges {
		res = append(res, nodeEdge[C]{
			node:        node,
			constraints: constraints,
		})
	}
	return res
}

func (edges nodeEdgesMap[C]) withDependentConstraints(dependentConstraints DependencyConstraints) nodeEdgesMap[C] {
	if len(edges) == 0 {
		return nil
	}
	res := make(nodeEdgesMap[C], len(edges))
	for node, constraints := range edges {
		res[node] = constraints.andDependent(dependentConstraints)
	}
	return res
}

type nodeEdge[C any] struct {
	node        *gNode[C]
	constraints DependencyConstraints
}

func (e nodeEdge[C]) Node() GraphNode[C] {
	return e.node
}

func (e nodeEdge[C]) Constraints() DependencyConstraints {
	return e.constraints
}

// gNode is internal representation of a component in the graph with the resulting configs calculated
// from own configs of the component and its dependencies and dependents.
// gNodes are created only for components with lc behavior (at least one of OnStart, OnWait, OnClose hooks)
type gNode[C any] struct {
	component C
	onStart   func(ctx context.Context) error
	onWait    func() error
	onClose   func(ctx context.Context) error
	// resulting constraints for the node calculated from own configs
	ownConstraints ComponentOwnConstraints
	// dependents store per-link RequireAlive constraint to check if shutdown should trigger when OnWait exits
	dependents []nodeEdge[C]
	dependsOn  []nodeEdge[C]
}

func (n *gNode[C]) Component() C {
	return n.component
}

func (n *gNode[C]) DependsOn() []GraphEdge[C] {
	res := make([]GraphEdge[C], len(n.dependsOn))
	for i, edge := range n.dependsOn {
		res[i] = edge
	}
	return res
}

type GraphEdge[C any] interface {
	Node() GraphNode[C]
	Constraints() DependencyConstraints
}

type GraphNode[C any] interface {
	Component() C
	DependsOn() []GraphEdge[C]
}

type Graph[C any] interface {
	Roots() []GraphEdge[C]

	self() *graph[C]
}

// graph represents an acyclic directed dependency graph of gNodes
type graph[C any] struct {
	// roots are nodes with no dependents. They are started last and closed first
	roots []nodeEdge[C]
	// leaves are nodes with no dependencies. They are started first and closed last
	leaves []*gNode[C]
	total  int
}

func (gr *graph[C]) Roots() []GraphEdge[C] {
	res := make([]GraphEdge[C], len(gr.roots))
	for i, edge := range gr.roots {
		res[i] = edge
	}
	return res
}

func (gr *graph[C]) self() *graph[C] {
	return gr
}
