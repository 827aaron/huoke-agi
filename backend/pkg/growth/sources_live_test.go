//go:build postgres

package growth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	w.savePace(t, map[string]string{"join_interval_minutes": "0"})
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

func TestSources_SyncSources_StopsWhenThePlatformIsUnreachable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	a := w.mustAddTelegramGroup(t, scope, tgGroup("312", "group_c", "C"), tgMessage(1, "11", "hi"))
	b := w.mustAddTelegramGroup(t, scope, tgGroup("313", "group_d", "D"), tgMessage(1, "12", "hi"))
	w.tg.Err = fmt.Errorf("%w: no connection to telegram within 15s", connectors.ErrUnreachable)

	out, err := w.svc.SyncSources(ctx, scope, []int64{a, b}, SyncOptions{})
	require.ErrorIs(t, err, connectors.ErrUnreachable)
	assert.Len(t, out, 1, "the second source does not wait for the same dead route")
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

func TestSources_AddSources_KeepsAFacebookGroupForASignedInAccount(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	res, err := w.svc.AddSources(ctx, scope, []string{"https://m.facebook.com/groups/thaigamers/?ref=share"}, OriginManual, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, AddResultUnresolved, res[0].Result)
	assert.Equal(t, "facebook", res[0].Source.Platform)
	assert.Equal(t, "facebook.com/groups/thaigamers", res[0].Source.Link)
	assert.Contains(t, res[0].Reason, "kept unresolved: ")
	t.Log(res[0].Reason)
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

func TestSources_JoinSources_JoinsPastThePaceWithAReminder(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	acc := w.firstAccount(t)
	a := w.mustAddTelegramGroup(t, scope, tgGroup("320", "group_a", "A"))
	b := w.mustAddTelegramGroup(t, scope, tgGroup("321", "group_b", "B"))
	c := w.mustAddTelegramGroup(t, scope, tgGroup("322", "group_c", "C"))

	w.signedInDaysAgo(t, acc, 3.1)
	out, err := w.svc.JoinSources(ctx, scope, []int64{a, b})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "joined", out[0].JoinStatus)
	assert.Empty(t, out[0].Advice, "the fourth day suggests one join")
	assert.Equal(t, "joined", out[1].JoinStatus, "a reminder never stops the join")
	assert.Equal(t, []string{AdviceDailyJoins, AdviceJoinInterval}, out[1].Advice)
	assert.Contains(t, out[1].Warning, "suggested joins for the last 24 hours")
	assert.Equal(t, 1, w.tg.Joined["321"])

	accounts, err := w.svc.ListAccounts(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, []string{AdviceDailyJoins, AdviceJoinInterval}, accounts[0].JoinAdvice)
	assert.NotNil(t, accounts[0].NextJoinAt)

	w.savePace(t, map[string]string{"risk_reminders": "off"})
	out, err = w.svc.JoinSources(ctx, scope, []int64{c})
	require.NoError(t, err)
	assert.Equal(t, "joined", out[0].JoinStatus)
	assert.Empty(t, out[0].Advice, "reminders are off")
	accounts, err = w.svc.ListAccounts(ctx, w.user)
	require.NoError(t, err)
	assert.Empty(t, accounts[0].JoinAdvice)
}

func TestSources_JoinSources_PrefersAnAccountWithinThePace(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.signedInDaysAgo(t, w.firstAccount(t), 0.5)
	fresh, other := w.addAccount(t, "seasoned")
	w.signedInDaysAgo(t, fresh, 30)
	src := tgGroup("325", "group_f", "F")
	other.AddSource(src)
	id := w.mustAddTelegramGroup(t, scope, src)
	_, err := w.db.Exec(`UPDATE channel_accounts SET last_used_at = CASE WHEN id = $1 THEN now() END`, fresh)
	require.NoError(t, err, "the warming-up account would otherwise be the least recently used")

	out, err := w.svc.JoinSources(ctx, scope, []int64{id})
	require.NoError(t, err)
	assert.Empty(t, out[0].Advice)
	assert.Equal(t, 1, other.Joined["325"], "the seasoned account joins")
	assert.Zero(t, w.tg.Joined["325"])
}

func TestSources_JoinSources_CountsInviteLinksOnTheirOwn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"join_interval_minutes": "0", "private_joins_per_day": "1"})
	invite := func(id, hash string) int64 {
		src := tgGroup(id, "", "Private "+id)
		src.Link = "https://t.me/+" + hash
		return w.mustAddTelegramGroup(t, scope, src)
	}
	first, second := invite("330", "AAAAbbbb"), invite("331", "CCCCdddd")
	public := w.mustAddTelegramGroup(t, scope, tgGroup("332", "public_c", "C"))

	out, err := w.svc.JoinSources(ctx, scope, []int64{first, second, public})
	require.NoError(t, err)
	assert.Empty(t, out[0].Advice)
	assert.Equal(t, []string{AdvicePrivateJoins}, out[1].Advice)
	assert.Empty(t, out[2].Advice, "a public group is not counted with invites")
}

func TestSources_SyncSource_BackReadsANewGroupOnlySoFar(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	msgs := []connectors.Message{tgMessage(1, "11", "old"), tgMessage(2, "12", "b"), tgMessage(3, "13", "c"),
		tgMessage(4, "14", "d"), tgMessage(5, "15", "e")}
	msgs[0].Time = time.Now().UTC().AddDate(0, 0, -8)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("340", "history", "History"), msgs...)

	page, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 3)
	require.NoError(t, err)
	assert.Len(t, page.Messages, 3)
	page, err = w.svc.SyncSource(ctx, scope, id, DirectionOlder, 3)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1, "message 1 is older than seven days")
	assert.Equal(t, "2", page.Messages[0].MessageID)
	page, err = w.svc.SyncSource(ctx, scope, id, DirectionOlder, 3)
	require.NoError(t, err)
	assert.Empty(t, page.Messages)

	w.savePace(t, map[string]string{"first_sync_messages": "2", "read_interval_seconds": "0"})
	other := w.mustAddTelegramGroup(t, scope, tgGroup("341", "history_b", "History B"),
		tgMessage(1, "21", "a"), tgMessage(2, "22", "b"), tgMessage(3, "23", "c"))
	page, err = w.svc.SyncSource(ctx, scope, other, DirectionOlder, 10)
	require.NoError(t, err)
	assert.Len(t, page.Messages, 2)
	page, err = w.svc.SyncSource(ctx, scope, other, DirectionOlder, 10)
	require.NoError(t, err)
	assert.Empty(t, page.Messages, "the back-read stops at the message count")

	w.tg.AddSource(tgGroup("341", "history_b", "History B"), tgMessage(4, "24", "new"))
	page, err = w.svc.SyncSource(ctx, scope, other, DirectionNewer, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1, "new messages are still read")
	assert.Equal(t, "4", page.Messages[0].MessageID)
}

func TestSources_JoinWarnings_NamesAccountsAtTheWarningCount(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"join_interval_minutes": "0", "joined_groups_warning": "2"})
	a := w.mustAddTelegramGroup(t, scope, tgGroup("350", "group_d", "D"))
	b := w.mustAddTelegramGroup(t, scope, tgGroup("351", "group_e", "E"))

	_, err := w.svc.JoinSources(ctx, scope, []int64{a})
	require.NoError(t, err)
	warnings, err := w.svc.JoinWarnings(ctx, w.user)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	_, err = w.svc.JoinSources(ctx, scope, []int64{b})
	require.NoError(t, err)
	warnings, err = w.svc.JoinWarnings(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, JoinWarning{AccountID: w.firstAccount(t), Platform: "telegram", Label: "default", JoinedSources: 2, WarnAt: 2},
		warnings[0])

	accounts, err := w.svc.ListAccounts(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, int64(2), accounts[0].JoinedSources)
	assert.Equal(t, int64(2), accounts[0].JoinsToday)
}

func TestSources_SyncSources_ReturnsWhatThePreScreenPicks(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	p := gameProfile()
	p.Signals = []string{"looking for a new game"}
	_, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: p})
	require.NoError(t, err)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	jevBackoff = time.Millisecond
	t.Cleanup(func() { jevBackoff = 2 * time.Second })

	var requests []map[string]any
	fail := false
	jev := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		requests = append(requests, body)
		if fail && len(requests) == 2 {
			rw.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		state := body["state"].(map[string]any)
		answers := map[string]any{}
		for qid, m := range state["messages"].(map[string]any) {
			excluded, text := 0.9, m.(map[string]any)["text"].(string)
			switch {
			case strings.Contains(text, "buy"):
				excluded = 0.1
			case strings.HasSuffix(text, "5"):
				excluded = 0.55
			}
			answers[qid] = map[string]any{"type": "noul", "noul": excluded}
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"answers": answers})
	}))
	t.Cleanup(jev.Close)
	w.svc.cfg.TypeSafeAPIURL, w.svc.cfg.TypeSafeAPIKey = jev.URL, ""

	msgs := make([]connectors.Message, 0, 45)
	for i := int64(1); i <= 45; i++ {
		text := fmt.Sprintf("message %d", i)
		if i%10 == 0 {
			text = fmt.Sprintf("where to buy %d", i)
		}
		msgs = append(msgs, tgMessage(i, fmt.Sprint(100+i), text))
	}
	a := w.mustAddTelegramGroup(t, scope, tgGroup("360", "screen_a", "Screen A"), msgs...)
	b := w.mustAddTelegramGroup(t, scope, tgGroup("361", "screen_b", "Screen B"), tgMessage(1, "21", "lol"))

	unjudged, err := w.svc.SyncSources(ctx, scope, []int64{b, a}, SyncOptions{})
	assert.ErrorIs(t, err, ErrNoJudge)
	require.Len(t, unjudged, 1, "a missing key stops the batch")
	assert.Empty(t, requests)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningCustom, map[string]string{
		"api_key": "ts-key", "threshold": "0.7", "api_url": w.svc.cfg.TypeSafeAPIURL,
	})
	require.NoError(t, err)
	fail = true
	results, err := w.svc.SyncSources(ctx, scope, []int64{a, 999999, b}, SyncOptions{Limit: 45})
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.Len(t, requests, 3, "45 messages in two requests, then the other source")
	state := requests[0]["state"].(map[string]any)
	assert.Equal(t, "telegram", state["platform"])
	assert.Equal(t, "Screen A", state["source"])
	assert.Contains(t, state["business"], "looking for a new game")
	assert.Len(t, state["messages"], 40)
	assert.Equal(t, "@u145", state["messages"].(map[string]any)["m0"].(map[string]any)["sender"])

	ids := []string{}
	for _, m := range results[0].Messages {
		ids = append(ids, m.MessageID)
	}
	assert.Equal(t, []string{"40", "30", "20", "10", "5", "4", "3", "2", "1"}, ids, "a batch Jev fails on is kept whole")
	assert.Equal(t, 45, results[0].Read)
	assert.Equal(t, 36, results[0].Skipped)
	assert.ErrorIs(t, results[1].Err, ErrSourceNotFound, "a bad id does not stop the batch")
	assert.Equal(t, 1, results[2].Read, "the source the missing key stopped is read from where it was")

	w.tg.AddSource(tgGroup("360", "screen_a", "Screen A"), tgMessage(46, "15", "hello"))
	all, err := w.svc.SyncSources(ctx, scope, []int64{a}, SyncOptions{Direction: DirectionNewer, All: true})
	require.NoError(t, err)
	require.Len(t, all[0].Messages, 1, "all_messages skips the pre-screen")
	assert.Zero(t, all[0].Skipped)
	assert.Len(t, requests, 3)
}

func TestSources_SyncSources_ReadsEveryJoinedSourceWhenGivenNoIDs(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	a := w.mustAddTelegramGroup(t, scope, tgGroup("370", "joined_a", "Joined A"), tgMessage(1, "31", "hi"))
	w.mustAddTelegramGroup(t, scope, tgGroup("371", "left_b", "Left B"), tgMessage(1, "32", "hi"))
	c := w.mustAddTelegramGroup(t, scope, tgGroup("372", "joined_c", "Joined C"), tgMessage(1, "33", "hi"), tgMessage(2, "34", "yo"))

	_, err := w.svc.SyncSources(ctx, scope, nil, SyncOptions{})
	assert.ErrorIs(t, err, ErrNoJoinedSource)

	_, err = w.svc.JoinSources(ctx, scope, []int64{c, a})
	require.NoError(t, err)
	results, err := w.svc.SyncSources(ctx, scope, nil, SyncOptions{})
	require.NoError(t, err)
	got := map[int64]int{}
	for _, r := range results {
		got[r.Source.ID] = r.Read
	}
	assert.Equal(t, map[int64]int{a: 1, c: 2}, got)
}

func TestSources_SyncSource_KeepsTheCursorWhenTheJudgeKeyIsRefused(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	refuse := true
	jev := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if refuse {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = rw.Write([]byte(`{"answers":{"m0":{"type":"noul","noul":0.1},"m1":{"type":"noul","noul":0.1}}}`))
	}))
	t.Cleanup(jev.Close)
	w.svc.cfg.TypeSafeAPIURL = jev.URL
	_, err := w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningCustom, map[string]string{"api_key": "ts-bad", "api_url": jev.URL})
	require.NoError(t, err)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("362", "refused", "Refused"), tgMessage(1, "11", "a"), tgMessage(2, "12", "b"))

	_, err = w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.ErrorIs(t, err, ErrNoJudge)
	acc, err := w.svc.ListAccounts(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "active", acc[0].Status, "a refused judge key is not the account's fault")
	assert.Zero(t, acc[0].ConsecutiveErrors)

	refuse = false
	res, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.NoError(t, err)
	assert.Len(t, res.Messages, 2, "the refused read is read again")
}

func TestSources_SyncSource_ReadsOnePageForwardPerCallUntilCaughtUp(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	id := w.mustAddTelegramGroup(t, scope, tgGroup("370", "busy", "Busy"), tgMessage(1, "11", "a"), tgMessage(2, "12", "b"))
	_, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.NoError(t, err)

	more := make([]connectors.Message, 0, 25)
	for i := int64(3); i <= 27; i++ {
		more = append(more, tgMessage(i, fmt.Sprint(i), "new"))
	}
	w.tg.AddSource(tgGroup("370", "busy", "Busy"), more...)

	for _, want := range []struct {
		first, last string
		count       int
		caughtUp    bool
	}{{"12", "3", 10, false}, {"22", "13", 10, false}, {"27", "23", 5, true}} {
		page, err := w.svc.SyncSource(ctx, scope, id, DirectionNewer, 10)
		require.NoError(t, err)
		require.Len(t, page.Messages, want.count, "one page per call, none skipped")
		assert.Equal(t, want.first, page.Messages[0].MessageID)
		assert.Equal(t, want.last, page.Messages[want.count-1].MessageID)
		require.NotNil(t, page.CaughtUp)
		assert.Equal(t, want.caughtUp, *page.CaughtUp)
	}

	page, err := w.svc.SyncSource(ctx, scope, id, DirectionNewer, 10)
	require.NoError(t, err)
	assert.Empty(t, page.Messages)
}

// acceptingJev serves a judgement model that excludes nothing, so the pre-screen keeps
// every message, and counts its requests.
func acceptingJev(t *testing.T, w *world) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	jev := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		calls.Add(1)
		answers := map[string]any{}
		for qid := range body["state"].(map[string]any)["messages"].(map[string]any) {
			answers[qid] = map[string]any{"type": "noul", "noul": 0.1}
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"answers": answers})
	}))
	t.Cleanup(jev.Close)
	w.svc.cfg.TypeSafeAPIURL = jev.URL
	_, err := w.svc.SaveSetting(context.Background(), w.user, SettingScreening, screeningCustom,
		map[string]string{"api_key": "ts-key", "api_url": jev.URL})
	require.NoError(t, err)
	return &calls
}

func messageBytes(t *testing.T, msgs []MessageView) int {
	t.Helper()
	raw, err := json.Marshal(msgs)
	require.NoError(t, err)
	return len(raw)
}

func TestSources_SyncSources_BoundedReadStopsAtItsBudgetAndTheNextCallContinues(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	calls := acceptingJev(t, w)
	msgs := make([]connectors.Message, 0, 50)
	for i := int64(1); i <= 50; i++ {
		msgs = append(msgs, tgMessage(i, fmt.Sprint(100+i), strings.Repeat("buy ", 150)))
	}
	id := w.mustAddTelegramGroup(t, scope, tgGroup("390", "bounded", "Bounded"), msgs...)

	opt := SyncOptions{Limit: 50, MaxBytes: 4096}
	first, err := w.svc.SyncSources(ctx, scope, []int64{id}, opt)
	require.NoError(t, err)
	res := first[0]
	require.NotEmpty(t, res.Messages)
	assert.LessOrEqual(t, messageBytes(t, res.Messages), 3584, "the messages stay inside the budget less the source overhead")
	assert.Equal(t, res.Read, len(res.Messages), "every message read is returned: nothing is skipped")
	assert.Equal(t, 50-res.Read, res.Pending)
	assert.Equal(t, "50", res.Messages[0].MessageID, "a back-read starts from the message nearest the cursor")
	last := 51 - len(res.Messages)
	assert.Equal(t, fmt.Sprint(last), res.Messages[len(res.Messages)-1].MessageID)
	assert.Equal(t, fmt.Sprint(last-1), res.NextFrom)
	assert.Len(t, []rune(res.Messages[0].Text), 401, "a bounded read returns the opening lines the pre-screen judged")
	assert.False(t, res.Source.HistoryExhausted)
	assert.EqualValues(t, 1, calls.Load(), "only the batch the read consumed was judged")

	second, err := w.svc.SyncSources(ctx, scope, []int64{id}, opt)
	require.NoError(t, err)
	require.NotEmpty(t, second[0].Messages)
	assert.Equal(t, res.NextFrom, second[0].Messages[0].MessageID, "the next read continues with the first pending message")
	assert.Equal(t, 50-res.Read-second[0].Read, second[0].Pending)

	rest, err := w.svc.SyncSources(ctx, scope, []int64{id}, SyncOptions{Limit: 50})
	require.NoError(t, err)
	assert.Zero(t, rest[0].Pending)
	assert.Empty(t, rest[0].NextFrom)
	assert.Equal(t, 50, res.Read+second[0].Read+rest[0].Read, "every message is read exactly once")
	assert.Equal(t, fmt.Sprint(second[0].NextFrom), rest[0].Messages[0].MessageID)
	assert.True(t, rest[0].Source.HistoryExhausted)
	assert.Len(t, []rune(rest[0].Messages[0].Text), 401, "a page read keeps the opening 400 characters")
	assert.EqualValues(t, 3, calls.Load())
}

func TestSources_SyncSources_HandsAnUnusedShareToTheNextSource(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	acceptingJev(t, w)
	a := w.mustAddTelegramGroup(t, scope, tgGroup("391", "quiet", "Quiet"), tgMessage(1, "11", "hi"))
	msgs := make([]connectors.Message, 0, 50)
	for i := int64(1); i <= 50; i++ {
		msgs = append(msgs, tgMessage(i, fmt.Sprint(200+i), strings.Repeat("buy ", 150)))
	}
	b := w.mustAddTelegramGroup(t, scope, tgGroup("392", "busy", "Busy"), msgs...)

	results, err := w.svc.SyncSources(ctx, scope, []int64{a, b}, SyncOptions{Limit: 50, MaxBytes: 8192})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Len(t, results[0].Messages, 1)
	assert.Zero(t, results[0].Pending)
	used := messageBytes(t, results[1].Messages)
	assert.Greater(t, used, 3584, "the second source takes what the first left of the budget")
	assert.LessOrEqual(t, used, 7168)
	assert.Positive(t, results[1].Pending)
}

func TestSources_SyncSource_BoundedForwardReadStopsBeforeThePendingMessages(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	acceptingJev(t, w)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("393", "forward", "Forward"), tgMessage(1, "11", "a"), tgMessage(2, "12", "b"))
	_, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.NoError(t, err)
	more := make([]connectors.Message, 0, 20)
	for i := int64(3); i <= 22; i++ {
		more = append(more, tgMessage(i, fmt.Sprint(i), strings.Repeat("buy ", 150)))
	}
	w.tg.AddSource(tgGroup("393", "forward", "Forward"), more...)

	opt := SyncOptions{Direction: DirectionNewer, Limit: 30, MaxBytes: 4096}
	results, err := w.svc.SyncSources(ctx, scope, []int64{id}, opt)
	require.NoError(t, err)
	res := results[0]
	n := len(res.Messages)
	require.Positive(t, n)
	require.NotNil(t, res.CaughtUp)
	assert.False(t, *res.CaughtUp, "a read that stopped before pending messages has not caught up")
	assert.Equal(t, "3", res.Messages[n-1].MessageID, "a forward read consumes from the cursor upwards")
	assert.Equal(t, fmt.Sprint(2+n), res.Messages[0].MessageID)
	assert.Equal(t, fmt.Sprint(3+n), res.NextFrom)
	assert.Equal(t, 20-n, res.Pending)

	results, err = w.svc.SyncSources(ctx, scope, []int64{id}, opt)
	require.NoError(t, err)
	next := results[0]
	require.NotEmpty(t, next.Messages)
	assert.Equal(t, res.NextFrom, next.Messages[len(next.Messages)-1].MessageID, "the next read continues with the first pending message")
}

func TestSources_ReadMessage_ReturnsTheFullTextWithoutMovingTheCursor(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "0"})
	acceptingJev(t, w)
	text := strings.Repeat("buy ", 300)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("394", "verbatim", "Verbatim"),
		tgMessage(1, "11", "one"), tgMessage(2, "12", "two"), tgMessage(3, "13", "three"), tgMessage(4, "14", text))

	page, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 2)
	require.NoError(t, err)
	require.Len(t, page.Messages, 2)
	assert.Equal(t, "4", page.Messages[0].MessageID)
	assert.Len(t, []rune(page.Messages[0].Text), 401, "the page carries the opening lines")
	assert.False(t, page.Source.HistoryExhausted)
	read := page.Source.MessagesRead

	full, err := w.svc.ReadMessage(ctx, scope, id, "4")
	require.NoError(t, err)
	assert.Equal(t, text, full.Text)
	assert.Equal(t, "https://t.me/c/1/4", full.Link)

	_, err = w.svc.ReadMessage(ctx, scope, id, "9")
	assert.ErrorIs(t, err, ErrMessageNotFound)

	again, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 2)
	require.NoError(t, err)
	require.Len(t, again.Messages, 2)
	assert.Equal(t, "2", again.Messages[0].MessageID, "the next page continues where the first stopped")
	assert.Equal(t, "1", again.Messages[1].MessageID)
	assert.Equal(t, read+2, again.Source.MessagesRead, "fetching one message is not a page read")
}

func TestSources_SyncSources_WaitsBetweenTwoSourcesOfOneAccount(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	w.savePace(t, map[string]string{"read_interval_seconds": "1"})
	a := w.mustAddTelegramGroup(t, scope, tgGroup("380", "gap_a", "A"), tgMessage(1, "11", "a"))
	b := w.mustAddTelegramGroup(t, scope, tgGroup("381", "gap_b", "B"), tgMessage(1, "21", "b"))

	_, err := w.svc.SyncSources(ctx, scope, []int64{a}, SyncOptions{})
	require.NoError(t, err)
	start := time.Now()
	_, err = w.svc.SyncSources(ctx, scope, []int64{a}, SyncOptions{Direction: DirectionNewer})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 900*time.Millisecond, "the same source is not held")
	_, err = w.svc.SyncSources(ctx, scope, []int64{b}, SyncOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond, "another source waits the read interval")
}

func TestSources_UpdateSource_ChangesTheTitleAndNoteButNotTheLink(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("910", "renamed", "Renamed"))

	title, note := " VIP players ", "active after 8pm"
	src, err := w.svc.UpdateSource(ctx, w.user, id, SourceUpdate{Title: &title, Note: &note})
	require.NoError(t, err)
	assert.Equal(t, "VIP players", src.Title)
	assert.Equal(t, "active after 8pm", src.Note)
	assert.Equal(t, "https://t.me/renamed", src.Link)

	empty := ""
	src, err = w.svc.UpdateSource(ctx, w.user, id, SourceUpdate{Note: &empty})
	require.NoError(t, err)
	assert.Equal(t, "VIP players", src.Title)
	assert.Empty(t, src.Note)

	_, err = w.svc.UpdateSource(ctx, seedUser(t, w.db, "b@example.test"), id, SourceUpdate{Note: &note})
	assert.ErrorIs(t, err, ErrSourceNotFound)
}
