package server

import (
	"context"
	"errors"
)

// ProductProjectionSubscriber fans out tool lifecycle events to the runtime,
// card, approval, evidence, incident, and orchestrator projections.
type ProductProjectionSubscriber struct {
	projections []ToolLifecycleSubscriber
}

// NewProductProjectionSubscriber builds the default projection fan-out chain.
func NewProductProjectionSubscriber(app *App) ProductProjectionSubscriber {
	return ProductProjectionSubscriber{
		projections: []ToolLifecycleSubscriber{
			NewRuntimeToolProjection(app),
			NewCardToolProjection(app),
			NewApprovalToolProjection(app),
			NewChoiceToolProjection(app),
			NewEvidenceToolProjection(app),
			NewIncidentToolProjection(app),
			NewOrchestratorToolProjection(app),
		},
	}
}

// HandleToolLifecycleEvent forwards the event to each configured projection in
// order and keeps delivering even if one projection fails.
func (p ProductProjectionSubscriber) HandleToolLifecycleEvent(ctx context.Context, event ToolLifecycleEvent) error {
	var errs []error
	for _, projection := range p.projections {
		if projection == nil {
			continue
		}
		if err := projection.HandleToolLifecycleEvent(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
