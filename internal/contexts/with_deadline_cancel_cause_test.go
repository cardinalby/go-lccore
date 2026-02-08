package contexts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithDeadlineCancelCause(t *testing.T) {
	t.Parallel()
	deadlineCause := errors.New("deadline cause")

	t.Run("cancel with cause", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := WithDeadlineCancelCause(context.Background(), time.Now().Add(time.Hour), deadlineCause)
		err := errors.New("test error")
		cancel(err)

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), err)
			require.NotErrorIs(t, context.Cause(ctx), deadlineCause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})

	t.Run("cancel with deadline cause", func(t *testing.T) {
		t.Parallel()
		ctx, _ := WithDeadlineCancelCause(context.Background(), time.Now().Add(100*time.Millisecond), deadlineCause)

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), deadlineCause)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for context cancellation")
		}
	})
}
