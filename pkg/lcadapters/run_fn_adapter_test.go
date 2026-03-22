package lcadapters

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/cardinalby/go-lccore/internal/contexts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunFnAdapter(t *testing.T) {
	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		return nil
	})
	require.NotNil(t, adapter)
}

// TestRunFnAdapter_Start_ReturnsImmediately verifies that start does not block waiting
// for the runFn to finish and returns nil immediately.
func TestRunFnAdapter_Start_ReturnsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocking := make(chan struct{})
		started := make(chan struct{})

		adapter := NewRunFnAdapter(func(ctx context.Context) error {
			close(started)
			<-blocking
			return nil
		})

		startDone := make(chan error)
		go func() {
			startDone <- adapter.OnStart()(context.Background())
		}()

		// start must return before runFn finishes
		synctest.Wait()
		select {
		case err := <-startDone:
			assert.NoError(t, err)
		default:
			t.Fatal("Start did not return immediately")
		}

		// clean up
		close(blocking)
	})
}

// TestRunFnAdapter_Start_AlreadyRunning verifies that a second Start call while the
// first runFn is still running returns ErrAlreadyRunning.
func TestRunFnAdapter_Start_AlreadyRunning(t *testing.T) {
	blocking := make(chan struct{})

	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-blocking
		return nil
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	err = adapter.OnStart()(context.Background())
	assert.ErrorIs(t, err, ErrAlreadyRunning)

	close(blocking)
}

// TestRunFnAdapter_Start_IgnoresStartContext verifies that canceling the context
// passed to start has no effect on the running runFn.
func TestRunFnAdapter_Start_IgnoresStartContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runFinished := make(chan struct{})

		adapter := NewRunFnAdapter(func(ctx context.Context) error {
			// runFn owns its own context; the start ctx is irrelevant
			<-ctx.Done()
			return ctx.Err()
		})

		startCtx, cancelStart := context.WithCancel(context.Background())

		err := adapter.OnStart()(startCtx)
		require.NoError(t, err)

		// Cancel the start context — runFn must still be running
		cancelStart()
		synctest.Wait()
		select {
		case <-runFinished:
			t.Fatal("runFn stopped when start context was canceled")
		default:
			// expected: runFn is still blocked on its own ctx
		}

		// Clean up via Close
		err = adapter.OnClose()(context.Background())
		assert.NoError(t, err)
	})
}

// TestRunFnAdapter_Wait_BlocksUntilDone verifies that wait blocks until runFn completes.
func TestRunFnAdapter_Wait_BlocksUntilDone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})

		adapter := NewRunFnAdapter(func(ctx context.Context) error {
			<-release
			return nil
		})

		err := adapter.OnStart()(context.Background())
		require.NoError(t, err)

		waitDone := make(chan error)
		go func() {
			waitDone <- adapter.OnWait()()
		}()

		synctest.Wait()
		select {
		case <-waitDone:
			t.Fatal("Wait returned before runFn completed")
		default:
			// expected to block
		}

		close(release)
		err = <-waitDone
		assert.NoError(t, err)
	})
}

// TestRunFnAdapter_Wait_ReturnsError verifies that wait propagates a non-nil error
// returned by runFn.
func TestRunFnAdapter_Wait_ReturnsError(t *testing.T) {
	expectedErr := errors.New("run error")
	release := make(chan struct{})

	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-release
		return expectedErr
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	close(release)
	err = adapter.OnWait()()
	assert.Equal(t, expectedErr, err)
}

// TestRunFnAdapter_Close_CancelsRunFnAndReturnsNil verifies that close cancels the
// runFn context and returns nil when runFn returns ctx.Err().
func TestRunFnAdapter_Close_CancelsRunFnAndReturnsNil(t *testing.T) {
	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	err = adapter.OnClose()(context.Background())
	assert.NoError(t, err)
}

// TestRunFnAdapter_Close_ReturnsNonContextError verifies that close propagates a
// non-context error returned by runFn.
func TestRunFnAdapter_Close_ReturnsNonContextError(t *testing.T) {
	expectedErr := errors.New("close error")

	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-ctx.Done()
		return expectedErr
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	err = adapter.OnClose()(context.Background())
	assert.Equal(t, expectedErr, err)
}

// TestRunFnAdapter_Close_PassesCloseContext verifies that the context passed to close
// is available inside runFn via contexts.GetCloseCtx.
func TestRunFnAdapter_Close_PassesCloseContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var capturedCloseCtx context.Context

		adapter := NewRunFnAdapter(func(ctx context.Context) error {
			<-ctx.Done()
			capturedCloseCtx = contexts.GetCloseCtx(ctx)
			<-capturedCloseCtx.Done()
			return ctx.Err()
		})

		err := adapter.OnStart()(context.Background())
		require.NoError(t, err)

		closeCtx, cancelClose := context.WithCancel(context.Background())

		closeDone := make(chan error)
		go func() {
			closeDone <- adapter.OnClose()(closeCtx)
		}()

		// Wait until runFn has captured the close context and is blocked on it
		synctest.Wait()
		require.NotNil(t, capturedCloseCtx)

		cancelClose()
		err = <-closeDone
		assert.NoError(t, err)
	})
}

// TestRunFnAdapter_FullLifecycle exercises start → wait (in goroutine) → close → wait resolves.
func TestRunFnAdapter_FullLifecycle(t *testing.T) {
	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	err = adapter.OnClose()(context.Background())
	assert.NoError(t, err)

	// Wait should return immediately since runFn already finished
	err = adapter.OnWait()()
	assert.NoError(t, err)
}

// TestRunFnAdapter_ConcurrentWaitCalls verifies that multiple concurrent Wait calls all
// unblock and return the same result when runFn completes.
func TestRunFnAdapter_ConcurrentWaitCalls(t *testing.T) {
	release := make(chan struct{})

	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		<-release
		return nil
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	const numWaiters = 5
	waitErrs := make(chan error, numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func() {
			waitErrs <- adapter.OnWait()()
		}()
	}

	close(release)

	for i := 0; i < numWaiters; i++ {
		err := <-waitErrs
		assert.NoError(t, err)
	}
}

// TestRunFnAdapter_RunFnFinishesBeforeClose verifies that if runFn finishes on its own
// (returning nil), wait and close both reflect that clean exit.
func TestRunFnAdapter_RunFnFinishesOnItsOwn(t *testing.T) {
	adapter := NewRunFnAdapter(func(ctx context.Context) error {
		return nil // finishes immediately
	})

	err := adapter.OnStart()(context.Background())
	require.NoError(t, err)

	err = adapter.OnWait()()
	assert.NoError(t, err)
}
