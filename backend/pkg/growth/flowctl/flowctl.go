// Package flowctl puts the customer's business profile into PentAGI's own channels: the
// compact block at the start of every task's input, and the profile file among the flow's
// resources. It wraps the flow controller, so the controller and the prompts stay as they are.
// It is a package of its own because pkg/controller already depends on pkg/growth.
package flowctl

import (
	"context"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/sirupsen/logrus"
)

// Profiler is what growth.Service offers the wrapper.
type Profiler interface {
	ResolveProfile(ctx context.Context, userID int64, selected *int64) (growth.ProfileView, error)
	ProfileForFlow(ctx context.Context, userID, flowID int64) (growth.ProfileView, error)
	BindFlowProfile(ctx context.Context, flowID int64, v growth.ProfileView) error
	ProfileInput(ctx context.Context, userID int64, v growth.ProfileView, input string, res []database.UserResource) (string, []database.UserResource)
	ProfileResources(ctx context.Context, userID int64, v growth.ProfileView, res []database.UserResource) []database.UserResource
	FlowOfAssistant(ctx context.Context, assistantID int64) (int64, error)
}

type flowController struct {
	controller.FlowController
	svc Profiler
}

func Wrap(fc controller.FlowController, svc Profiler) controller.FlowController {
	return &flowController{FlowController: fc, svc: svc}
}

// newFlowProfile is the profile a new flow starts with: the one the client chose with
// growth.WithSelectedProfile, else the default. A failure to load the default starts the
// flow without a profile, as a passing database error should not refuse the task; a
// chosen profile that is not the owner's refuses it.
func (f *flowController) newFlowProfile(ctx context.Context, userID int64) (growth.ProfileView, bool, error) {
	var selected *int64
	if id, ok := growth.SelectedProfile(ctx); ok {
		selected = &id
	}
	v, err := f.svc.ResolveProfile(ctx, userID, selected)
	switch {
	case err == nil:
		return v, true, nil
	case selected != nil:
		return growth.ProfileView{}, false, err
	default:
		logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("task started without the business profile")
		return growth.ProfileView{}, false, nil
	}
}

func (f *flowController) bind(ctx context.Context, flowID int64, v growth.ProfileView) {
	if err := f.svc.BindFlowProfile(ctx, flowID, v); err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("flow_id", flowID).Warn("flow falls back to the default business profile")
	}
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
	v, ok, err := f.newFlowProfile(ctx, userID)
	if err != nil {
		return 0, err
	}
	if ok {
		input, resources = f.svc.ProfileInput(ctx, userID, v, input, resources)
	}
	flowID, err := f.FlowController.CreateFlow(ctx, userID, input, prvname, prvtype, functions, resources)
	if err == nil && ok {
		f.bind(ctx, flowID, v)
	}
	return flowID, err
}

// CreateAssistant only adds the file: an assistant reads the profile when it needs it,
// instead of carrying it in every chat message. A chosen profile applies to the flow the
// assistant starts; an existing flow keeps its own.
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
	var (
		v   growth.ProfileView
		ok  bool
		err error
	)
	if flowID == 0 {
		v, ok, err = f.newFlowProfile(ctx, userID)
	} else if v, err = f.svc.ProfileForFlow(ctx, userID, flowID); err == nil {
		ok = true
	} else {
		logrus.WithContext(ctx).WithError(err).WithField("flow_id", flowID).Warn("assistant started without the business profile")
		err = nil
	}
	if err != nil {
		return 0, err
	}
	if ok {
		resources = f.svc.ProfileResources(ctx, userID, v, resources)
	}
	assistantID, err := f.FlowController.CreateAssistant(ctx, userID, flowID, input, useAgents, prvname, prvtype, functions, resources)
	if err != nil || !ok || flowID != 0 {
		return assistantID, err
	}
	if newFlowID, err := f.svc.FlowOfAssistant(ctx, assistantID); err == nil {
		f.bind(ctx, newFlowID, v)
	} else {
		logrus.WithContext(ctx).WithError(err).WithField("assistant_id", assistantID).Warn("flow falls back to the default business profile")
	}
	return assistantID, nil
}

func (f *flowController) GetFlow(ctx context.Context, flowID int64) (controller.FlowWorker, error) {
	fw, err := f.FlowController.GetFlow(ctx, flowID)
	if err != nil || fw == nil {
		return fw, err
	}
	return &flowWorker{FlowWorker: fw, svc: f.svc}, nil
}

// flowWorker gives every follow-up task the flow's profile as it is saved now.
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
	v, err := w.svc.ProfileForFlow(ctx, w.GetUserID(), w.GetFlowID())
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("flow_id", w.GetFlowID()).Warn("task started without the business profile")
		return w.FlowWorker.PutInput(ctx, input, prv, resources)
	}
	input, resources = w.svc.ProfileInput(ctx, w.GetUserID(), v, input, resources)
	return w.FlowWorker.PutInput(ctx, input, prv, resources)
}
