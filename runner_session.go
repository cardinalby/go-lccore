package lccore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cardinalby/go-lccore/internal/contexts"
	"github.com/cardinalby/go-lccore/internal/tests"
)

type opResult[C comparable] struct {
	node statefulNode[C]
	err  error
}

type lcHookResult[C comparable] struct {
	hookName LifecycleHookName
	opResult opResult[C]
}

type phaseState uint8

const (
	phaseStateNone phaseState = iota
	phaseStateInProgress
	phaseStateCompleted
	phaseStateSkipped
)

func (s phaseState) isDone() bool {
	return s == phaseStateCompleted || s == phaseStateSkipped
}

type gNodeState struct {
	start              phaseState
	wait               phaseState
	close              phaseState
	closedDependencies int
	doneDependents     int
	readyDependencies  int
	shutdownCause      error
}

func (s *gNodeState) isDone() bool {
	return s.wait >= phaseStateCompleted && s.close >= phaseStateCompleted
}

type statefulNode[C comparable] struct {
	*gNode[C]
	state *gNodeState
}

type sessionState[C comparable] struct {
	remainingWaits        int
	remainingCloses       int
	remainingReadySignals int
	nodeStates            map[*gNode[C]]*gNodeState
}

func newSessionState[C comparable](graph *graph[C]) *sessionState[C] {
	nodesCount := graph.total
	return &sessionState[C]{
		remainingReadySignals: nodesCount,
		remainingWaits:        nodesCount,
		remainingCloses:       nodesCount,
		nodeStates:            make(map[*gNode[C]]*gNodeState, nodesCount),
	}
}

func (s *sessionState[C]) isAllDone() bool {
	if tests.IsTestingBuild {
		if s.remainingWaits < 0 {
			panic(fmt.Sprintf("remainingWaits is negative: %d", s.remainingWaits))
		}
		if s.remainingCloses < 0 {
			panic(fmt.Sprintf("remainingCloses is negative: %d", s.remainingCloses))
		}
	}

	return s.remainingWaits+s.remainingCloses == 0
}

func (s *sessionState[C]) markNodeIsReady(node *gNode[C]) {
	s.remainingReadySignals--
	for _, dependent := range node.dependents {
		s.stateOf(dependent.node).readyDependencies++
	}
}

func (s *sessionState[C]) markNodeIsDone(node *gNode[C]) {
	for _, dependency := range node.dependsOn {
		s.stateOf(dependency.node).doneDependents++
	}
}

func (s *sessionState[C]) markNodeIsClosed(node *gNode[C]) {
	s.remainingCloses--
	for _, dependent := range node.dependents {
		s.stateOf(dependent.node).closedDependencies++
	}
}

func (s *sessionState[C]) stateOf(node *gNode[C]) *gNodeState {
	state, ok := s.nodeStates[node]
	if !ok {
		state = &gNodeState{}
		s.nodeStates[node] = state
	}
	return state
}

func (s *sessionState[C]) statefulNode(node *gNode[C]) statefulNode[C] {
	return statefulNode[C]{
		gNode: node,
		state: s.stateOf(node),
	}
}

type runnerSession[C comparable] struct {
	graph            *graph[C]
	options          RunnerOptions[C]
	onReady          func()
	shutdownCause    error // non-nil if global shutdown is in progress
	shutdownErr      error // an error to return from run()
	stats            *sessionState[C]
	startsCtx        context.Context
	cancelStarts     context.CancelCauseFunc
	startedAt        time.Time
	lcHookResults    chan lcHookResult[C]
	closeCtx         context.Context
	cancelCloseAfter func(d time.Duration, cause error)
	cancelCloseCtx   func(cause error)
}

func newRunnerSession[C comparable](
	graph *graph[C],
	options RunnerOptions[C],
	onReady func(),
) *runnerSession[C] {
	if onReady == nil {
		onReady = func() {}
	}
	return &runnerSession[C]{
		graph:         graph,
		options:       options,
		onReady:       onReady,
		stats:         newSessionState[C](graph),
		lcHookResults: make(chan lcHookResult[C]),
	}
}

func (rs *runnerSession[C]) run(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	leaves := rs.graph.leaves
	if len(leaves) == 0 {
		rs.onReady()
		return nil
	}
	rs.startedAt = time.Now()

	if rs.options.StartTimeout > 0 {
		rs.startsCtx, rs.cancelStarts = contexts.WithDeadlineCancelCause(
			ctx,
			time.Now().Add(rs.options.StartTimeout),
			ErrTimedOut{
				LifecycleHookName:    LifecycleHookNameOnStart,
				Timeout:              rs.options.StartTimeout,
				IsRunnerStartTimeout: true,
			},
		)
	} else {
		rs.startsCtx, rs.cancelStarts = context.WithCancelCause(ctx)
	}
	defer rs.cancelStarts(nil)

	// Initialize the shared close context. On shutdown, cancelCloseAfter will be called once
	// to arm the ShutdownTimeout timer; all OnClose hooks share this context.
	rs.closeCtx, rs.cancelCloseAfter, rs.cancelCloseCtx = contexts.WithDeferredCancelCause(context.Background())
	defer rs.cancelCloseCtx(nil)

	for _, leaf := range leaves {
		if done := rs.tryStartNode(rs.stats.statefulNode(leaf)); done {
			return rs.shutdownErr
		}
	}
	return rs.loop(ctx)
}

func (rs *runnerSession[C]) loop(ctx context.Context) error {
	// single loop approach allows handling all states, callbacks, listener calls in a single goroutine to avoid
	// synchronization
	for {
		ctxDone, handleCtxDone := rs.getLoopContextDoneChan(ctx)

		select {
		case phaseRes := <-rs.lcHookResults:
			if done := rs.handleHookResult(phaseRes); done {
				return rs.shutdownErr
			}

		case <-ctxDone:
			if done := handleCtxDone(); done {
				return rs.shutdownErr
			}
			// ctxDone will keep firing; switch to an inner loop that only reads hook results
			for {
				if done := rs.handleHookResult(<-rs.lcHookResults); done {
					return rs.shutdownErr
				}
			}
		}
	}
}

func (rs *runnerSession[C]) getLoopContextDoneChan(
	runCtx context.Context,
) (
	ctxDone <-chan struct{},
	handleCtxDone func() (done bool),
) {
	// During the start phase use startsCtx so StartTimeout is observed as a shutdown trigger.
	// After all components are ready, switch to ctx so we don't spuriously react to a
	// startsCtx deadline that may have already fired.
	if rs.stats.remainingReadySignals > 0 {
		return rs.startsCtx.Done(), func() (done bool) {
			if runCtx.Err() != nil {
				return rs.tryShutDown(runCtx.Err(), context.Cause(runCtx))
			}
			startsCtxCause := context.Cause(rs.startsCtx)
			if errors.As(startsCtxCause, new(ErrTimedOut)) {
				return rs.tryShutDown(startsCtxCause, startsCtxCause)
			}
			return rs.tryShutDown(rs.startsCtx.Err(), startsCtxCause)
		}
	}
	return runCtx.Done(), func() (done bool) {
		return rs.tryShutDown(runCtx.Err(), context.Cause(runCtx))
	}
}

func (rs *runnerSession[C]) handleHookResult(opRes lcHookResult[C]) (done bool) {
	switch opRes.hookName {
	case LifecycleHookNameOnStart:
		return rs.handleNodeStartResult(opRes.opResult, phaseStateCompleted)
	case LifecycleHookNameOnWait:
		return rs.handleNodeWaited(opRes.opResult, phaseStateCompleted)
	case LifecycleHookNameOnClose:
		return rs.handleNodeClosed(opRes.opResult, phaseStateCompleted)
	default:
		panic(fmt.Sprintf("unknown node phase: %v", opRes.hookName))
	}
}

func (rs *runnerSession[C]) tryStartNode(node statefulNode[C]) (done bool) {
	switch {
	case node.state.start.isDone():
		if tests.IsTestingBuild {
			panic(fmt.Sprintf("node %v is already started", node.component))
		}
		return rs.stats.isAllDone()

	case node.state.start == phaseStateInProgress ||
		node.state.readyDependencies < len(node.dependsOn):
		return false
	}

	//goland:noinspection GoBoolExpressions
	if tests.IsTestingBuild && node.state.readyDependencies > len(node.dependsOn) {
		panic(fmt.Sprintf("node %v has more ready dependencies (%d) than it has (%d) dependencies",
			node.component, node.state.readyDependencies, len(node.dependsOn)))
	}

	if node.onStart == nil {
		return rs.handleNodeStartResult(opResult[C]{node: node, err: nil}, phaseStateSkipped)
	}

	if rs.options.Listener != nil {
		rs.options.Listener.OnStart(node.component)
	}

	node.state.start = phaseStateInProgress
	startCtx := rs.startsCtx
	go func() {
		err := node.onStart(startCtx)
		rs.lcHookResults <- lcHookResult[C]{
			hookName: LifecycleHookNameOnStart,
			opResult: opResult[C]{node: node, err: err},
		}
	}()
	return false
}

func (rs *runnerSession[C]) handleNodeStartResult(res opResult[C], doneState phaseState) (done bool) {
	//goland:noinspection GoBoolExpressions
	if tests.IsTestingBuild && res.node.state.readyDependencies != len(res.node.dependsOn) {
		panic(fmt.Sprintf("node %v started with %d ready deps but has %d dependencies",
			res.node.component, res.node.state.readyDependencies, len(res.node.dependsOn)))
	}

	res.node.state.start = doneState

	if res.err != nil {
		return rs.handleNodeStartError(res)
	}

	if done := rs.handleNodeIsReady(res.node); done {
		return true
	}

	if rs.shutdownCause != nil {
		return rs.tryCloseNode(res.node, rs.shutdownCause)
	}

	return rs.stats.isAllDone()
}

func (rs *runnerSession[C]) handleNodeStartError(res opResult[C]) (done bool) {
	//goland:noinspection GoBoolExpressions
	if tests.IsTestingBuild && res.node.onStart == nil {
		panic(fmt.Sprintf("handleNodeStartResult(%v) with err with no onStart", res.node.component))
	}

	rs.stats.markNodeIsDone(res.node.gNode)
	res.err = errLifecycleHookFailed[C]{
		component:         res.node.component,
		lifecycleHookName: LifecycleHookNameOnStart,
		err:               res.err,
	}

	res.node.state.wait = phaseStateSkipped
	res.node.state.close = phaseStateSkipped
	rs.stats.markNodeIsClosed(res.node.gNode)
	rs.stats.remainingWaits--

	if rs.options.Listener != nil {
		rs.options.Listener.OnDone(res.node.component, res.err)
	}

	if done := rs.tryShutDown(res.err, res.err); done {
		return true
	}

	closeCause := rs.shutdownCause
	if closeCause == nil {
		closeCause = res.err
	}
	return rs.tryCloseNodeDependencies(res.node, closeCause)
}

func (rs *runnerSession[C]) handleNodeIsReady(node statefulNode[C]) (done bool) {
	if node.onStart != nil && rs.options.Listener != nil {
		rs.options.Listener.OnReady(node.component)
	}
	rs.stats.markNodeIsReady(node.gNode)

	if rs.shutdownCause != nil {
		return rs.tryWaitForNode(node)
	}

	if rs.stats.remainingReadySignals == 0 {
		rs.onReady()
	} else //goland:noinspection GoBoolExpressions
	if tests.IsTestingBuild && rs.stats.remainingReadySignals < 0 {
		panic(fmt.Sprintf("remainingReadySignals is negative: %d", rs.stats.remainingReadySignals))
	}

	for _, dependent := range node.dependents {
		if done := rs.tryStartNode(rs.stats.statefulNode(dependent.node)); done {
			return true
		}
	}

	return rs.tryWaitForNode(node)
}

func (rs *runnerSession[C]) tryWaitForNode(node statefulNode[C]) (done bool) {
	switch {
	case node.state.wait == phaseStateInProgress:
		return false

	case node.state.wait.isDone():
		return rs.stats.isAllDone()

	case node.onWait == nil:
		return rs.handleNodeWaited(opResult[C]{node: node, err: nil}, phaseStateSkipped)
	}

	node.state.wait = phaseStateInProgress
	go func() {
		err := node.onWait()
		rs.lcHookResults <- lcHookResult[C]{
			hookName: LifecycleHookNameOnWait,
			opResult: opResult[C]{node: node, err: err},
		}
	}()
	return false
}

func (rs *runnerSession[C]) handleNodeWaited(res opResult[C], doneState phaseState) (done bool) {
	rs.stats.remainingWaits--
	res.node.state.wait = doneState

	if doneState == phaseStateCompleted {
		//goland:noinspection GoBoolExpressions
		if tests.IsTestingBuild && res.node.onWait == nil {
			panic(fmt.Sprintf("handleNodeWaited(%v) with no onWait", res.node.component))
		}
		// Check if shutdown should be triggered
		res.err = rs.getUnexpectedWaitResultErr(res.node.gNode, res.err)
		if res.node.state.close == phaseStateNone {
			res.node.state.close = phaseStateSkipped
			rs.stats.markNodeIsClosed(res.node.gNode)
		}
	}

	if res.node.state.isDone() {
		if rs.options.Listener != nil {
			rs.options.Listener.OnDone(res.node.component, res.err)
		}
		rs.stats.markNodeIsDone(res.node.gNode)
	}

	if res.err != nil && rs.shutdownCause == nil {
		res.err = errLifecycleHookFailed[C]{
			component:         res.node.component,
			lifecycleHookName: LifecycleHookNameOnWait,
			err:               res.err,
		}
		if done := rs.tryShutDown(res.err, res.err); done {
			return true
		}
	}

	if rs.shutdownCause != nil ||
		res.err != nil ||
		res.node.state.close.isDone() ||
		res.node.state.doneDependents == len(res.node.dependents) {
		closeCause := rs.shutdownCause
		if closeCause == nil {
			closeCause = res.err
		}
		if done := rs.tryCloseNode(res.node, closeCause); done {
			return true
		}
	}

	return rs.stats.isAllDone()
}

func (rs *runnerSession[C]) tryCloseNode(node statefulNode[C], cause error) (done bool) {
	switch {
	case node.state.start == phaseStateInProgress || node.state.close == phaseStateInProgress:
		return false

	case node.state.close.isDone():
		if node.state.closedDependencies < len(node.dependsOn) {
			return rs.tryCloseNodeDependencies(node, cause)
		}
		return rs.stats.isAllDone()

	case node.state.start == phaseStateNone:
		node.state.start = phaseStateSkipped
		node.state.wait = phaseStateSkipped
		rs.stats.remainingWaits--
		return rs.handleNodeClosed(opResult[C]{node: node, err: cause}, phaseStateSkipped)

	case node.state.wait == phaseStateInProgress:
		if node.state.doneDependents < len(node.dependents) || cause == nil {
			return false
		}
		//goland:noinspection GoBoolExpressions
		if tests.IsTestingBuild && node.state.doneDependents > len(node.dependents) {
			panic(fmt.Sprintf("node %v has more done dependents (%d) than it has (%d) dependents",
				node.component, node.state.doneDependents, len(node.dependents),
			))
		}
	}

	if node.onClose == nil || (node.onWait != nil && node.state.wait.isDone()) {
		return rs.handleNodeClosed(opResult[C]{node: node, err: nil}, phaseStateSkipped)
	}

	if rs.options.Listener != nil {
		rs.options.Listener.OnClose(node.component, cause)
	}

	node.state.close = phaseStateInProgress
	go func() {
		closeErr := node.onClose(rs.closeCtx)
		rs.lcHookResults <- lcHookResult[C]{
			hookName: LifecycleHookNameOnClose,
			opResult: opResult[C]{node: node, err: closeErr},
		}
	}()
	return false
}

func (rs *runnerSession[C]) tryCloseNodeDependencies(node statefulNode[C], cause error) (done bool) {
	for _, dependency := range node.dependsOn {
		if done := rs.tryCloseNode(rs.stats.statefulNode(dependency.node), cause); done {
			return true
		}
	}
	return rs.stats.isAllDone()
}

func (rs *runnerSession[C]) handleNodeClosed(res opResult[C], doneState phaseState) (done bool) {
	//goland:noinspection GoBoolExpressions
	if tests.IsTestingBuild && res.node.state.close.isDone() {
		panic(fmt.Sprintf("node %v is already closed", res.node.component))
	}

	res.node.state.close = doneState
	rs.stats.markNodeIsClosed(res.node.gNode)

	if res.node.state.isDone() {
		if rs.options.Listener != nil {
			rs.options.Listener.OnDone(res.node.component, res.err)
		}
		rs.stats.markNodeIsDone(res.node.gNode)
		closeCause := rs.shutdownCause
		if closeCause == nil {
			closeCause = res.err
		}
		return rs.tryCloseNodeDependencies(res.node, closeCause)
	}
	return false
}

func (rs *runnerSession[C]) tryShutDown(err error, cause error) (done bool) {
	if rs.shutdownCause != nil {
		return rs.stats.isAllDone()
	}

	if rs.options.Listener != nil {
		rs.options.Listener.OnShutdown(cause)
	}

	rs.shutdownCause = cause
	rs.shutdownErr = err
	rs.cancelStarts(cause)

	// Arm the ShutdownTimeout: cancel the shared close context after the configured duration.
	// All OnClose hooks (including any already running before shutdown was triggered) share
	// this context, so the timeout applies globally from the moment shutdown begins.
	// If ShutdownTimeout is 0, cancelCloseAfter is never called and the context stays live.
	if rs.options.ShutdownTimeout > 0 {
		rs.cancelCloseAfter(rs.options.ShutdownTimeout, ErrTimedOut{
			LifecycleHookName:       LifecycleHookNameOnClose,
			Timeout:                 rs.options.ShutdownTimeout,
			IsRunnerShutdownTimeout: true,
		})
	}

	for _, root := range rs.graph.roots {
		if done := rs.tryCloseNode(rs.stats.statefulNode(root.node), cause); done {
			return true
		}
	}
	return rs.stats.isAllDone()
}

func (rs *runnerSession[C]) getUnexpectedWaitResultErr(node *gNode[C], waitResult error) error {
	if waitResult != nil {
		return waitResult
	}
	// check if any of the still alive dependents have RequireAlive constraint for this node
	for _, depEdge := range node.dependents {
		if depEdge.constraints.RequireAlive && !rs.stats.stateOf(depEdge.node).close.isDone() {
			return ErrUnexpectedWaitResult[C]{
				Dependent:  depEdge.node.component,
				WaitResult: nil,
			}
		}
	}
	return nil
}
