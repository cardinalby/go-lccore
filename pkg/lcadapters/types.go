package lcadapters

import "context"

// Adapter defines the interface that describes lifecycle behavior compatible with lccore.ComponentProps format
type Adapter interface {
	OnStart() func(ctx context.Context) error
	OnWait() func() error
	OnClose() func(ctx context.Context) error
}
