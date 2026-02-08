package lcadapters

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cardinalby/go-lccore/internal/contexts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRunnable is a test implementation of Runnable
type mockRunnable struct {
	runFunc     func(ctx context.Context, onReady func()) error
	runCalled   atomic.Bool
	runCtx      context.Context
	readyCalled atomic.Bool
}

func (m *mockRunnable) Run(ctx context.Context, onReady func()) error {
	m.runCalled.Store(true)
	m.runCtx = ctx
	if m.runFunc != nil {
		return m.runFunc(ctx, onReady)
	}
	onReady()
	<-ctx.Done()
	return ctx.Err()
}

func TestNewRunnableNodeBehaviorAdapter(t *testing.T) {
	runnable := &mockRunnable{}
	adapter := NewRunFnAdapter(runnable.Run)
	require.NotNil(t, adapter)
}

func TestRunnableAdapter_Start_Success(t *testing.T) {
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-ctx.Done()
			return nil
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	err := adapter.OnStart()(ctx)
	assert.NoError(t, err)
	assert.True(t, runnable.runCalled.Load())
}

func TestRunnableAdapter_Start_ImmediateReturn(t *testing.T) {
	expectedErr := errors.New("immediate error")
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			return expectedErr
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	err := adapter.OnStart()(ctx)
	assert.Equal(t, expectedErr, err)
}

func TestRunnableAdapter_Start_ContextCanceledBeforeReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		readyBlocker := make(chan struct{})
		runnable := &mockRunnable{
			runFunc: func(ctx context.Context, onReady func()) error {
				<-readyBlocker // block until test cancels context
				onReady()
				<-ctx.Done()
				return ctx.Err()
			},
		}

		adapter := NewRunFnAdapter(runnable.Run)
		ctx, cancel := context.WithCancel(context.Background())

		startDone := make(chan error)
		go func() {
			startDone <- adapter.OnStart()(ctx)
		}()

		// Wait for goroutine to be blocked
		synctest.Wait()
		cancel()
		close(readyBlocker)

		err := <-startDone
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})
}

func TestRunnableAdapter_Start_AlreadyRunning(t *testing.T) {
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-ctx.Done()
			return nil
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	err := adapter.OnStart()(ctx)
	require.NoError(t, err)

	// Try to start again while still running
	err = adapter.OnStart()(ctx)
	assert.ErrorIs(t, err, ErrAlreadyRunning)
}

func TestRunnableAdapter_Wait_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		readySignal := make(chan struct{})
		runnable := &mockRunnable{
			runFunc: func(ctx context.Context, onReady func()) error {
				onReady()
				<-readySignal
				return nil
			},
		}

		adapter := NewRunFnAdapter(runnable.Run)
		ctx := context.Background()

		err := adapter.OnStart()(ctx)
		require.NoError(t, err)

		waitDone := make(chan error)
		go func() {
			waitDone <- adapter.OnWait()()
		}()

		// Wait should block until runFn completes
		synctest.Wait()
		select {
		case <-waitDone:
			t.Fatal("Wait returned too early")
		default:
			// Expected to block
		}

		close(readySignal)
		err = <-waitDone
		assert.NoError(t, err)
	})
}

func TestRunnableAdapter_Wait_WithError(t *testing.T) {
	expectedErr := errors.New("run error")
	readySignal := make(chan struct{})
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-readySignal
			return expectedErr
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	err := adapter.OnStart()(ctx)
	require.NoError(t, err)

	close(readySignal)
	err = adapter.OnWait()()
	assert.Equal(t, expectedErr, err)
}

func TestRunnableAdapter_Close_Success(t *testing.T) {
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-ctx.Done()
			// Perform cleanup with close context
			closeCtx := contexts.GetCloseCtx(ctx)
			assert.NotNil(t, closeCtx)
			return ctx.Err()
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	startCtx := context.Background()

	err := adapter.OnStart()(startCtx)
	require.NoError(t, err)

	closeCtx := context.Background()
	err = adapter.OnClose()(closeCtx)
	assert.NoError(t, err)
}

func TestRunnableAdapter_Close_ReturnsNonContextError(t *testing.T) {
	expectedErr := errors.New("close error")
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-ctx.Done()
			return expectedErr
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	startCtx := context.Background()

	err := adapter.OnStart()(startCtx)
	require.NoError(t, err)

	closeCtx := context.Background()
	err = adapter.OnClose()(closeCtx)
	assert.Equal(t, expectedErr, err)
}

func TestRunnableAdapter_Close_PassesCloseContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var capturedCloseCtx context.Context
		runnable := &mockRunnable{
			runFunc: func(ctx context.Context, onReady func()) error {
				onReady()
				<-ctx.Done()
				capturedCloseCtx = contexts.GetCloseCtx(ctx)
				// Simulate cleanup that uses close context
				<-capturedCloseCtx.Done()
				return ctx.Err()
			},
		}

		adapter := NewRunFnAdapter(runnable.Run)
		startCtx := context.Background()

		err := adapter.OnStart()(startCtx)
		require.NoError(t, err)

		closeCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		closeDone := make(chan error)
		go func() {
			closeDone <- adapter.OnClose()(closeCtx)
		}()

		// Wait for close context to be set up
		synctest.Wait()
		require.NotNil(t, capturedCloseCtx)

		cancel()
		err = <-closeDone
		assert.NoError(t, err)
	})
}

func TestRunnableAdapter_FullLifecycle(t *testing.T) {
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-ctx.Done()
			return nil
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)

	// Start
	startCtx := context.Background()
	err := adapter.OnStart()(startCtx)
	require.NoError(t, err)

	// Close
	closeCtx := context.Background()
	err = adapter.OnClose()(closeCtx)
	assert.NoError(t, err)

	// Wait should return immediately after close
	err = adapter.OnWait()()
	assert.NoError(t, err)
}

func TestRunnableAdapter_ConcurrentWaitCalls(t *testing.T) {
	readySignal := make(chan struct{})
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			<-readySignal
			return nil
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	err := adapter.OnStart()(ctx)
	require.NoError(t, err)

	// Multiple concurrent Wait calls
	const numWaiters = 5
	waitErrs := make(chan error, numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func() {
			waitErrs <- adapter.OnWait()()
		}()
	}

	close(readySignal)

	for i := 0; i < numWaiters; i++ {
		err := <-waitErrs
		assert.NoError(t, err)
	}
}

func TestRunnableAdapter_Start_ReadyCalledMultipleTimes(t *testing.T) {
	runnable := &mockRunnable{
		runFunc: func(ctx context.Context, onReady func()) error {
			onReady()
			onReady() // Call ready multiple times
			onReady()
			<-ctx.Done()
			return nil
		},
	}

	adapter := NewRunFnAdapter(runnable.Run)
	ctx := context.Background()

	// Should not panic or cause issues
	err := adapter.OnStart()(ctx)
	assert.NoError(t, err)

	closeCtx := context.Background()
	err = adapter.OnClose()(closeCtx)
	assert.NoError(t, err)
}

func TestRunnableAdapter_Start_ReadyNeverCalled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runnable := &mockRunnable{
			runFunc: func(ctx context.Context, onReady func()) error {
				// Never call onReady
				<-ctx.Done()
				return ctx.Err()
			},
		}

		adapter := NewRunFnAdapter(runnable.Run)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := adapter.OnStart()(ctx)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	})
}

func TestRunnableAdapter_CloseContext_UsedDuringCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanupDone := make(chan struct{})
		var cleanupErr error

		runnable := &mockRunnable{
			runFunc: func(ctx context.Context, onReady func()) error {
				onReady()
				<-ctx.Done()

				// Use close context for cleanup
				closeCtx := contexts.GetCloseCtx(ctx)
				select {
				case <-closeCtx.Done():
					cleanupErr = closeCtx.Err()
				default:
					// Cleanup completed within timeout
				}
				close(cleanupDone)
				return nil
			},
		}

		adapter := NewRunFnAdapter(runnable.Run)
		startCtx := context.Background()

		err := adapter.OnStart()(startCtx)
		require.NoError(t, err)

		closeCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		err = adapter.OnClose()(closeCtx)
		assert.NoError(t, err)

		<-cleanupDone
		assert.NoError(t, cleanupErr)
	})
}

func TestErrAlreadyRunning(t *testing.T) {
	assert.Equal(t, "runFn is already running", ErrAlreadyRunning.Error())
}
