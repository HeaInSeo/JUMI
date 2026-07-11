package fixtures

import (
	"context"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type badRunner struct {
	adapter backend.Adapter
}

// ruleid: jumi-waitnode-must-check-succeeded
func (r *badRunner) waitAndFinalize(ctx context.Context, handle backend.Handle, attemptID string, startedAt time.Time, execNode spec.Node) error {
	result, err := r.adapter.WaitNode(ctx, handle)
	if err != nil {
		return err
	}
	_ = result
	_ = attemptID
	_ = startedAt
	_ = execNode
	return nil
}

type goodRunner struct {
	adapter backend.Adapter
}

// ok: jumi-waitnode-must-check-succeeded
func (r *goodRunner) waitAndFinalize(ctx context.Context, handle backend.Handle, attemptID string, startedAt time.Time, execNode spec.Node) error {
	result, err := r.adapter.WaitNode(ctx, handle)
	if err != nil {
		return err
	}
	if !result.Succeeded {
		return nil
	}
	_ = attemptID
	_ = startedAt
	_ = execNode
	return nil
}
