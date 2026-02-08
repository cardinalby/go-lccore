package lccore

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

const hookCtrlTimeout = 2 * time.Second
const hookCtrlTryTimeout = 100 * time.Millisecond

// monotonic counter for ordering assertions
var hookCtrlSeqCounter atomic.Int64

type hookCtrl struct {
	// Closed when the hook goroutine has entered (before blocking).
	enteredCh chan struct{}

	// The hook goroutine blocks reading from this channel.
	// Test sends nil (success) or an error (failure).
	resultCh chan error

	// Closed when the hook goroutine has exited (after result received).
	exitedCh chan struct{}

	// Monotonic sequence number assigned when hook enters.
	enteredSeq atomic.Int64

	// The error that was sent via Succeed/Fail, stored for consistency checks.
	// nil pointer = not yet sent; non-nil pointer = sent (value may be nil for success).
	sentResult atomic.Pointer[error]

	// The context received by the hook, stored atomically when the hook enters.
	// nil = hook not yet entered or hook does not receive a context (e.g. waitFunc).
	receivedCtx atomic.Pointer[context.Context]
}

func newHookCtrl() *hookCtrl {
	return &hookCtrl{
		enteredCh: make(chan struct{}),
		resultCh:  make(chan error, 1),
		exitedCh:  make(chan struct{}),
	}
}

// Succeed unblocks the hook with a nil error.
func (c *hookCtrl) Succeed() {
	var nilErr error
	c.sentResult.Store(&nilErr)
	c.resultCh <- nil
}

// Fail unblocks the hook with the given error.
func (c *hookCtrl) Fail(err error) {
	c.sentResult.Store(&err)
	c.resultCh <- err
}

// WaitEntered blocks the test goroutine until the hook has been entered.
func (c *hookCtrl) WaitEntered(t testing.TB) {
	t.Helper()
	select {
	case <-c.enteredCh:
	case <-time.After(hookCtrlTimeout):
		t.Fatal("timeout waiting for hook to be entered")
	}
}

// WaitExited blocks the test goroutine until the hook has returned.
func (c *hookCtrl) WaitExited(t testing.TB) {
	t.Helper()
	select {
	case <-c.exitedCh:
	case <-time.After(hookCtrlTimeout):
		t.Fatal("timeout waiting for hook to exit")
	}
}

func (c *hookCtrl) AssertWaitNotEntered(t testing.TB) {
	select {
	case <-c.enteredCh:
		t.Fatal("expected hook not to be entered, but it was")
	case <-time.After(hookCtrlTryTimeout):
	}
}

// HasEntered returns true if the hook was entered (non-blocking).
func (c *hookCtrl) HasEntered() bool {
	select {
	case <-c.enteredCh:
		return true
	default:
		return false
	}
}

// HasExited returns true if the hook has exited.
func (c *hookCtrl) HasExited() bool {
	select {
	case <-c.exitedCh:
		return true
	default:
		return false
	}
}

// SentNilResult returns true if Succeed() was called (sent nil error).
func (c *hookCtrl) SentNilResult() bool {
	p := c.sentResult.Load()
	return p != nil && *p == nil
}

func (c *hookCtrl) enterBody() {
	close(c.enteredCh)
	c.enteredSeq.Store(hookCtrlSeqCounter.Add(1))
}

func (c *hookCtrl) enterBodyWithCtx(ctx context.Context) {
	c.receivedCtx.Store(&ctx)
	c.enterBody()
}

func (c *hookCtrl) exitBody() {
	close(c.exitedCh)
}

// ReceivedCtx returns the context that was passed to the hook when it entered.
// Returns nil if the hook has not yet been entered or has no context (e.g. waitFunc).
func (c *hookCtrl) ReceivedCtx() context.Context {
	p := c.receivedCtx.Load()
	if p == nil {
		return nil
	}
	return *p
}

func (c *hookCtrl) startFunc() func(ctx context.Context) error {
	return func(ctx context.Context) error {
		c.enterBodyWithCtx(ctx)
		err := <-c.resultCh
		c.exitBody()
		return err
	}
}

func (c *hookCtrl) waitFunc() func() error {
	return func() error {
		c.enterBody()
		err := <-c.resultCh
		c.exitBody()
		return err
	}
}

func (c *hookCtrl) closeFunc() func(ctx context.Context) error {
	return func(ctx context.Context) error {
		c.enterBodyWithCtx(ctx)
		err := <-c.resultCh
		c.exitBody()
		return err
	}
}
