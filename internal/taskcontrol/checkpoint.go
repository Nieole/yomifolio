package taskcontrol

import "context"

type checkpointKey struct{}

// WithCheckpoint adds a cooperative execution fence. The callback must not hold
// resources needed by the work it guards and must be safe for concurrent workers.
func WithCheckpoint(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, checkpointKey{}, check)
}

func checkpoint(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if check, ok := ctx.Value(checkpointKey{}).(func(context.Context) error); ok {
		return check(ctx)
	}
	return nil
}
