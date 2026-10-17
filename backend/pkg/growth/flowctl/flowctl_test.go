package flowctl

import (
	"context"
	"testing"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var profileFile = database.UserResource{ID: 77, Path: "business_profile.md"}

// stampProfiler marks the input of user 7 and adds the profile file, like a saved profile would.
type stampProfiler struct{}

func (stampProfiler) WithProfile(
	_ context.Context, userID int64, input string, res []database.UserResource,
) (string, []database.UserResource) {
	if userID != 7 {
		return input, res
	}
	return "<business_profile>\n</business_profile>\n\n" + input, append(res, profileFile)
}

func (stampProfiler) WithProfileResource(_ context.Context, userID int64, res []database.UserResource) []database.UserResource {
	if userID != 7 {
		return res
	}
	return append(res, profileFile)
}

type recordedCall struct {
	input     string
	resources []database.UserResource
}

// recordingController records what reaches the real controller.
type recordingController struct {
	controller.FlowController
	flows      []recordedCall
	assistants []recordedCall
	worker     *recordingWorker
}

func (r *recordingController) CreateFlow(
	ctx context.Context, _ int64, input string, _ provider.ProviderName, _ provider.ProviderType,
	_ *tools.Functions, res []database.UserResource,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.flows = append(r.flows, recordedCall{input, res})
	return 1, nil
}

func (r *recordingController) CreateAssistant(
	ctx context.Context, _, _ int64, input string, _ bool, _ provider.ProviderName, _ provider.ProviderType,
	_ *tools.Functions, res []database.UserResource,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.assistants = append(r.assistants, recordedCall{input, res})
	return 2, nil
}

func (r *recordingController) GetFlow(context.Context, int64) (controller.FlowWorker, error) {
	return r.worker, nil
}

type recordingWorker struct {
	controller.FlowWorker
	userID int64
	inputs []recordedCall
}

func (w *recordingWorker) GetUserID() int64 { return w.userID }

func (w *recordingWorker) PutInput(ctx context.Context, input string, _ provider.Provider, res []database.UserResource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.inputs = append(w.inputs, recordedCall{input, res})
	return nil
}

func TestFlowctl_CreateFlow_StartsTheFirstTaskWithTheProfile(t *testing.T) {
	inner := &recordingController{}
	fc := Wrap(inner, stampProfiler{})

	_, err := fc.CreateFlow(context.Background(), 7, "find 50 players", "", "", nil, nil)
	require.NoError(t, err)
	_, err = fc.CreateFlow(context.Background(), 8, "find 50 players", "", "", nil, nil)
	require.NoError(t, err)

	require.Len(t, inner.flows, 2)
	assert.Equal(t, "<business_profile>\n</business_profile>\n\nfind 50 players", inner.flows[0].input)
	assert.Equal(t, []database.UserResource{profileFile}, inner.flows[0].resources)
	assert.Equal(t, "find 50 players", inner.flows[1].input, "a customer without a profile is untouched")
	assert.Empty(t, inner.flows[1].resources)
}

func TestFlowctl_GetFlow_GivesEveryFollowUpTaskTheProfile(t *testing.T) {
	worker := &recordingWorker{userID: 7}
	fc := Wrap(&recordingController{worker: worker}, stampProfiler{})

	fw, err := fc.GetFlow(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, fw.PutInput(context.Background(), "20 more please", nil, nil))

	require.Len(t, worker.inputs, 1)
	assert.Equal(t, "<business_profile>\n</business_profile>\n\n20 more please", worker.inputs[0].input)
	assert.Equal(t, []database.UserResource{profileFile}, worker.inputs[0].resources)
}

func TestFlowctl_CreateAssistant_AttachesOnlyTheProfileFile(t *testing.T) {
	inner := &recordingController{}
	fc := Wrap(inner, stampProfiler{})

	_, err := fc.CreateAssistant(context.Background(), 7, 1, "which groups did we read?", false, "", "", nil, nil)
	require.NoError(t, err)

	require.Len(t, inner.assistants, 1)
	assert.Equal(t, "which groups did we read?", inner.assistants[0].input)
	assert.Equal(t, []database.UserResource{profileFile}, inner.assistants[0].resources)
}
