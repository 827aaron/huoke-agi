package growth

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"pentagi/pkg/config"
	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_RoundNumbers_CountsEachFlowFromOneOldestFirst(t *testing.T) {
	flow := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	runs := []database.GrowthRun{
		{ID: 9, FlowID: flow(2)}, {ID: 7, FlowID: flow(1)}, {ID: 5, FlowID: flow(2)}, {ID: 3, FlowID: flow(1)},
	}
	assert.Equal(t, map[int64]int{3: 1, 7: 2, 5: 1, 9: 2}, RoundNumbers(runs))
}

// progressStore is the store of an owner with no leads and no saved settings.
type progressStore struct{ database.Querier }

func (progressStore) CountUserLeads(ctx context.Context, _ database.CountUserLeadsParams) (database.CountUserLeadsRow, error) {
	return database.CountUserLeadsRow{}, ctx.Err()
}

func (progressStore) GetUserSourceStats(ctx context.Context, _ int64) (database.GetUserSourceStatsRow, error) {
	return database.GetUserSourceStatsRow{}, ctx.Err()
}

func (progressStore) GetUserGrowthSettings(ctx context.Context, _ int64) ([]database.GrowthSetting, error) {
	return nil, ctx.Err()
}

func TestRunner_ProgressSince_WarnsOfAMissingJevKeyAndTheRoundsPrescreenFallbacks(t *testing.T) {
	ctx := context.Background()
	scope := Scope{UserID: 1, FlowID: 10}

	keyed := NewService(progressStore{}, &config.Config{TypeSafeAPIKey: "k"})
	p, err := keyed.Progress(ctx, scope)
	require.NoError(t, err)
	assert.Equal(t, 0, p.PrescreenFallbacks)
	assert.Equal(t, "", p.Warning)

	keyed.notePrescreenFallbacks(scope, 2)
	p, err = keyed.Progress(ctx, scope)
	require.NoError(t, err)
	assert.Equal(t, 2, p.PrescreenFallbacks)
	assert.Equal(t, "the pre-screen (Jev) failed on 2 batch(es) this round: those messages came back unscreened and unscored, "+
		"so the two-stage judgement did not save tokens on them; check the Jev key and model on the settings page", p.Warning)

	unkeyed := NewService(progressStore{}, &config.Config{})
	unkeyed.notePrescreenFallbacks(scope, 1)
	p, err = unkeyed.Progress(ctx, scope)
	require.NoError(t, err)
	assert.Equal(t, "no judgement model (Jev) is configured: ask the user to fill in its key on the settings page; "+
		"the pre-screen (Jev) failed on 1 batch(es) this round: those messages came back unscreened and unscored, "+
		"so the two-stage judgement did not save tokens on them; check the Jev key and model on the settings page", p.Warning)

	raw, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"prescreen_fallback_batches":1`)
	assert.Contains(t, string(raw), `"warning":"no judgement model (Jev) is configured`)
}
