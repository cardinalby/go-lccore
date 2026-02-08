package contexts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithDeferredCancelCause(t *testing.T) {
	t.Parallel()

	t.Run("cancels context after duration with given cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("test cause")
		ctx, cancelAfter, _ := WithDeferredCancelCause(context.Background())

		cancelAfter(50*time.Millisecond, cause)

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), cause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("first call wins, second call ignored", func(t *testing.T) {
		t.Parallel()
		firstCause := errors.New("first cause")
		secondCause := errors.New("second cause")
		ctx, cancelAfter, _ := WithDeferredCancelCause(context.Background())

		cancelAfter(50*time.Millisecond, firstCause)
		cancelAfter(10*time.Millisecond, secondCause) // shorter duration but should be ignored

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), firstCause)
			require.NotErrorIs(t, context.Cause(ctx), secondCause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("parent cancellation cancels child context", func(t *testing.T) {
		t.Parallel()
		parentCtx, parentCancel := context.WithCancel(context.Background())
		ctx, _, _ := WithDeferredCancelCause(parentCtx)

		parentCancel()

		select {
		case <-ctx.Done():
			require.ErrorIs(t, ctx.Err(), context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("parent canceled before timer fires stops goroutine without cancel", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("deferred cause")
		parentCtx, parentCancel := context.WithCancel(context.Background())
		ctx, cancelAfter, _ := WithDeferredCancelCause(parentCtx)

		// Schedule a long-duration cancel
		cancelAfter(time.Hour, cause)

		// Cancel parent immediately
		parentCancel()

		select {
		case <-ctx.Done():
			// Cause is context.Canceled when parent is canceled without a cause
			require.ErrorIs(t, context.Cause(ctx), context.Canceled)
			require.NotErrorIs(t, context.Cause(ctx), cause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("cancelAnyway cancels immediately without cancelAfter", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("anyway cause")
		ctx, _, cancelAnyway := WithDeferredCancelCause(context.Background())

		cancelAnyway(cause)

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), cause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("cancelAnyway stops pending timer and cancels immediately", func(t *testing.T) {
		t.Parallel()
		timerCause := errors.New("timer cause")
		anywayCause := errors.New("anyway cause")
		ctx, cancelAfter, cancelAnyway := WithDeferredCancelCause(context.Background())

		// Arm a long timer
		cancelAfter(time.Hour, timerCause)

		// cancelAnyway should cancel immediately, stopping the timer goroutine
		cancelAnyway(anywayCause)

		select {
		case <-ctx.Done():
			// First cancel wins: cancelAnyway's cause is set only if cancelAfter's timer
			// hasn't fired yet (it hasn't — it's an hour). context.WithCancelCause keeps
			// the first cause, so anywayCause wins.
			require.ErrorIs(t, context.Cause(ctx), anywayCause)
			require.NotErrorIs(t, context.Cause(ctx), timerCause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("cancelAnyway after cancelAfter already fired keeps original cause", func(t *testing.T) {
		t.Parallel()
		timerCause := errors.New("timer cause")
		anywayCause := errors.New("anyway cause")
		ctx, cancelAfter, cancelAnyway := WithDeferredCancelCause(context.Background())

		// Cancel immediately via cancelAfter
		cancelAfter(0, timerCause)

		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}

		// cancelAnyway after context is already canceled — cause should not change
		cancelAnyway(anywayCause)
		require.ErrorIs(t, context.Cause(ctx), timerCause)
		require.NotErrorIs(t, context.Cause(ctx), anywayCause)
	})

	t.Run("context not canceled before duration elapses", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("test cause")
		ctx, cancelAfter, _ := WithDeferredCancelCause(context.Background())

		cancelAfter(200*time.Millisecond, cause)

		select {
		case <-ctx.Done():
			t.Fatal("context should not be canceled yet")
		case <-time.After(50 * time.Millisecond):
			// context still alive — correct
		}

		// Now wait for it to actually cancel
		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), cause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})
}
