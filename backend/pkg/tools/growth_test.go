package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pentagi/pkg/growth"
	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrowth_Handle_AppendsTheProgressLineInsideATask(t *testing.T) {
	task := int64(3)
	backend := &fakeGrowthBackend{progress: growth.Progress{Target: 50, NewLeads: 12}}
	tools := newGrowthTools(backend, 1, 42, &task)

	out, err := tools.Handle(t.Context(), GrowthUpsertLeadsToolName, json.RawMessage(`{"leads":[{
		"platform_user_id": 777, "score": "9", "tier": "Qualified", "match_reason": "player+3",
		"evidence": [{"source_id": "5", "message_id": 12, "excerpt": "any game like this?", "message_time": "2026-10-15 08:30"}]
	}]}`))
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(out, "\nPROGRESS: new_leads_this_task=12 target=50"), out)

	require.Len(t, backend.upserted, 1)
	in := backend.upserted[0]
	assert.Equal(t, "777", in.PlatformUserID)
	assert.Equal(t, 9, in.Score)
	assert.Equal(t, "qualified", in.Tier)
	require.Len(t, in.Evidence, 1)
	assert.Equal(t, int64(5), in.Evidence[0].SourceID)
	assert.Equal(t, "12", in.Evidence[0].MessageID)
	require.NotNil(t, in.Evidence[0].MessageTime)
	assert.Equal(t, time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC), *in.Evidence[0].MessageTime)
	assert.Equal(t, growth.Scope{UserID: 1, FlowID: 42, TaskID: &task}, backend.lastScope)
}

func TestGrowth_Handle_OmitsTheProgressLineOutsideATask(t *testing.T) {
	backend := &fakeGrowthBackend{progress: growth.Progress{Target: 50}}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthSearchSourcesToolName,
		json.RawMessage(`{"query":"rpg"}`))
	require.NoError(t, err)
	assert.NotContains(t, out, "PROGRESS:")
}

func TestGrowth_Handle_MarksLinksFoundInASourceAsDiscovered(t *testing.T) {
	backend := &fakeGrowthBackend{}
	tools := newGrowthTools(backend, 1, 42, nil)

	_, err := tools.Handle(t.Context(), GrowthAddSourcesToolName, json.RawMessage(`{"links":["t.me/a_group"],"discovered_from_source_id":"9"}`))
	require.NoError(t, err)
	assert.Equal(t, growth.OriginAI, backend.lastOrigin)
	require.NotNil(t, backend.lastFrom)
	assert.Equal(t, int64(9), *backend.lastFrom)

	_, err = tools.Handle(t.Context(), GrowthAddSourcesToolName, json.RawMessage(`{"links":["t.me/a_group"]}`))
	require.NoError(t, err)
	assert.Equal(t, growth.OriginManual, backend.lastOrigin)
	assert.Nil(t, backend.lastFrom)
}

func TestGrowth_Handle_DeliverPassesAllowPartial(t *testing.T) {
	task := int64(3)
	backend := &fakeGrowthBackend{delivery: growth.Delivery{
		Status: growth.DeliveryIncomplete, Gap: 8, Progress: growth.Progress{Target: 50, NewLeads: 42},
	}}
	out, err := newGrowthTools(backend, 1, 42, &task).Handle(t.Context(), GrowthDeliverLeadsToolName,
		json.RawMessage(`{"allow_partial":"true"}`))
	require.NoError(t, err)
	assert.True(t, backend.lastAllowPartial)
	assert.Contains(t, out, `"status":"incomplete"`)
	assert.True(t, strings.HasSuffix(out, "PROGRESS: new_leads_this_task=42 target=50"))
}

func TestGrowth_Handle_TurnsDomainErrorsIntoInstructions(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&connectors.FloodWaitError{Wait: 90 * time.Second}, "wait 1m30s"},
		{growth.ErrBrowserSource, "Open the page with the browser tool"},
		{growth.ErrNoAccount, "an operator has to log the account in"},
		{connectors.ErrUnauthorized, "an operator has to log the account in"},
		{connectors.ErrNoConnector, "not available in this deployment"},
		{growth.ErrSourceNotFound, "Use a source_id returned by"},
		{growth.ErrNoJudge, "fill in its key on the settings page. Retrying will not help"},
		{errors.New("disk on fire"), "the growth tool failed: disk on fire"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			out, err := newGrowthTools(&fakeGrowthBackend{err: tt.err}, 1, 42, nil).Handle(t.Context(),
				GrowthSyncSourceToolName, json.RawMessage(`{"source_id":5}`))
			require.NoError(t, err, "a domain error is a result, not a retry")
			assert.Contains(t, out, tt.want)
		})
	}
}

func TestGrowth_Handle_SyncWithoutIDsAsksForEveryJoinedSource(t *testing.T) {
	backend := &fakeGrowthBackend{}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthSyncSourceToolName, json.RawMessage(`{"action":"history"}`))
	require.NoError(t, err)
	assert.Empty(t, backend.lastSyncIDs)
	assert.JSONEq(t, `{"results":[]}`, out)

	out, err = newGrowthTools(&fakeGrowthBackend{err: growth.ErrNoJoinedSource}, 1, 42, nil).
		Handle(t.Context(), GrowthSyncSourceToolName, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "no source in the pool is joined yet. Join sources with growth_join_source first, or pass source_ids.", out)
}

func TestGrowth_Handle_ReturnsTheErrorOfACancelledCall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := newGrowthTools(&fakeGrowthBackend{err: context.Canceled}, 1, 42, nil).Handle(ctx,
		GrowthSyncSourceToolName, json.RawMessage(`{"source_id":5}`))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGrowth_Handle_JoinReportsWhereItStopped(t *testing.T) {
	out, err := newGrowthTools(&fakeGrowthBackend{err: &connectors.FloodWaitError{Wait: time.Minute}}, 1, 42, nil).
		Handle(t.Context(), GrowthJoinSourceToolName, json.RawMessage(`{"source_ids":[5,"6"]}`))
	require.NoError(t, err)
	assert.Contains(t, out, `"joined"`)
	assert.Contains(t, out, `"stopped":"rate limited by the platform`)
}

func TestGrowth_Handle_ListLeadsScopesToTheFlow(t *testing.T) {
	task := int64(3)
	backend := &fakeGrowthBackend{}
	tools := newGrowthTools(backend, 1, 42, &task)

	_, err := tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{"scope":"delivered","tier":"QUALIFIED"}`))
	require.NoError(t, err)
	require.NotNil(t, backend.lastFilter.FlowID)
	assert.Equal(t, int64(42), *backend.lastFilter.FlowID)
	require.NotNil(t, backend.lastFilter.Delivered)
	assert.True(t, *backend.lastFilter.Delivered)
	assert.Equal(t, "qualified", backend.lastFilter.Tier)

	_, err = tools.Handle(t.Context(), GrowthListLeadsToolName,
		json.RawMessage(`{"scope":"all","since":"2026-10-17T00:00:00+07:00"}`))
	require.NoError(t, err)
	assert.Nil(t, backend.lastFilter.FlowID, "all is every lead of the user")
	assert.Nil(t, backend.lastFilter.TaskID)
	assert.True(t, time.Date(2026, 10, 16, 17, 0, 0, 0, time.UTC).Equal(backend.lastFilter.Since), backend.lastFilter.Since)

	_, err = tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{"scope":"everyone"}`))
	assert.Error(t, err)
	_, err = tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{"since":"today"}`))
	assert.ErrorContains(t, err, "is not a time")
}

func TestGrowth_Handle_ProgressCountsFromSince(t *testing.T) {
	backend := &fakeGrowthBackend{}
	_, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthGetProgressToolName,
		json.RawMessage(`{"since":"2026-10-17"}`))
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC).Equal(backend.lastSince), backend.lastSince)
}
