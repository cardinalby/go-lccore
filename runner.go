package lccore

import (
	"context"
	"errors"
	"sync/atomic"
)

type runner[C comparable] struct {
	graph     Graph[C]
	options   RunnerOptions[C]
	isRunning atomic.Bool
}

var ErrAlreadyRunning = errors.New("is already running")

// NewRunner creates a new Runner for the given graph and options.
func NewRunner[C comparable](graph Graph[C], options RunnerOptions[C]) Runner[C] {
	return &runner[C]{
		graph:   graph,
		options: options,
	}
}

func (r *runner[C]) Run(ctx context.Context, onReady func()) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.isRunning.Swap(true) {
		return ErrAlreadyRunning
	}
	defer func() {
		r.isRunning.Store(false)
	}()
	rs := newRunnerSession(r.graph.self(), r.options, onReady)
	return rs.run(ctx)
}
