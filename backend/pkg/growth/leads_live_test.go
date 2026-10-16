//go:build postgres

package growth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeads_UpsertLeads_OneLeadPerPersonAcrossFlows(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	first := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, first, tgGroup("900", "puzzle_fans", "Puzzle"))

	res, err := w.svc.UpsertLeads(ctx, first, []LeadInput{qualified("42", src, "m1")})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusNew, res[0].Status)
	assert.Equal(t, 1, res[0].EvidenceAdded)

	second := w.seedTask(t)
	res, err = w.svc.UpsertLeads(ctx, second, []LeadInput{qualified("42", src, "m1"), qualified("42", src, "m2")})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusExisting, res[0].Status)
	assert.Equal(t, 0, res[0].EvidenceAdded, "the same message is evidence once")
	assert.Equal(t, 1, res[1].EvidenceAdded)

	leads, err := w.svc.ListLeads(ctx, w.user, LeadFilter{})
	require.NoError(t, err)
	require.Len(t, leads, 1)
	assert.Len(t, leads[0].Evidence, 2)
	require.NotNil(t, leads[0].FlowID)
	assert.Equal(t, second.FlowID, *leads[0].FlowID, "the lead belongs to the flow that saw it last")
}

func TestLeads_UpsertLeads_TierFollowsTheScore(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("901", "card_games", "Cards"))

	watch := qualified("7", src, "w1")
	watch.Score, watch.Tier = 5, TierQualified
	low := qualified("8", src, "w2")
	low.Score = 3
	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{watch, low})
	require.NoError(t, err)
	assert.Equal(t, TierWatchlist, res[0].Tier)
	assert.Contains(t, res[0].Reason, `tier "watchlist" follows from score 5`)
	assert.Equal(t, LeadStatusRejected, res[1].Status)

	up := qualified("7", src, "w3")
	up.Score = 8
	res, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{up})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusUpgraded, res[0].Status)

	down := qualified("7", src, "w4")
	down.Score = 4
	res, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{down})
	require.NoError(t, err)
	assert.Equal(t, TierQualified, res[0].Tier, "a weaker sighting does not demote")
	assert.Equal(t, 8, res[0].Score)
}

func TestLeads_UpsertLeads_RejectsEvidenceOutsideThePool(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	other := seedUser(t, w.db, "rival@example.test")
	var foreign int64
	require.NoError(t, w.db.QueryRow(`
		INSERT INTO growth_sources (user_id, platform, source_key, link, origin)
		VALUES ($1, 'telegram', 'id:1', 'https://t.me/rival_pool', 'manual') RETURNING id`, other).Scan(&foreign))

	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{qualified("1", foreign, "m1"), {PlatformUserID: "2", Score: 9}})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusRejected, res[0].Status)
	assert.Contains(t, res[0].Reason, "not in the source pool")
	assert.Equal(t, LeadStatusRejected, res[1].Status)
}

func TestLeads_UpsertLeads_KeysWebPeopleByProfileOrForumName(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	page, err := w.svc.AddSources(ctx, scope, []string{"https://www.forum.example.com/t/42"}, OriginManual, nil)
	require.NoError(t, err)
	src := page[0].Source.ID

	byName := LeadInput{Username: "@GamerOne", Score: 7, Evidence: []EvidenceInput{{SourceID: src, Excerpt: "looking for a new mmo"}}}
	byProfile := LeadInput{
		PlatformUserID: "https://forum.example.com/u/42/?utm_source=x", Score: 7,
		Evidence: []EvidenceInput{{SourceID: src, MessageLink: "https://forum.example.com/t/42#p7", Excerpt: "any recs?"}},
	}
	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{byName, byProfile})
	require.NoError(t, err)
	assert.Equal(t, "forum.example.com:GamerOne", res[0].PlatformUserID)
	assert.Equal(t, "forum.example.com/u/42", res[1].PlatformUserID)

	lead, err := w.svc.GetLead(ctx, w.user, res[0].LeadID)
	require.NoError(t, err)
	require.Len(t, lead.Evidence, 1)
	assert.Equal(t, "forum.example.com/t/42", lead.Evidence[0].MessageID, "a page without a message id is keyed by its link")
	assert.WithinDuration(t, time.Now(), lead.Evidence[0].CapturedAt, time.Minute)

	again, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{byName})
	require.NoError(t, err)
	assert.Equal(t, 0, again[0].EvidenceAdded)
}

func TestLeads_UpsertLeads_CountsQualifiedLeadsPerSource(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	rich := w.mustAddTelegramGroup(t, scope, tgGroup("902", "rich_group", "Rich"))
	poor := w.mustAddTelegramGroup(t, scope, tgGroup("903", "poor_group", "Poor"))
	w.mustSaveQualified(t, scope, rich, 1, 3)

	pool, err := w.svc.ListSources(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, pool, 2)
	assert.Equal(t, rich, pool[0].ID, "the productive source comes first")
	assert.Equal(t, 3, pool[0].QualifiedLeads)
	assert.Equal(t, poor, pool[1].ID)
	assert.Equal(t, 0, pool[1].QualifiedLeads)
}
