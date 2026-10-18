//go:build postgres

package growth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalJev answers every question by keywords of the message: the exclusion question
// yes on "lol", player on "play", pain on "bored", paying on "top up", match on "RPG".
func signalJev(t *testing.T) *httptest.Server {
	t.Helper()
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
		keyword := map[string]string{"player": "play", "pain": "bored", "paying": "top up", "match": "RPG"}
		answers := map[string]any{}
		for qid := range body.Questions {
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
	w.svc.cfg.TypeSafeAPIURL = signalJev(t).URL

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
