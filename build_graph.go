package lccore

import (
	"slices"

	"github.com/cardinalby/go-lccore/internal/data_structs/stack"
)

// the first pass: collect extra deps for each component from its dependents, don't detect cycles
// returns map of component -> its extra deps
func collectExtraDeps[C comparable, L DependencyLink[C, L]](
	rootLinks []L,
	getComponentProps GetComponentPropsFunc[C, L],
) map[C][]L {
	visited := make(map[C][]L)

	var visitTopDown func(link DependencyLink[C, L])
	visitTopDown = func(link DependencyLink[C, L]) {
		component := link.Dependency()
		collected, isVisited := visited[component]
		linkExtraDeps := link.TreeExtraDependencies()
		for _, extraDep := range linkExtraDeps {
			visitTopDown(extraDep)
		}
		collected = append(collected, linkExtraDeps...)
		visited[component] = collected
		if isVisited {
			return
		}
		componentProps := getComponentProps(component)
		for _, dep := range componentProps.DependsOn() {
			visitTopDown(dep)
		}
	}

	for _, link := range rootLinks {
		visitTopDown(link)
	}

	return visited
}

func BuildGraph[C comparable, L DependencyLink[C, L]](
	rootLinks []L,
	getComponentProps GetComponentPropsFunc[C, L],
) (Graph[C], error) {
	gr := &graph[C]{}
	collectedExtraDeps := collectExtraDeps(rootLinks, getComponentProps)
	type stackEntry struct {
		link          L
		hasLcBehavior bool
	}
	visitingStack := &stack.Stack[stackEntry]{}

	// component visit result (regardless of link it was visited from)
	type visitResult[C any] struct {
		// false means temporary mark
		isPermanent bool
		own         *gNode[C]
		// closest reachable gNodes with their constraints if the component has no lc behavior
		transitive nodeEdgesMap[C]
	}

	visited := make(map[C]visitResult[C])
	// collect dependents for each component to assign after visiting top-down
	dependentsOf := make(map[*gNode[C]]nodeEdgesMap[C])

	var visitTopDown func(link L, treeExtraDeps []L) (visitResult[C], error)
	visitTopDown = func(link L, treeExtraDeps []L) (visitResult[C], error) {
		component := link.Dependency()
		visitedRes, isVisited := visited[component]

		if isVisited && visitedRes.isPermanent {
			return visitedRes, nil
		}

		componentProps := getComponentProps(component)
		hasOwnLcBehavior := isComponentLcBehaviorSet(componentProps)
		visitingStack.Push(stackEntry{
			link:          link,
			hasLcBehavior: hasOwnLcBehavior,
		})
		defer visitingStack.Pop()

		if isVisited && !visitedRes.isPermanent {
			// is temporarily marked => cycle between components but not necessarily between nodes:
			// 1. If a component has lifecycle behavior and the cycle goes only through components without lifecycle behavior
			// 2. If a component has no lifecycle behavior, but the cycle goes through components with lifecycle behavior
			// In both cases, we can still run these nodes without considering the cyclic component relationships.
			// Check `visitingStack` to detect such cases
			countOfComponentsWithLcInCycle := 0
			if hasOwnLcBehavior {
				countOfComponentsWithLcInCycle++
			}
			// iterate intermediate nodes between two `Component` entries:
			// - skip the last component in visitingStack since it equals `component`,
			// - iterate till the next entry of `component`
			// improvement: allow start <-> close cycles since they don't block each other
			cycleStartIdx := -1
			stackLen := len(*visitingStack)
			for i := stackLen - 2; i >= 0; i-- {
				stackEntry := (*visitingStack)[i]
				if stackEntry.link.Dependency() == component {
					cycleStartIdx = i
					break
				}
				if stackEntry.hasLcBehavior {
					countOfComponentsWithLcInCycle++
				}
			}
			if countOfComponentsWithLcInCycle > 1 {
				err := errCyclicDependency[L]{
					path: make([]L, 0, stackLen-cycleStartIdx),
				}
				for i := cycleStartIdx; i < stackLen; i++ {
					err.path = append(err.path, (*visitingStack)[i].link)
				}
				return visitResult[C]{}, err
			}
			return visitedRes, nil
		}
		var res visitResult[C]
		// not visited yet, set temporary mark
		visited[component] = res

		componentCollectedExtraDeps := collectedExtraDeps[component]
		depEdges := make(nodeEdgesMap[C])

		treeExtraDeps = append(slices.Clone(treeExtraDeps), componentCollectedExtraDeps...)
		for _, extraDep := range treeExtraDeps {
			extraDepVisitRes, err := visitTopDown(extraDep, nil)
			if err != nil {
				return res, err
			}
			if extraDepVisitRes.own == nil && len(extraDepVisitRes.transitive) == 0 {
				continue
			}
			depEdges.addVisitResult(extraDep.Constraints(), extraDepVisitRes.own, extraDepVisitRes.transitive)
		}

		for _, dep := range componentProps.DependsOn() {
			depVisitRes, err := visitTopDown(dep, treeExtraDeps)
			if err != nil {
				return res, err
			}
			if depVisitRes.own == nil && len(depVisitRes.transitive) == 0 {
				continue
			}
			depEdges.addVisitResult(dep.Constraints(), depVisitRes.own, depVisitRes.transitive)
		}

		if hasOwnLcBehavior {
			res.own = &gNode[C]{
				component:      component,
				onStart:        componentProps.OnStart(),
				onWait:         componentProps.OnWait(),
				onClose:        componentProps.OnClose(),
				ownConstraints: componentProps.OwnConstraints(),
				dependsOn:      depEdges.toSlice(),
			}
			gr.total++
			if len(res.own.dependsOn) == 0 {
				gr.leaves = append(gr.leaves, res.own)
			}
			// Register this node as a dependent of its dependencies to assign dependents after visiting all nodes
			for _, depGNode := range res.own.dependsOn {
				dependentsEdgesMap, ok := dependentsOf[depGNode.node]
				if !ok {
					dependentsEdgesMap = make(nodeEdgesMap[C])
					dependentsOf[depGNode.node] = dependentsEdgesMap
				}
				dependentsEdgesMap.add(nodeEdge[C]{
					node:        res.own,
					constraints: depGNode.constraints,
				})
			}
		} else {
			res.transitive = depEdges
		}

		res.isPermanent = true
		visited[component] = res
		return res, nil
	}

	// visit all root links
	rootEdges := make(nodeEdgesMap[C])
	for _, rootLink := range rootLinks {
		res, err := visitTopDown(rootLink, nil)
		if err != nil {
			return nil, err
		}
		if res.own == nil && len(res.transitive) == 0 {
			continue
		}
		rootEdges.addVisitResult(rootLink.Constraints(), res.own, res.transitive)
	}
	gr.roots = rootEdges.toSlice()

	// assign dependents to gNodes
	for gNode, dependents := range dependentsOf {
		gNode.dependents = dependents.toSlice()
	}

	return gr, nil
}
