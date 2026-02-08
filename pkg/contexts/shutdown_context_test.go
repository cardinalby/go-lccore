package contexts

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	internalctx "github.com/cardinalby/go-lccore/internal/contexts"
	"github.com/cardinalby/go-lccore/internal/signals"
	"github.com/stretchr/testify/require"
)

func TestShutdownContext_Signal(t *testing.T) {
	tests := []struct {
		name   string
		signal os.Signal
	}{
		{
			name:   "SIGINT",
			signal: syscall.SIGINT,
		},
		{
			name:   "SIGTERM",
			signal: syscall.SIGTERM,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Do not run signal tests in parallel
			// t.Parallel()
			ctx, cancel := NewShutdownContext(context.TODO())
			require.Equal(t, 1, signals.MockSubscribersCount())
			defer cancel(nil)

			go func() {
				// A small delay to allow the signal handler to be set up.
				time.Sleep(100 * time.Millisecond)
				signals.SendMockSignal(tt.signal)
			}()

			select {
			case <-ctx.Done():
				err := context.Cause(ctx)
				require.Error(t, err)
				var signalErr ErrSignalReceived
				require.ErrorAs(t, err, &signalErr)
				require.Equal(t, tt.signal, signalErr.Signal)
				require.Equal(t, tt.signal.String()+" signal received", err.Error())
				require.Equal(t, 0, signals.MockSubscribersCount())
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for signal")
			}
		})
	}
}

func TestShutdownContext_Cancel(t *testing.T) {
	ctx, cancel := NewShutdownContext(context.TODO())
	require.Equal(t, 1, signals.MockSubscribersCount())
	testCause := errors.New("test_cause")

	cancel(testCause)

	select {
	case <-ctx.Done():
		require.ErrorIs(t, context.Cause(ctx), testCause)
		require.Equal(t, 0, signals.MockSubscribersCount())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for context cancellation")
	}
}

func TestShutdownContext_WithTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		testCause := errors.New("test_cause")
		parentCtx, parentCtxCancel := context.WithTimeoutCause(context.TODO(), time.Second, testCause)
		defer parentCtxCancel()

		ctx, cancel := NewShutdownContext(parentCtx)
		defer cancel(nil)
		require.Equal(t, 1, signals.MockSubscribersCount())

		time.Sleep(500 * time.Millisecond)

		require.Equal(t, 1, signals.MockSubscribersCount())
		require.NoError(t, ctx.Err())

		time.Sleep(501 * time.Millisecond)

		select {
		case <-ctx.Done():
			require.ErrorIs(t, context.Cause(ctx), testCause)
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			require.Equal(t, 0, signals.MockSubscribersCount())
		default:
			t.Fatal("context is not done")
		}
	})
}

// TestGetCloseCtx verifies the public re-export of GetCloseCtx.
func TestGetCloseCtx(t *testing.T) {
	t.Run("returns background for uncanceled context", func(t *testing.T) {
		ctx, cancel := internalctx.BgWithCloseCtxCancelCause()
		defer cancel(nil, context.Background())

		result := GetCloseCtx(ctx)
		require.Equal(t, context.Background(), result)
	})

	t.Run("returns close context after cancel", func(t *testing.T) {
		ctx, cancel := internalctx.BgWithCloseCtxCancelCause()
		closeCtx, closeCancel := context.WithCancel(context.Background())
		defer closeCancel()

		cause := errors.New("shutdown")
		cancel(cause, closeCtx)

		result := GetCloseCtx(ctx)
		require.Equal(t, closeCtx, result)
	})

	t.Run("returns background for plain context", func(t *testing.T) {
		result := GetCloseCtx(context.Background())
		require.Equal(t, context.Background(), result)
	})
}
