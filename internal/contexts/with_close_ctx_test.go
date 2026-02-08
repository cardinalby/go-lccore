package contexts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBgWithCloseCtxCancelCause(t *testing.T) {
	t.Parallel()

	testCause := errors.New("test cause")

	t.Run("creates context derived from background", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		defer cancel(nil, context.Background())

		require.NotNil(t, ctx)
		require.NoError(t, ctx.Err())
	})

	t.Run("cancel with cause and close context", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		closeCtx := context.Background()

		cancel(testCause, closeCtx)

		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, testCause, context.Cause(ctx))
	})

	t.Run("cancel with nil cause", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		closeCtx := context.Background()

		cancel(nil, closeCtx)

		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, context.Canceled, context.Cause(ctx))
	})

	t.Run("multiple cancel calls - first close context wins", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		firstCloseCtx, firstCancel := context.WithCancel(context.Background())
		defer firstCancel()
		secondCloseCtx, secondCancel := context.WithCancel(context.Background())
		defer secondCancel()

		firstCause := errors.New("first cause")
		secondCause := errors.New("second cause")

		cancel(firstCause, firstCloseCtx)
		cancel(secondCause, secondCloseCtx)

		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, firstCause, context.Cause(ctx))

		retrievedCloseCtx := GetCloseCtx(ctx)
		require.Equal(t, firstCloseCtx, retrievedCloseCtx)
	})

	t.Run("context cancellation propagates", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		closeCtx := context.Background()

		done := make(chan struct{})
		go func() {
			<-ctx.Done()
			close(done)
		}()

		cancel(testCause, closeCtx)

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatal("context cancellation did not propagate")
		}
	})
}

func TestGetCloseCtx(t *testing.T) {
	t.Parallel()

	t.Run("returns background for regular context", func(t *testing.T) {
		ctx := context.Background()
		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, ctx, closeCtx)
	})

	t.Run("returns background for context.WithCancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, context.Background(), closeCtx)
	})

	t.Run("returns background before cancel is called", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		defer cancel(nil, context.Background())

		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, context.Background(), closeCtx)
	})

	t.Run("returns close context after cancel is called", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		expectedCloseCtx := context.Background()

		cancel(nil, expectedCloseCtx)

		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, expectedCloseCtx, closeCtx)
	})

	t.Run("returns close context with values", func(t *testing.T) {
		type testKey struct{}
		ctx, cancel := BgWithCloseCtxCancelCause()
		expectedCloseCtx := context.WithValue(context.Background(), testKey{}, "test-value")

		cancel(nil, expectedCloseCtx)

		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, expectedCloseCtx, closeCtx)
		require.Equal(t, "test-value", closeCtx.Value(testKey{}))
	})

	t.Run("returns close context with deadline", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		expectedCloseCtx, expectedCancel := context.WithTimeout(context.Background(), time.Hour)
		defer expectedCancel()

		cancel(nil, expectedCloseCtx)

		closeCtx := GetCloseCtx(ctx)
		require.Equal(t, expectedCloseCtx, closeCtx)

		deadline, ok := closeCtx.Deadline()
		require.True(t, ok)
		require.True(t, deadline.After(time.Now()))
	})

	t.Run("close context can be used for cleanup operations", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()

		cancel(errors.New("shutdown"), closeCtx)

		// Simulate cleanup operation using close context
		retrievedCloseCtx := GetCloseCtx(ctx)
		require.NotNil(t, retrievedCloseCtx)

		cleanupDone := make(chan struct{})
		go func() {
			select {
			case <-retrievedCloseCtx.Done():
				// Cleanup interrupted
			case <-time.After(100 * time.Millisecond):
				// Cleanup completed
			}
			close(cleanupDone)
		}()

		select {
		case <-cleanupDone:
			// Success
		case <-time.After(time.Second):
			t.Fatal("cleanup operation did not complete")
		}
	})

	t.Run("concurrent access to close context", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()
		closeCtx := context.Background()

		// Start multiple goroutines trying to get the close context
		done := make(chan struct{})
		results := make(chan context.Context, 10)

		for i := 0; i < 10; i++ {
			go func() {
				<-done
				results <- GetCloseCtx(ctx)
			}()
		}

		// Cancel the context
		cancel(nil, closeCtx)

		// Signal goroutines to start
		close(done)

		// Collect results
		for i := 0; i < 10; i++ {
			result := <-results
			require.Equal(t, closeCtx, result)
		}
	})
}

func TestBgWithCloseCtxCancelCause_Integration(t *testing.T) {
	t.Parallel()

	t.Run("typical shutdown scenario", func(t *testing.T) {
		// Create main context
		ctx, cancel := BgWithCloseCtxCancelCause()

		// Simulate some work
		workStarted := make(chan struct{})
		workDone := make(chan struct{})

		go func() {
			close(workStarted)
			<-ctx.Done()
			// Work is canceled, now perform cleanup
			closeCtx := GetCloseCtx(ctx)
			require.NotNil(t, closeCtx)

			// Simulate cleanup with close context
			select {
			case <-closeCtx.Done():
				t.Error("close context was canceled prematurely")
			case <-time.After(50 * time.Millisecond):
				// Cleanup completed successfully
			}
			close(workDone)
		}()

		// Wait for work to start
		<-workStarted

		// Initiate shutdown with close context
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		cancel(errors.New("shutdown initiated"), closeCtx)

		// Wait for cleanup to complete
		select {
		case <-workDone:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup did not complete in time")
		}
	})

	t.Run("close context timeout during cleanup", func(t *testing.T) {
		ctx, cancel := BgWithCloseCtxCancelCause()

		// Create a close context with very short timeout
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer closeCancel()

		cancel(errors.New("shutdown"), closeCtx)

		// Retrieve close context and wait for it to timeout
		retrievedCloseCtx := GetCloseCtx(ctx)
		require.NotNil(t, retrievedCloseCtx)

		select {
		case <-retrievedCloseCtx.Done():
			require.ErrorIs(t, retrievedCloseCtx.Err(), context.DeadlineExceeded)
		case <-time.After(time.Second):
			t.Fatal("close context did not timeout as expected")
		}
	})
}
