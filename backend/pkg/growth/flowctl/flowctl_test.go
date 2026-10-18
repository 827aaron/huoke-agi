package flowctl

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stampProfiler gives user 7 profile 3 by default and any profile below 10 by choice; it
// fails to load anything for other users, and binds flow 1 to profile 4.
type stampProfiler struct {
	bound map[int64]int64
}

func newStampProfiler() *stampProfiler { return &stampProfiler{bound: map[int64]int64{}} }

func (p *stampProfiler) ResolveProfile(ctx context.Context, userID int64, selected *int64) (growth.ProfileView, error) {
	if err := ctx.Err(); err != nil {
		return growth.ProfileView{}, err
	}
	switch {
	case userID != 7:
		return growth.ProfileView{}, errors.New("database is down")
	case selected == nil:
		return growth.ProfileView{ID: 3}, nil
	case *selected < 10:
		return growth.ProfileView{ID: *selected}, nil
	default:
		return growth.ProfileView{}, growth.ErrNoProfile
	}
}

func (p *stampProfiler) ProfileForFlow(ctx context.Context, userID, flowID int64) (growth.ProfileView, error) {
	if id, ok := p.bound[flowID]; ok {
		return growth.ProfileView{ID: id}, nil
	}
	return p.ResolveProfile(ctx, userID, nil)
}

func (p *stampProfiler) BindFlowProfile(ctx context.Context, flowID int64, v growth.ProfileView) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.bound[flowID] = v.ID
	return nil
}

func (p *stampProfiler) ProfileInput(
	_ context.Context, _ int64, v growth.ProfileView, input string, res []database.UserResource,
) (string, []database.UserResource) {
	return fmt.Sprintf("<business_profile>%d</business_profile>\n\n%s", v.ID, input), append(res, profileFile(v.ID))
}

func (p *stampProfiler) ProfileResources(_ context.Context, _ int64, v growth.ProfileView, res []database.UserResource) []database.UserResource {
	return append(res, profileFile(v.ID))
}

func (p *stampProfiler) FlowOfAssistant(ctx context.Context, assistantID int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return assistantID + 100, nil
}

func profileFile(id int64) database.UserResource {
	return database.UserResource{Path: fmt.Sprintf("business_profile_%d.md", id)}
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
	flowID int64
	inputs []recordedCall
}

func (w *recordingWorker) GetUserID() int64 { return w.userID }

func (w *recordingWorker) GetFlowID() int64 { return w.flowID }

func (w *recordingWorker) PutInput(ctx context.Context, input string, _ provider.Provider, res []database.UserResource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.inputs = append(w.inputs, recordedCall{input, res})
	return nil
}

func TestFlowctl_CreateFlow_StartsTheFirstTaskWithTheProfile(t *testing.T) {
	tests := []struct {
		name      string
		userID    int64
		selected  *int64
		wantInput string
		wantFiles []database.UserResource
		wantBound map[int64]int64
	}{
		{
			name: "the default without a choice", userID: 7,
			wantInput: "<business_profile>3</business_profile>\n\nfind 50 players",
			wantFiles: []database.UserResource{profileFile(3)}, wantBound: map[int64]int64{1: 3},
		},
		{
			name: "the chosen profile", userID: 7, selected: ptr(int64(5)),
			wantInput: "<business_profile>5</business_profile>\n\nfind 50 players",
			wantFiles: []database.UserResource{profileFile(5)}, wantBound: map[int64]int64{1: 5},
		},
		{
			name: "untouched when the profile cannot be loaded", userID: 8,
			wantInput: "find 50 players", wantBound: map[int64]int64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner, prof := &recordingController{}, newStampProfiler()
			ctx := context.Background()
			if tt.selected != nil {
				ctx = growth.WithSelectedProfile(ctx, *tt.selected)
			}

			_, err := Wrap(inner, prof).CreateFlow(ctx, tt.userID, "find 50 players", "", "", nil, nil)
			require.NoError(t, err)

			require.Len(t, inner.flows, 1)
			assert.Equal(t, tt.wantInput, inner.flows[0].input)
			assert.Equal(t, tt.wantFiles, inner.flows[0].resources)
			assert.Equal(t, tt.wantBound, prof.bound)
		})
	}
}

func TestFlowctl_CreateFlow_RefusesAProfileThatIsNotTheOwners(t *testing.T) {
	inner := &recordingController{}

	ctx := growth.WithSelectedProfile(context.Background(), 42)
	_, err := Wrap(inner, newStampProfiler()).CreateFlow(ctx, 7, "find 50 players", "", "", nil, nil)

	require.ErrorIs(t, err, growth.ErrNoProfile)
	assert.Empty(t, inner.flows)
}

func TestFlowctl_GetFlow_GivesEveryFollowUpTaskTheFlowsProfile(t *testing.T) {
	worker := &recordingWorker{userID: 7, flowID: 1}
	prof := newStampProfiler()
	prof.bound[1] = 4

	fw, err := Wrap(&recordingController{worker: worker}, prof).GetFlow(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, fw.PutInput(context.Background(), "20 more please", nil, nil))

	require.Len(t, worker.inputs, 1)
	assert.Equal(t, "<business_profile>4</business_profile>\n\n20 more please", worker.inputs[0].input)
	assert.Equal(t, []database.UserResource{profileFile(4)}, worker.inputs[0].resources)
}

func TestFlowctl_CreateAssistant_AttachesOnlyTheProfileFile(t *testing.T) {
	inner, prof := &recordingController{}, newStampProfiler()
	prof.bound[1] = 4
	fc := Wrap(inner, prof)

	_, err := fc.CreateAssistant(context.Background(), 7, 1, "which groups did we read?", false, "", "", nil, nil)
	require.NoError(t, err)
	_, err = fc.CreateAssistant(growth.WithSelectedProfile(context.Background(), 6), 7, 0, "draft a profile", false, "", "", nil, nil)
	require.NoError(t, err)

	require.Len(t, inner.assistants, 2)
	assert.Equal(t, "which groups did we read?", inner.assistants[0].input)
	assert.Equal(t, []database.UserResource{profileFile(4)}, inner.assistants[0].resources, "an existing flow keeps its profile")
	assert.Equal(t, []database.UserResource{profileFile(6)}, inner.assistants[1].resources)
	assert.Equal(t, map[int64]int64{1: 4, 102: 6}, prof.bound, "the flow the assistant started takes the chosen profile")
}

func ptr[T any](v T) *T { return &v }
