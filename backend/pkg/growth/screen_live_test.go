//go:build postgres

package growth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalJev answers every question by keywords of the message: the exclusion question
// yes on "lol", player on "play", pain on "bored", paying on "top up", match on "RPG",
// unwanted on "seller", and an owner's dimension "agent_team" on "my team". It records
// the question ids of the last request.
func signalJev(t *testing.T, asked *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			State struct {
				Messages map[string]struct {
					Text string `json:"text"`
				} `json:"messages"`
			} `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		keyword := map[string]string{
			"player": "play", "pain": "bored", "paying": "top up", "match": "RPG", "unwanted": "seller", "agent_team": "my team",
		}
		answers := map[string]any{}
		ids := make([]string, 0, len(body.Questions))
		for qid := range body.Questions {
			ids = append(ids, qid)
			key, signal, _ := strings.Cut(qid, "_")
			text := body.State.Messages[key].Text
			yes := strings.Contains(text, "lol")
			if signal != "" {
				yes = strings.Contains(text, keyword[signal])
			}
			p := 0.1
			if yes {
				p = 0.9
			}
			answers[qid] = map[string]any{"type": "noul", "noul": p}
		}
		if asked != nil {
			sort.Strings(ids)
			mu.Lock()
			*asked = ids
			mu.Unlock()
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"answers": answers})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestScreen_SyncSources_ScoresEveryKeptMessageAndLeadsTakeThatScore(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	p := gameProfile()
	p.Scoring = &Scoring{Dimensions: defaultDimensions(3, 3, 2, 3), Recent: &RecentDimension{Points: 2}, Qualified: 8, Watchlist: 5}
	_, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: p})
	require.NoError(t, err)
	w.savePace(t, map[string]string{"read_interval_seconds": "0", "first_sync_days": "90"})
	var asked []string
	w.svc.cfg.TypeSafeAPIURL = signalJev(t, &asked).URL

	at := func(m connectors.Message, age time.Duration) connectors.Message {
		m.Time = time.Now().UTC().Add(-age).Truncate(time.Second)
		return m
	}
	src := w.mustAddTelegramGroup(t, scope, tgGroup("380", "rpg_lounge", "RPG lounge"),
		at(tgMessage(1, "31", "bored of my RPG, I top up every week and play daily"), time.Hour),
		at(tgMessage(2, "32", "I play RPG games"), 20*24*time.Hour),
		at(tgMessage(3, "33", "lol I play RPG all day"), time.Hour),
		at(tgMessage(4, "34", "I play sometimes"), 60*24*time.Hour),
	)

	results, err := w.svc.SyncSources(ctx, scope, []int64{src}, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := map[string]MessageView{}
	for _, m := range results[0].Messages {
		got[m.MessageID] = m
	}
	require.Len(t, got, 2, "the exclusion question drops 3, the watchlist line drops 4")
	assert.Equal(t, []string{
		"m0", "m0_match", "m0_pain", "m0_paying", "m0_player", "m0_unwanted",
		"m1", "m1_match", "m1_pain", "m1_paying", "m1_player", "m1_unwanted",
		"m2", "m2_match", "m2_pain", "m2_paying", "m2_player", "m2_unwanted",
		"m3", "m3_match", "m3_pain", "m3_paying", "m3_player", "m3_unwanted",
	}, asked, "the default dimensions ask the four built-in questions; the profile's unwanted list adds one")
	assert.Equal(t, 13, got["1"].Score)
	assert.Equal(t, TierQualified, got["1"].Tier)
	assert.Equal(t, "player+3, pain+3, paying+2, match+3, 7d+2 = 13", got["1"].Signals)
	assert.Equal(t, 7, got["2"].Score)
	assert.Equal(t, TierWatchlist, got["2"].Tier, "7 is below this profile's qualified line of 8")
	assert.Equal(t, "player+3, match+3, 30d+1 = 7", got["2"].Signals)
	assert.Equal(t, 2, results[0].Skipped)

	lead := qualified("31", src, "1")
	lead.Score = 2
	watch := qualified("32", src, "2")
	watch.Score, watch.Tier = 0, ""
	saved, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{lead, watch})
	require.NoError(t, err)
	require.Len(t, saved, 2)
	assert.Equal(t, LeadStatusNew, saved[0].Status)
	assert.Equal(t, 13, saved[0].Score)
	assert.Equal(t, TierQualified, saved[0].Tier)
	assert.Contains(t, saved[0].Reason, "score 13 is the judgement model's for this evidence, not 2")
	assert.Equal(t, 7, saved[1].Score)
	assert.Equal(t, TierWatchlist, saved[1].Tier)
}

func TestScreen_SyncSources_JudgesAgainstTheFlowsInstruction(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.savePace(t, map[string]string{"read_interval_seconds": "0", "first_sync_days": "90"})
	var (
		mu    sync.Mutex
		tasks []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			State struct {
				Task string `json:"task"`
			} `json:"state"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		tasks = append(tasks, body.State.Task)
		mu.Unlock()
		_ = json.NewEncoder(rw).Encode(map[string]any{"answers": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	w.svc.cfg.TypeSafeAPIURL = srv.URL

	scope := w.seedTask(t)
	_, err := w.db.Exec(`UPDATE tasks SET input = $1 WHERE id = $2`, "  find 10 Malaysian\n agents  ", *scope.TaskID)
	require.NoError(t, err)
	first := w.mustAddTelegramGroup(t, scope, tgGroup("950", "agents_my", "Agents MY"), tgMessage(1, "51", "who is hiring agents"))
	_, err = w.svc.SyncSources(ctx, scope, []int64{first}, SyncOptions{})
	require.NoError(t, err)

	next := w.seedTaskIn(t, scope.FlowID)
	_, err = w.db.Exec(`UPDATE tasks SET input = 'keep going' WHERE id = $1`, *next.TaskID)
	require.NoError(t, err)
	second := w.mustAddTelegramGroup(t, next, tgGroup("951", "agents_kl", "Agents KL"), tgMessage(1, "52", "I run a team"))
	_, err = w.svc.SyncSources(ctx, next, []int64{second}, SyncOptions{})
	require.NoError(t, err)

	other := w.seedTask(t)
	third := w.mustAddTelegramGroup(t, other, tgGroup("952", "players_my", "Players MY"), tgMessage(1, "53", "I play slots"))
	_, err = w.svc.SyncSources(ctx, other, []int64{third}, SyncOptions{})
	require.NoError(t, err)

	_, err = w.svc.UpsertLeads(ctx, next, []LeadInput{qualified("52", second, "1")})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"find 10 Malaysian agents",
		"find 10 Malaysian agents\nLatest instruction: keep going",
		"find 50",
		"find 10 Malaysian agents\nLatest instruction: keep going",
	}, tasks, "each flow is judged against its own instruction, and a follow-up keeps the first one")
}

func TestScreen_SyncSources_AsksTheProfilesOwnDimensionsAndFlagsTheUnwanted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	p := gameProfile()
	p.Unwanted = []string{"account sellers"}
	p.UnwantedPenalty = 4
	p.Scoring = &Scoring{
		Dimensions: []ScoringDimension{
			{Key: "player", Name: "Audience", Description: "The sender uses or follows this kind of product or service themselves.", Points: 3},
			{Name: "Agent team", Description: "The sender says they lead a team of agents.", Points: 5},
		},
		Qualified: 7, Watchlist: 1,
	}
	_, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: p})
	require.NoError(t, err)
	w.savePace(t, map[string]string{"read_interval_seconds": "0", "first_sync_days": "90"})
	var asked []string
	w.svc.cfg.TypeSafeAPIURL = signalJev(t, &asked).URL

	src := w.mustAddTelegramGroup(t, scope, tgGroup("381", "agents_my", "Agents MY"),
		tgMessage(1, "41", "I play daily and my team has 20 agents"),
		tgMessage(2, "42", "my team sells accounts, I am a seller"),
		tgMessage(3, "43", "I play now and then, bored of it, I top up a lot"),
		tgMessage(4, "44", "seller here, cheap accounts"),
		tgMessage(5, "42", "I play too and my team is growing"),
	)
	results, err := w.svc.SyncSources(ctx, scope, []int64{src}, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	got := map[string]MessageView{}
	for _, m := range results[0].Messages {
		got[m.MessageID] = m
	}
	assert.Equal(t, []string{
		"m0", "m0_agent_team", "m0_player", "m0_unwanted",
		"m1", "m1_agent_team", "m1_player", "m1_unwanted",
		"m2", "m2_agent_team", "m2_player", "m2_unwanted",
		"m3", "m3_agent_team", "m3_player", "m3_unwanted",
		"m4", "m4_agent_team", "m4_player", "m4_unwanted",
	}, asked, "only the profile's dimensions are asked, under their keys")
	require.Len(t, got, 4, "the penalty alone drops 4 below the watchlist line")
	assert.Equal(t, 8, got["1"].Score)
	assert.Equal(t, "player+3, agent_team+5 = 8", got["1"].Signals)
	assert.Equal(t, TierQualified, got["1"].Tier)
	assert.Equal(t, 1, got["2"].Score, "an unwanted hit costs the profile's penalty and is kept")
	assert.Equal(t, "agent_team+5, unwanted-4 = 1", got["2"].Signals)
	assert.Equal(t, TierWatchlist, got["2"].Tier)
	assert.Equal(t, 3, got["3"].Score, "pain and paying are not this profile's dimensions")
	assert.Equal(t, "player+3 = 3", got["3"].Signals)
	assert.Equal(t, 8, got["5"].Score)

	seller := qualified("42", src, "2")
	seller.Evidence = append(seller.Evidence, qualified("42", src, "5").Evidence...)
	saved, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{qualified("41", src, "1"), seller})
	require.NoError(t, err)
	require.Len(t, saved, 2)
	assert.Equal(t, LeadStatusNew, saved[0].Status)
	assert.Equal(t, LeadStatusNew, saved[1].Status, "an unwanted hit never drops a lead")

	leads, err := w.svc.ListLeads(ctx, w.user, LeadFilter{})
	require.NoError(t, err)
	byUser := map[string]LeadView{}
	for _, l := range leads {
		byUser[l.PlatformUserID] = l
	}
	assert.Equal(t, []string{}, byUser["41"].Flags)
	assert.Equal(t, []string{"player", "agent_team"}, byUser["41"].Signals)
	assert.Equal(t, []string{LeadFlagUnwanted}, byUser["42"].Flags, "flagged by any judged message, scored by the best")
	assert.Equal(t, 8, byUser["42"].Score)
	assert.Equal(t, []string{"player", "agent_team"}, byUser["42"].Signals)
	flagged := true
	only, err := w.svc.ListLeads(ctx, w.user, LeadFilter{Flagged: &flagged})
	require.NoError(t, err)
	require.Len(t, only, 1)
	assert.Equal(t, "42", only[0].PlatformUserID)

	_, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{qualified("42", src, "5")})
	require.NoError(t, err)
	again, err := w.svc.ListLeads(ctx, w.user, LeadFilter{Flagged: &flagged})
	require.NoError(t, err)
	require.Len(t, again, 1, "a later clean message keeps the flag")
}
