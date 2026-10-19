package conversation

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type delegationProjectKey struct{}

var ErrDelegationProjectChanged = errors.New("delegation project changed before acceptance")

// WithDelegationProject binds a server-prepared personal memory snapshot to its
// project. The shared immediate/queued acceptance transaction checks it under
// the conversation lock; moving a conversation cannot leak old-project facts.
func WithDelegationProject(ctx context.Context, projectID uuid.UUID) context.Context {
	return context.WithValue(ctx, delegationProjectKey{}, projectID)
}
