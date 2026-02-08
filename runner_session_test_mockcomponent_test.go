package lccore

import (
	"context"
	"fmt"
)

type mockComponent struct {
	name      testComponent
	startCtrl *hookCtrl
	waitCtrl  *hookCtrl
	closeCtrl *hookCtrl
	deps      []testLink
}

func (m *mockComponent) OnStart() func(ctx context.Context) error {
	if m.startCtrl == nil {
		return nil
	}
	return m.startCtrl.startFunc()
}

func (m *mockComponent) OnWait() func() error {
	if m.waitCtrl == nil {
		return nil
	}
	return m.waitCtrl.waitFunc()
}

func (m *mockComponent) OnClose() func(ctx context.Context) error {
	if m.closeCtrl == nil {
		return nil
	}
	return m.closeCtrl.closeFunc()
}

func (m *mockComponent) OwnConstraints() ComponentOwnConstraints {
	return ComponentOwnConstraints{}
}

func (m *mockComponent) DependsOn() []testLink {
	return m.deps
}

// Start returns the hookCtrl for OnStart. Panics if the component has no OnStart hook.
func (m *mockComponent) Start() *hookCtrl {
	if m.startCtrl == nil {
		panic(fmt.Sprintf("component %q has no OnStart hook", m.name))
	}
	return m.startCtrl
}

// Wait returns the hookCtrl for OnWait. Panics if the component has no OnWait hook.
func (m *mockComponent) Wait() *hookCtrl {
	if m.waitCtrl == nil {
		panic(fmt.Sprintf("component %q has no OnWait hook", m.name))
	}
	return m.waitCtrl
}

// Close returns the hookCtrl for OnClose. Panics if the component has no OnClose hook.
func (m *mockComponent) Close() *hookCtrl {
	if m.closeCtrl == nil {
		panic(fmt.Sprintf("component %q has no OnClose hook", m.name))
	}
	return m.closeCtrl
}
