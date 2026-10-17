//go:build postgres

package growth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	w.judgeAsAGamePublisher(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("901", "card_games", "Cards"))
	w.svc.cfg.TypeSafeAPIURL = signalJev(t).URL
	says := func(messageID, excerpt string, score int) LeadInput {
		in := qualified("7", src, messageID)
		in.Score, in.Evidence[0].Excerpt = score, excerpt
		return in
	}

	watch := says("w1", "I play and top up", 5)
	low := says("w2", "I play", 3)
	low.PlatformUserID = "8"
	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{watch, low})
	require.NoError(t, err)
	assert.Equal(t, 5, res[0].Score)
	assert.Equal(t, TierWatchlist, res[0].Tier)
	assert.Contains(t, res[0].Reason, `tier "watchlist" follows from score 5`)
	assert.Equal(t, LeadStatusRejected, res[1].Status)

	res, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{says("w3", "bored, so I play and top up", 8)})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusUpgraded, res[0].Status)

	res, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{says("w4", "I play and top up", 5)})
	require.NoError(t, err)
	assert.Equal(t, TierQualified, res[0].Tier, "a weaker sighting does not demote")
	assert.Equal(t, 8, res[0].Score)
}

func TestLeads_UpsertLeads_JudgesTheExcerptsOfAWebLeadOnce(t *testing.T) {
	w := newWorld(t)
	w.judgeAsAGamePublisher(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	jev := signalJev(t)
	var calls atomic.Int32
	counted := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jev.Config.Handler.ServeHTTP(rw, r)
	}))
	t.Cleanup(counted.Close)
	w.svc.cfg.TypeSafeAPIURL = counted.URL
	added, err := w.svc.AddSources(ctx, scope, []string{"https://forum.example.com/t/42"}, OriginManual, nil)
	require.NoError(t, err)
	page := added[0].Source.ID
	lead := func(user, messageID, excerpt string) LeadInput {
		return LeadInput{PlatformUserID: user, Score: 3, Evidence: []EvidenceInput{{SourceID: page, MessageID: messageID, Excerpt: excerpt}}}
	}

	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{
		lead("gamer1", "p1", "bored of my RPG, I play daily and top up"),
		lead("chef", "p2", "lol bored of my RPG, I play daily and top up"),
	})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusNew, res[0].Status)
	assert.Equal(t, 11, res[0].Score)
	assert.Equal(t, TierQualified, res[0].Tier)
	assert.Contains(t, res[0].Reason, "score 11 is the judgement model's for this evidence, not 3")
	assert.Equal(t, LeadStatusRejected, res[1].Status, "the excerpt shows the sender is excluded, whatever its signals")
	assert.Equal(t, int32(2), calls.Load())

	res, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{lead("gamer1", "p1", "bored of my RPG, I play daily and top up")})
	require.NoError(t, err)
	assert.Equal(t, 11, res[0].Score)
	assert.Equal(t, int32(2), calls.Load(), "the stored score is reused")
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

func TestLeads_UpsertLeads_KeepsEveryContactOnceAndListsByIt(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("901", "card_fans", "Cards"))

	reachable := qualified("41", src, "m1")
	reachable.Contacts = []Contact{{Type: "email", Value: "ann@example.test"}, {Type: "WeChat", Value: "ann_wx"},
		{Type: strings.Repeat("x", 31), Value: "123"}, {Type: "platform_handle", Value: "@u41"}}
	res, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{reachable, qualified("42", src, "m2")})
	require.NoError(t, err)
	assert.Contains(t, res[0].Reason, "is empty or longer than 30 characters")

	again := qualified("41", src, "m3")
	again.Contacts = []Contact{{Type: " Email ", Value: "ANN@example.test"}, {Type: "phone", Value: "+66 81 234 5678"}}
	_, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{again})
	require.NoError(t, err)

	yes, no := true, false
	with, err := w.svc.ListLeads(ctx, w.user, LeadFilter{HasContact: &yes})
	require.NoError(t, err)
	require.Len(t, with, 1)
	assert.Equal(t, "41", with[0].PlatformUserID)
	assert.Equal(t, []Contact{
		{Type: "email", Value: "ann@example.test"}, {Type: "WeChat", Value: "ann_wx"}, {Type: "phone", Value: "+66 81 234 5678"},
	}, with[0].Contacts)
	without, err := w.svc.ListLeads(ctx, w.user, LeadFilter{HasContact: &no})
	require.NoError(t, err)
	require.Len(t, without, 1)
	assert.Equal(t, "42", without[0].PlatformUserID)
	assert.Equal(t, []Contact{}, without[0].Contacts)
}

func TestLeads_UpsertLeads_WhenJevFailsOnTheExcerpts(t *testing.T) {
	jevBackoff = time.Millisecond
	t.Cleanup(func() { jevBackoff = 2 * time.Second })
	webLead := func(t *testing.T, w *world) (Scope, LeadInput) {
		scope := w.seedTask(t)
		added, err := w.svc.AddSources(context.Background(), scope, []string{"https://forum.example.com/t/7"}, OriginManual, nil)
		require.NoError(t, err)
		return scope, LeadInput{PlatformUserID: "fan", Score: 7, Evidence: []EvidenceInput{
			{SourceID: added[0].Source.ID, MessageID: "p1", Excerpt: "bored of my RPG, I play daily"},
		}}
	}

	t.Run("a refused key stops the save", func(t *testing.T) {
		w := newWorld(t)
		refused := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(refused.Close)
		w.svc.cfg.TypeSafeAPIURL = refused.URL
		scope, in := webLead(t, w)

		_, err := w.svc.UpsertLeads(context.Background(), scope, []LeadInput{in})
		require.ErrorIs(t, err, ErrNoJudge)
		leads, err := w.svc.ListLeads(context.Background(), w.user, LeadFilter{})
		require.NoError(t, err)
		assert.Empty(t, leads)
	})

	t.Run("an unreachable Jev leaves the agent's score", func(t *testing.T) {
		w := newWorld(t)
		w.judgeAsAGamePublisher(t)
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()
		w.svc.cfg.TypeSafeAPIURL = gone.URL
		scope, in := webLead(t, w)

		res, err := w.svc.UpsertLeads(context.Background(), scope, []LeadInput{in})
		require.NoError(t, err)
		assert.Equal(t, LeadStatusNew, res[0].Status)
		assert.Equal(t, 7, res[0].Score)
		assert.Equal(t, TierQualified, res[0].Tier)
	})
}
