package runtime

import (
	"context"
	"github.com/google/uuid"
)

type actorKey struct{}

// WithActor carries the authenticated/accepted submitter, never a model argument.
func WithActor(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, actorKey{}, id)
}

func Actor(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(actorKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
