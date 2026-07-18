package orchestration

import (
	"context"
	"sync"

	"github.com/callumny/kingdom/internal/tools"
)

// ApprovalRequest is the single-use hand-off between orchestration and the UI.
// Closing done makes the decision safe to observe from either goroutine.
type ApprovalRequest struct {
	approval tools.Approval
	done     chan struct{}
	once     sync.Once
	approved bool
}

func NewApprovalRequest(approval tools.Approval) *ApprovalRequest {
	return &ApprovalRequest{approval: approval, done: make(chan struct{})}
}

func (r *ApprovalRequest) Approval() tools.Approval { return r.approval }

func (r *ApprovalRequest) Resolve(approved bool) bool {
	resolved := false
	r.once.Do(func() {
		r.approved = approved
		resolved = true
		close(r.done)
	})
	return resolved
}

func (r *ApprovalRequest) Wait(ctx context.Context) (bool, error) {
	select {
	case <-r.done:
		return r.approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
