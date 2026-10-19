package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestGrowth_Handle_AddsEveryLinkAsTheAIsDiscovery(t *testing.T) {
	backend := &fakeGrowthBackend{}
	tools := newGrowthTools(backend, 1, 42, nil)

	_, err := tools.Handle(t.Context(), GrowthAddSourcesToolName, json.RawMessage(`{"links":["t.me/a_group"],"discovered_from_source_id":"9"}`))
	require.NoError(t, err)
	assert.Equal(t, growth.OriginAI, backend.lastOrigin)
	require.NotNil(t, backend.lastFrom)
	assert.Equal(t, int64(9), *backend.lastFrom)

	_, err = tools.Handle(t.Context(), GrowthAddSourcesToolName, json.RawMessage(`{"links":["t.me/a_group"]}`))
	require.NoError(t, err)
	assert.Equal(t, growth.OriginAI, backend.lastOrigin, "a link without the source it came from")
	assert.Nil(t, backend.lastFrom)
}

func TestGrowth_Handle_DeliverPassesAllowPartialAndHasContact(t *testing.T) {
	task := int64(3)
	backend := &fakeGrowthBackend{delivery: growth.Delivery{
		Status: growth.DeliveryIncomplete, Gap: 8, Progress: growth.Progress{Target: 50, NewLeads: 42},
	}}
	out, err := newGrowthTools(backend, 1, 42, &task).Handle(t.Context(), GrowthDeliverLeadsToolName,
		json.RawMessage(`{"allow_partial":"true","has_contact":true}`))
	require.NoError(t, err)
	assert.True(t, backend.lastAllowPartial)
	assert.True(t, backend.lastHasContact)
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

func TestGrowth_Handle_SyncReportsThePrescreenFallbacksOfARead(t *testing.T) {
	out, err := newGrowthTools(&fakeGrowthBackend{fallbacks: 2}, 1, 42, nil).Handle(t.Context(),
		GrowthSyncSourceToolName, json.RawMessage(`{"source_id":5}`))
	require.NoError(t, err)
	assert.Contains(t, out, `"prescreen_fallback_batches":2`)
	assert.Contains(t, out, `"warning":"the pre-screen (Jev) failed on 2 batch(es) in this read"`)

	out, err = newGrowthTools(&fakeGrowthBackend{}, 1, 42, nil).Handle(t.Context(),
		GrowthSyncSourceToolName, json.RawMessage(`{"source_id":5}`))
	require.NoError(t, err)
	assert.NotContains(t, out, "prescreen_fallback_batches")
	assert.NotContains(t, out, `"warning"`)
}

func TestGrowth_Handle_SyncPassesTheResultBudgetAndReportsWhatIsPending(t *testing.T) {
	caughtUp := false
	tests := []struct {
		name string
		args string
		res  growth.SyncResult
		want []string
	}{
		{
			name: "back-read",
			args: `{"source_id":5,"direction":"older"}`,
			res: growth.SyncResult{
				Source:   growth.SourceView{ID: 5, Title: "MOBA talk", Description: strings.Repeat("d", 300), Health: "active"},
				Messages: []growth.MessageView{{MessageID: "50", SenderID: "1", Text: "buy"}},
				Read:     1, Pending: 49, NextFrom: "49",
			},
			want: []string{`"pending":49`, `"next_from_message_id":"49"`, "49 more messages wait, next from message_id 49", "direction=older"},
		},
		{
			name: "forward read",
			args: `{"source_id":5,"direction":"newer"}`,
			res: growth.SyncResult{
				Source:   growth.SourceView{ID: 5, Title: "MOBA talk"},
				Messages: []growth.MessageView{{MessageID: "3", SenderID: "1", Text: "buy"}},
				Read:     1, Pending: 19, NextFrom: "4", CaughtUp: &caughtUp,
			},
			want: []string{`"pending":19`, `"caught_up":false`, "next from message_id 4", "direction=newer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeGrowthBackend{synced: map[int64]growth.SyncResult{5: tt.res}}
			tools := newGrowthTools(backend, 1, 42, nil)
			tools.resultLimit = 10_000

			out, err := tools.Handle(t.Context(), GrowthSyncSourceToolName, json.RawMessage(tt.args))
			require.NoError(t, err)
			assert.Equal(t, 9_488, backend.lastSync.MaxBytes, "the limit less the room for the outer object")
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
			assert.Contains(t, out, `"title":"MOBA talk"`)
			assert.NotContains(t, out, strings.Repeat("d", 300), "the source line carries no description")
		})
	}
}

func TestGrowth_Handle_SyncReturnsAFittingReadUnchanged(t *testing.T) {
	msgs := []growth.MessageView{
		{MessageID: "7", SenderID: "1", Text: strings.Repeat("a", 1500), Score: 8, Tier: "qualified"},
		{MessageID: "6", SenderID: "2", Text: "b"},
	}
	backend := &fakeGrowthBackend{synced: map[int64]growth.SyncResult{5: {
		Source: growth.SourceView{ID: 5}, Messages: msgs, Read: 30, Skipped: 28,
	}}}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthSyncSourceToolName, json.RawMessage(`{"source_id":5}`))
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	assert.EqualValues(t, 2, payload["count"])
	assert.EqualValues(t, 30, payload["read"])
	assert.EqualValues(t, 28, payload["skipped_count"])
	assert.NotContains(t, payload, "pending")
	assert.NotContains(t, payload, "next_from_message_id")
	assert.NotContains(t, payload, "note")
	raw, err := json.Marshal(payload["messages"])
	require.NoError(t, err)
	want, err := json.Marshal(msgs)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(raw), "the messages reach the model as the read returned them")
}

func TestGrowth_Handle_ListLeadsStopsAtTheResultLimitAndNamesTheNextOffset(t *testing.T) {
	leads := make([]growth.LeadView, 12)
	for i := range leads {
		leads[i] = growth.LeadView{ID: int64(i + 1), PlatformUserID: fmt.Sprint(1000 + i), MatchReason: strings.Repeat("r", 200)}
	}
	backend := &fakeGrowthBackend{leads: leads}
	tools := newGrowthTools(backend, 1, 42, nil)
	tools.resultLimit = 2048

	out, err := tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{"offset":"4"}`))
	require.NoError(t, err)
	assert.Equal(t, 4, backend.lastFilter.Offset)
	assert.LessOrEqual(t, len(out), 2048)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	shown := payload["leads"].([]any)
	require.NotEmpty(t, shown)
	assert.Less(t, len(shown), 12)
	assert.EqualValues(t, len(shown), payload["count"])
	assert.EqualValues(t, 4, payload["offset"])
	assert.Contains(t, payload["note"], fmt.Sprintf("%d more of the 12 leads", 12-len(shown)))
	assert.Contains(t, payload["note"], fmt.Sprintf("offset=%d", 4+len(shown)))
	assert.EqualValues(t, 1, shown[0].(map[string]any)["lead_id"])

	tools.resultLimit = 100_000
	out, err = tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, 0, backend.lastFilter.Offset)
	payload = map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	assert.EqualValues(t, 12, payload["count"])
	assert.Len(t, payload["leads"], 12)
	assert.NotContains(t, payload, "note")
	assert.NotContains(t, payload, "offset")
}

func TestGrowth_FitItems_KeepsTheFirstAndCutsAtTheBudget(t *testing.T) {
	// Each item serializes to 6 bytes and is charged 7 with its separator.
	items := []string{"aaaa", "bbbb", "cccc"}
	tests := []struct {
		name     string
		items    []string
		budget   int
		wantKept int
		wantLeft int
	}{
		{"nothing to show", nil, 100, 0, 0},
		{"everything fits", items, 21, 3, 0},
		{"the budget ends between items", items, 20, 2, 1},
		{"one less than two items", items, 13, 1, 2},
		{"the first item alone is over budget", items, 1, 1, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kept, left := fitItems(tt.items, tt.budget)
			assert.Len(t, kept, tt.wantKept)
			assert.Equal(t, tt.wantLeft, left)
		})
	}
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
	assert.Nil(t, backend.lastFilter.HasContact)
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

	_, err = tools.Handle(t.Context(), GrowthListLeadsToolName, json.RawMessage(`{"has_contact":true}`))
	require.NoError(t, err)
	require.NotNil(t, backend.lastFilter.HasContact)
	assert.True(t, *backend.lastFilter.HasContact)

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

func TestGrowth_Handle_UpsertPassesTheContactsAndAnOmittedScore(t *testing.T) {
	task := int64(3)
	backend := &fakeGrowthBackend{}
	_, err := newGrowthTools(backend, 1, 42, &task).Handle(t.Context(), GrowthUpsertLeadsToolName, json.RawMessage(`{"leads":[{
		"platform_user_id":"31","match_reason":"wants a new game and tops up often",
		"contacts":[{"type":"email","value":"ann@example.test"}],
		"evidence":[{"source_id":7,"message_id":"1","excerpt":"bored of my RPG"}]}]}`))
	require.NoError(t, err)
	require.Len(t, backend.upserted, 1)
	assert.Equal(t, 0, backend.upserted[0].Score)
	assert.Equal(t, []growth.Contact{{Type: "email", Value: "ann@example.test"}}, backend.upserted[0].Contacts)
}

func TestGrowth_Handle_ReadsTheTasksProfileOrAChosenOne(t *testing.T) {
	backend := &fakeGrowthBackend{
		profile:  growth.ProfileView{ID: 3, Title: "agents", Profile: growth.Profile{Name: "Acme"}},
		profiles: []growth.ProfileView{{ID: 3, Title: "agents", Default: true}, {ID: 4, Title: "players"}},
	}
	tools := newGrowthTools(backend, 1, 42, nil)

	out, err := tools.Handle(t.Context(), GrowthGetProfileToolName, json.RawMessage(`{"message":"m"}`))
	require.NoError(t, err)
	assert.Equal(t, growth.Scope{UserID: 1, FlowID: 42}, backend.lastScope)
	assert.Contains(t, out, `"profile_id":3`)
	assert.Contains(t, out, `"saved_profiles":[{"profile_id":3,"title":"agents","default":true},{"profile_id":4,"title":"players","default":false}]`)
	assert.Contains(t, out, `"a stated age under 18"`)

	_, err = tools.Handle(t.Context(), GrowthGetProfileToolName, json.RawMessage(`{"profile_id":"4","message":"m"}`))
	require.NoError(t, err)
	assert.Equal(t, int64(4), backend.lastProfile)
}

func TestGrowth_Handle_DraftsTheWholeProfileSentAsAnObjectOrAString(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"object", `{"title":"MY agents","profile_id":"3","profile":{"name":"Acme","regions":["MY"]},"message":"m"}`},
		{"string", `{"title":"MY agents","profile_id":3,"profile":"{\"name\":\"Acme\",\"regions\":[\"MY\"]}","message":"m"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeGrowthBackend{}
			out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthDraftProfileToolName, json.RawMessage(tt.args))
			require.NoError(t, err)

			assert.Equal(t, growth.Scope{UserID: 1, FlowID: 42}, backend.lastScope)
			assert.Equal(t, "MY agents", backend.lastTitle)
			require.NotNil(t, backend.lastBase)
			assert.Equal(t, int64(3), *backend.lastBase)
			assert.Equal(t, "Acme", backend.lastDraft.Name)
			assert.Equal(t, []string{"MY"}, backend.lastDraft.Regions)
			assert.Contains(t, out, GrowthSaveProfileToolName)
		})
	}
}

func TestGrowth_Handle_TellsTheModelHowToFixAnInvalidDraft(t *testing.T) {
	backend := &fakeGrowthBackend{err: fmt.Errorf("%w: scoring needs 1 <= watchlist < qualified", growth.ErrInvalidProfile)}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthDraftProfileToolName,
		json.RawMessage(`{"title":"t","profile":{"name":"Acme"},"message":"m"}`))

	require.NoError(t, err)
	assert.Contains(t, out, "watchlist < qualified")
	assert.Contains(t, out, "call growth_draft_profile again")
}

func TestGrowth_Handle_UpdatesOnePromptOnTheSavedProfile(t *testing.T) {
	backend := &fakeGrowthBackend{profile: growth.ProfileView{ID: 3, Title: "agents", EditedBy: "assistant", HasPrevious: true,
		Profile: growth.Profile{Need: "players", Exclude: "bots\nresellers"}}}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthUpdateProfileToolName,
		json.RawMessage(`{"profile_id":"3","action":"append","field":"exclude","text":"people in Singapore","message":"m"}`))

	require.NoError(t, err)
	assert.Equal(t, int64(3), backend.lastProfile)
	assert.Equal(t, growth.ProfileOp{Action: "append", Field: "exclude", Text: "people in Singapore"}, backend.lastOp)
	assert.Contains(t, out, `"has_previous":true`)
	assert.Contains(t, out, `"exclude":"bots\nresellers"`)
}

func TestGrowth_Handle_SavesTheDraftWithTheChosenOptions(t *testing.T) {
	backend := &fakeGrowthBackend{profile: growth.ProfileView{ID: 9, Title: "MY agents"}}
	out, err := newGrowthTools(backend, 1, 42, nil).Handle(t.Context(), GrowthSaveProfileToolName,
		json.RawMessage(`{"use_for_task":"true","as_new":true,"message":"m"}`))

	require.NoError(t, err)
	assert.Equal(t, growth.Scope{UserID: 1, FlowID: 42}, backend.lastScope)
	assert.Equal(t, growth.ApplyDraftOptions{AsNew: true, UseForFlow: true}, backend.lastApply)
	assert.Contains(t, out, `"profile_id":9`)
}
