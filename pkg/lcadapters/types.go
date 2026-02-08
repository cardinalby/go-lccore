package lcadapters

import "context"

type Adapter interface {
	OnStart() func(ctx context.Context) error
	OnWait() func() error
	OnClose() func(ctx context.Context) error
}
