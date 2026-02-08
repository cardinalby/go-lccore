package lccore

import (
	"context"
	"testing"
)

// 6.1: Graph A→B, both start only (no wait, no close). Runner returns nil immediately.
func TestRunner_StartOnly_MultipleNodes(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().DependsOn("B").Done().
		Node("B").HasStart().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()

	a.Start().WaitEntered(t)
	a.Start().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireStartBefore(t, "B", "A")
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 6.2: Graph A→B, both have start+close (no wait). Close order: A then B.
func TestRunner_StartClose_NoWait_ChainCloseOrder(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()

	a.Start().WaitEntered(t)
	a.Start().Succeed()

	// A's wait skipped → close triggers immediately (no dependents)
	a.Close().WaitEntered(t)
	a.Close().Succeed()

	// A done → B's close triggers
	b.Close().WaitEntered(t)
	b.Close().Succeed()

	handle.RequireDone(t, matchNoError())
	s.RequireClosedBefore(t, "A", "B")
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 6.3: A→B where A has start+wait+close, B has start+close (no wait). Cancel triggers close chain.
func TestRunner_MixedHooks_StartWait_And_StartClose(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	// B has no wait — B's close doesn't trigger yet (A is not done)

	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A's close enters (wait in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// A done → B's close triggers
	b.Close().WaitEntered(t)
	b.Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireCloseEmitted(t, "A", "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.4: A→B where A has start+wait+close, B has wait only — edge case.
// B has no OnClose; without it the runner cannot notify B to stop.
// The test manually unblocks B's wait after A closes, simulating B observing the shutdown via context.
func TestRunner_WaitOnly_Node_InChain(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	// B has no start → immediately ready; B's wait enters
	b.Wait().WaitEntered(t)

	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A closed (root), B has no close
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	// B's wait must be unblocked (no close to signal it)
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoCloseEmitted(t, "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.5: A→B where A has start+wait+close, B has close only. Cancel triggers A's close, then B's.
func TestRunner_CloseOnly_Node_InChain(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	// B has no start → immediately ready; B has no wait → wait skipped.
	// B close doesn't trigger yet (A not done).
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A closes (root, wait in progress)
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	// A done → B close triggers
	b.Close().WaitEntered(t)
	b.Close().Succeed()

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireCloseEmitted(t, "A", "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.6: A with start+wait (no close) — edge case. Cancel: A has no OnClose so the test unblocks wait manually.
// This verifies that a component without OnClose still completes cleanly if it exits its wait on its own.
func TestRunner_StartWait_NoClose_CtxCancel(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// No close hook; must unblock wait
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoCloseEmitted(t, "A")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.8: A→B where A has start+wait+close, B has wait+close (no start).
// Cancel triggers A's close, then B's close; B's wait must still be unblocked.
func TestRunner_WaitClose_Node_InChain(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	// B has no start → immediately ready; B's wait enters
	b.Wait().WaitEntered(t)

	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A closes first (root), then B
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoStartEmitted(t, "B")
	s.Listener().RequireCloseEmitted(t, "A", "B")
	s.RequireClosedBefore(t, "A", "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.9: A→B where A has start+wait+close, B has wait+close (no start). B's wait exits nil naturally.
// Close skipped for B (wait already done); A also exits nil → runner returns nil.
func TestRunner_WaitClose_Node_InChain_NaturalCompletion(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Wait().WaitEntered(t)
	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	// Natural exit: B's wait returns nil first, then A's
	b.Wait().Succeed()
	a.Wait().Succeed()

	handle.RequireDone(t, matchNoError())
	s.Listener().RequireNoStartEmitted(t, "B")
	// B's close was skipped (wait already exited before shutdown)
	s.Listener().RequireNoCloseEmitted(t, "B")
	s.Listener().RequireDoneError(t, "A", matchNoError())
	s.Listener().RequireDoneError(t, "B", matchNoError())
	s.Listener().RequireShutdownNotEmitted(t)
	s.AssertAll(t)
}

// 6.10: A→B where A has start+wait+close, B has start+wait (no close).
// B is a dependency with no close hook; cancel must unblock B's wait manually.
func TestRunner_StartWait_Node_AsDependency(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	b.Start().WaitEntered(t)
	b.Start().Succeed()
	b.Wait().WaitEntered(t)

	a.Start().WaitEntered(t)
	a.Start().Succeed()
	a.Wait().WaitEntered(t)

	cancel()

	// A closes (root); B has no close → must unblock B's wait manually
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)
	// B has no close hook; it must exit its wait on its own (simulated here)
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoCloseEmitted(t, "B")
	s.Listener().RequireCloseEmitted(t, "A")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.11: A→B where A has wait+close (no start), B has start+wait+close.
// A must wait for B to be ready before A is considered ready.
// Cancel triggers close in order: A then B.
func TestRunner_WaitClose_DependsOn_StartWaitClose(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasWait().HasClose().DependsOn("B").Done().
		Node("B").HasStart().HasWait().HasClose().Done().
		Roots("A").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	b := s.Component("B")
	a := s.Component("A")

	// B starts first (leaf); A has no start so it becomes ready once B is ready
	b.Start().WaitEntered(t)
	b.Start().Succeed()
	b.Wait().WaitEntered(t)

	// A has no start → wait enters after B is ready
	a.Wait().WaitEntered(t)

	cancel()

	// A closes first (root), then B
	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	b.Close().WaitEntered(t)
	b.Close().Succeed()
	b.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	s.Listener().RequireNoStartEmitted(t, "A")
	s.Listener().RequireCloseEmitted(t, "A", "B")
	s.RequireClosedBefore(t, "A", "B")
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}

// 6.7: Two independent roots A (start+wait+close) and B (start only). Cancel after both ready.
func TestRunner_AllHookCombinations_TwoIndependentRoots(t *testing.T) {
	s := newScenarioBuilder().
		Node("A").HasStart().HasWait().HasClose().Done().
		Node("B").HasStart().Done().
		Roots("A", "B").
		Build(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := s.Run(ctx, nil)

	a := s.Component("A")
	b := s.Component("B")

	a.Start().WaitEntered(t)
	b.Start().WaitEntered(t)

	a.Start().Succeed()
	b.Start().Succeed()

	// Wait for A's wait to start, which confirms both A and B have had their starts
	// processed (A was started after B or concurrently, but runner processes them sequentially).
	// Also wait for B's start to exit so its result is in the runner's pipeline.
	a.Wait().WaitEntered(t)
	b.Start().WaitExited(t)

	cancel()

	a.Close().WaitEntered(t)
	a.Close().Succeed()
	a.Wait().Fail(context.Canceled)

	handle.RequireDone(t, matchErrorIs(context.Canceled))
	// B's OnDone should be emitted (even though it finished much earlier)
	s.Listener().RequireDoneEmitted(t, "B")
	s.Listener().RequireDoneError(t, "B", matchNoError())
	s.Listener().RequireShutdownEmitted(t, matchErrorIs(context.Canceled))
	s.AssertAll(t)
}
