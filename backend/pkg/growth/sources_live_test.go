//go:build postgres

package growth

import (
	"context"
	"errors"
	"testing"

	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSources_AddSources_OneRowPerGroupHoweverItArrives(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.tg.AddSource(tgGroup("100", "RpgFans", "RPG fans"))

	found, err := w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "rpg", 10)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.True(t, found[0].New)
	assert.Equal(t, OriginAI, found[0].Origin)

	pasted, err := w.svc.AddSources(ctx, scope, []string{"https://t.me/rpgfans/", "@RpgFans", "t.me/joinchat"}, OriginManual, nil)
	require.NoError(t, err)
	require.Len(t, pasted, 3)
	assert.Equal(t, AddResultExisting, pasted[0].Result)
	assert.Equal(t, found[0].ID, pasted[0].Source.ID)
	assert.Equal(t, AddResultExisting, pasted[1].Result)
	assert.Equal(t, AddResultInvalid, pasted[2].Result)

	pool, err := w.svc.ListSources(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, pool, 1)
	assert.Equal(t, OriginManual, pool[0].Origin, "a pasted link marks the source as the customer's")
}

func TestSources_AddSources_KeepsAnUnresolvableLinkAndMergesItOnceResolved(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.tg.Err = errors.New("network down")

	res, err := w.svc.AddSources(ctx, scope, []string{"https://t.me/strategy_hub"}, OriginManual, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, AddResultUnresolved, res[0].Result)
	placeholder := res[0].Source.ID

	w.tg.Err = nil
	w.tg.AddSource(tgGroup("200", "strategy_hub", "Strategy hub"))
	found, err := w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "strategy", 10)
	require.NoError(t, err)
	require.Len(t, found, 1)

	pool, err := w.svc.ListSources(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, pool, 1, "the placeholder is dropped once the same group is known by id")
	assert.NotEqual(t, placeholder, pool[0].ID)
	assert.Equal(t, SourceStatusActive, pool[0].Status)
}

func TestSources_JoinSources_RecordsEachOutcomeAndSkipsJoined(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	open := w.mustAddTelegramGroup(t, scope, tgGroup("300", "open_group", "Open"))
	closed := w.mustAddTelegramGroup(t, scope, tgGroup("301", "closed_group", "Closed"))
	w.tg.SetJoinResult("301", connectors.JoinResult{Status: connectors.JoinStatusPending, Reason: "approval required"})
	web, err := w.svc.AddSources(ctx, scope, []string{"https://forum.example.com/t/42"}, OriginManual, nil)
	require.NoError(t, err)

	out, err := w.svc.JoinSources(ctx, scope, []int64{open, closed, web[0].Source.ID, 999999})
	require.NoError(t, err)
	require.Len(t, out, 4)
	assert.Equal(t, "joined", out[0].JoinStatus)
	assert.Equal(t, "pending", out[1].JoinStatus)
	assert.Equal(t, "not_applicable", out[2].JoinStatus)
	assert.Equal(t, "error", out[3].JoinStatus)

	again, err := w.svc.JoinSources(ctx, scope, []int64{open})
	require.NoError(t, err)
	assert.Equal(t, "already joined", again[0].Reason)
	assert.Equal(t, 1, w.tg.Joined["300"], "a joined group is not joined twice")
}

func TestSources_JoinSources_StopsOnARateLimit(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	a := w.mustAddTelegramGroup(t, scope, tgGroup("310", "group_a", "A"))
	b := w.mustAddTelegramGroup(t, scope, tgGroup("311", "group_b", "B"))
	w.tg.Err = &connectors.FloodWaitError{Wait: 600e9}

	out, err := w.svc.JoinSources(ctx, scope, []int64{a, b})
	var flood *connectors.FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Len(t, out, 1, "the second source is not tried against the same limit")
}

func TestSources_SyncSource_PagesBackwardsAndTracksYield(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("400", "moba_talk", "MOBA talk"),
		tgMessage(1, "11", "first"), tgMessage(2, "12", "second"), tgMessage(3, "13", "third"))

	page, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 2)
	require.NoError(t, err)
	require.Len(t, page.Messages, 2)
	assert.Equal(t, "3", page.Messages[0].MessageID)
	assert.Equal(t, "2", page.Messages[1].MessageID)
	assert.Equal(t, 0, page.Source.EmptyStreak)

	w.mustSaveQualified(t, scope, id, 12, 1)

	page, err = w.svc.SyncSource(ctx, scope, id, DirectionOlder, 2)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "1", page.Messages[0].MessageID)
	assert.True(t, page.Source.HistoryExhausted)
	assert.Equal(t, 1, page.Source.QualifiedLeads)
	assert.Equal(t, 0, page.Source.EmptyStreak, "the previous read produced a qualified lead")

	for range 3 {
		page, err = w.svc.SyncSource(ctx, scope, id, DirectionNewer, 2)
		require.NoError(t, err)
		assert.Empty(t, page.Messages)
	}
	assert.Equal(t, 3, page.Source.EmptyStreak)
	assert.Equal(t, "stale", page.Source.Health)

	w.tg.AddSource(connectors.Source{ExternalID: "400"}, tgMessage(4, "14", "fourth"))
	page, err = w.svc.SyncSource(ctx, scope, id, DirectionNewer, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "4", page.Messages[0].MessageID)
}

func TestSources_SyncSource_SendsAWebSourceToTheBrowser(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	res, err := w.svc.AddSources(ctx, scope, []string{"https://www.Forum.example.com/t/42/?utm_source=x#reply"}, OriginManual, nil)
	require.NoError(t, err)
	require.Equal(t, AddResultAdded, res[0].Result)
	assert.Equal(t, "web", res[0].Source.Platform)

	again, err := w.svc.AddSources(ctx, scope, []string{"forum.example.com/t/42"}, OriginManual, nil)
	require.NoError(t, err)
	assert.Equal(t, AddResultExisting, again[0].Result)

	_, err = w.svc.SyncSource(ctx, scope, res[0].Source.ID, DirectionOlder, 10)
	assert.ErrorIs(t, err, ErrBrowserSource)
}

func TestSources_AddSources_RecordsWhichSourceMentionedTheLink(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	parent := w.mustAddTelegramGroup(t, scope, tgGroup("500", "parent_group", "Parent"))

	res, err := w.svc.AddSources(ctx, scope, []string{"https://forum.example.com/t/7"}, OriginAI, &parent)
	require.NoError(t, err)
	require.NotNil(t, res[0].Source.DiscoveredFrom)
	assert.Equal(t, parent, *res[0].Source.DiscoveredFrom)

	other := int64(987654)
	_, err = w.svc.AddSources(ctx, scope, []string{"https://forum.example.com/t/8"}, OriginAI, &other)
	assert.ErrorIs(t, err, ErrSourceNotFound)
}

func TestSources_SearchSources_NeedsAnAccount(t *testing.T) {
	w := newWorld(t)
	other := seedUser(t, w.db, "nobody@example.test")

	_, err := w.svc.SearchSources(context.Background(), Scope{UserID: other}, connectors.PlatformTelegram, "rpg", 5)
	assert.ErrorIs(t, err, ErrNoAccount)
}

func TestSources_SyncSource_MovesARejectedSessionToNeedsLogin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("600", "fps_squad", "FPS"), tgMessage(1, "1", "hi"))
	w.tg.Err = connectors.ErrUnauthorized

	_, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.ErrorIs(t, err, connectors.ErrUnauthorized)

	_, err = w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	assert.ErrorIs(t, err, ErrNoAccount, "the account waits for an operator to log it in again")
}
