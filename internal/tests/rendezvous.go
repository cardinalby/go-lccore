package tests

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

const defaultRendezvousTimeout = time.Second

// Rendezvous is a synchronization primitive for tests that allows checking that all members are
// executing concurrently at a certain point in time
type Rendezvous[T comparable] struct {
	timeout         time.Duration
	mu              sync.Mutex
	awaitingMembers map[T]struct{}
	arrivedMembers  map[T]struct{}
	ready           chan struct{}
}

func NewRendezvous[T comparable](members ...T) *Rendezvous[T] {
	awaitingMembers := make(map[T]struct{}, len(members))
	for _, m := range members {
		if _, exists := awaitingMembers[m]; exists {
			panic(fmt.Sprintf("duplicate member in rendezvous: %v", m))
		}
		awaitingMembers[m] = struct{}{}
	}
	return &Rendezvous[T]{
		timeout:         defaultRendezvousTimeout,
		awaitingMembers: awaitingMembers,
		arrivedMembers:  make(map[T]struct{}),
		ready:           make(chan struct{}),
	}
}

func (r *Rendezvous[T]) WithTimeout(timeout time.Duration) *Rendezvous[T] {
	r.timeout = timeout
	return r
}

func (r *Rendezvous[T]) ArriveAndWait(t testing.TB, member T) {
	t.Helper()
	r.mu.Lock()
	if _, exists := r.arrivedMembers[member]; exists {
		t.Fatalf("member %v already arrived at rendezvous", member)
	}
	r.arrivedMembers[member] = struct{}{}

	if _, exists := r.awaitingMembers[member]; !exists {
		t.Fatalf("unexpected member %v arrived at rendezvous", member)
	}
	delete(r.awaitingMembers, member)
	if len(r.awaitingMembers) == 0 {
		close(r.ready)
	}
	r.mu.Unlock()

	select {
	case <-r.ready:
	case <-time.After(r.timeout):
		r.mu.Lock()
		missingMembers := make([]T, 0, len(r.awaitingMembers))
		for m := range r.awaitingMembers {
			missingMembers = append(missingMembers, m)
		}
		r.mu.Unlock()
		t.Fatalf("timeout waiting for members to arrive at rendezvous: %v", missingMembers)
	}
}
