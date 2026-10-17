// Package flowctl puts the customer's business profile into PentAGI's own channels: the
// compact block at the start of every task's input, and the profile file among the flow's
// resources. It wraps the flow controller, so the controller and the prompts stay as they are.
// It is a package of its own because pkg/controller already depends on pkg/growth.
package flowctl

import (
	"context"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"
)

// Profiler is what growth.Service offers the wrapper.
type Profiler interface {
	WithProfile(ctx context.Context, userID int64, input string, res []database.UserResource) (string, []database.UserResource)
	WithProfileResource(ctx context.Context, userID int64, res []database.UserResource) []database.UserResource
}

type flowController struct {
	controller.FlowController
	svc Profiler
}

func Wrap(fc controller.FlowController, svc Profiler) controller.FlowController {
	return &flowController{FlowController: fc, svc: svc}
}

func (f *flowController) CreateFlow(
	ctx context.Context,
	userID int64,
	input string,
	prvname provider.ProviderName,
	prvtype provider.ProviderType,
	functions *tools.Functions,
	resources []database.UserResource,
) (int64, error) {
	input, resources = f.svc.WithProfile(ctx, userID, input, resources)
	return f.FlowController.CreateFlow(ctx, userID, input, prvname, prvtype, functions, resources)
}

// CreateAssistant only adds the file: an assistant reads the profile when it needs it,
// instead of carrying it in every chat message.
func (f *flowController) CreateAssistant(
	ctx context.Context,
	userID int64,
	flowID int64,
	input string,
	useAgents bool,
	prvname provider.ProviderName,
	prvtype provider.ProviderType,
	functions *tools.Functions,
	resources []database.UserResource,
) (int64, error) {
	resources = f.svc.WithProfileResource(ctx, userID, resources)
	return f.FlowController.CreateAssistant(ctx, userID, flowID, input, useAgents, prvname, prvtype, functions, resources)
}

func (f *flowController) GetFlow(ctx context.Context, flowID int64) (controller.FlowWorker, error) {
	fw, err := f.FlowController.GetFlow(ctx, flowID)
	if err != nil {
		return fw, err
	}
	return &flowWorker{FlowWorker: fw, svc: f.svc}, nil
}

// flowWorker gives every follow-up task the profile as it is saved now.
type flowWorker struct {
	controller.FlowWorker
	svc Profiler
}

func (w *flowWorker) PutInput(
	ctx context.Context,
	input string,
	prv provider.Provider,
	resources []database.UserResource,
) error {
	input, resources = w.svc.WithProfile(ctx, w.GetUserID(), input, resources)
	return w.FlowWorker.PutInput(ctx, input, prv, resources)
}
