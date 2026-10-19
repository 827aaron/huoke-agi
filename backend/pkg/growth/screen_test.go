package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/screening"
)

// judgeStore is a store with no saved settings, so the server's config decides the Jev
// key; a score write is recorded and otherwise refused, as screening a failed batch must not score.
type judgeStore struct {
	database.Querier
	scores atomic.Int32
}

func (s *judgeStore) GetUserGrowthSettings(ctx context.Context, _ int64) ([]database.GrowthSetting, error) {
	return nil, ctx.Err()
}

func (s *judgeStore) UpsertMessageScore(ctx context.Context, _ database.UpsertMessageScoreParams) error {
	s.scores.Add(1)
	return ctx.Err()
}

func TestScreen_ScreenerFor_RefusesWithoutAJevKey(t *testing.T) {
	svc := NewService(&judgeStore{}, &config.Config{})
	scope := Scope{UserID: 1, FlowID: 10}

	_, err := svc.screenerFor(context.Background(), scope)
	assert.ErrorIs(t, err, ErrNoJudge)
	assert.ErrorIs(t, svc.JudgeReady(context.Background(), 1), ErrNoJudge)
	assert.Contains(t, svc.judgeWarning(context.Background(), 1, 0), "no judgement model (Jev) is configured")

	assert.NoError(t, NewService(&judgeStore{}, &config.Config{TypeSafeAPIKey: "k"}).JudgeReady(context.Background(), 1))
}

// failingJev answers the first `fail` batches with 500 and every later one with no answers.
func failingJev(t *testing.T, fail int) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if int(calls.Add(1)) <= fail {
			http.Error(rw, "upstream down", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"answers": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestScreen_Screen_CountsTheBatchesJevFailedOnAndKeepsThemWhole(t *testing.T) {
	store := &judgeStore{}
	svc := NewService(store, &config.Config{TypeSafeAPIKey: "k"})
	sc := &screener{
		client:  &screening.Client{URL: failingJev(t, 1).URL, Key: "k", Retries: 0},
		profile: ProfileView{ID: 4, Profile: Profile{Scoring: &Scoring{Dimensions: defaultDimensions(3, 3, 2, 3), Qualified: 7, Watchlist: 4}}},
		now:     time.Now,
	}
	msgs := make([]MessageView, 0, screenBatch+1)
	for i := 0; i < screenBatch+1; i++ {
		msgs = append(msgs, MessageView{MessageID: fmt.Sprint(i), SenderID: "s", Text: "hello"})
	}

	kept, err := svc.screen(context.Background(), 1, sc, 7, "telegram", "Fans", msgs)
	require.NoError(t, err)
	assert.Len(t, kept, screenBatch+1, "a failed batch and an unjudged one are both kept whole")
	assert.Equal(t, 1, sc.fallbacks, "only the batch that failed counts, not the one Jev answered")
	assert.Equal(t, int32(0), store.scores.Load())
}

func TestScreen_Screen_FailsTheReadWhenTheKeyIsRefused(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		http.Error(rw, "bad key", http.StatusUnauthorized)
	}))
	t.Cleanup(refused.Close)
	svc := NewService(&judgeStore{}, &config.Config{TypeSafeAPIKey: "k"})
	sc := &screener{client: &screening.Client{URL: refused.URL, Key: "k"}, now: time.Now}

	_, err := svc.screen(context.Background(), 1, sc, 7, "telegram", "Fans", []MessageView{{MessageID: "1", Text: "hi"}})
	assert.ErrorIs(t, err, ErrNoJudge)
	assert.Equal(t, 0, sc.fallbacks, "a refused key blocks the read; it is not a fallback")
}

func TestScreen_PrescreenFallbacks_TalliesPerRoundAndSumsOutsideOne(t *testing.T) {
	svc := NewService(&judgeStore{}, &config.Config{})
	task1, task2 := int64(100), int64(200)
	svc.notePrescreenFallbacks(Scope{UserID: 1, FlowID: 10, TaskID: &task1}, 2)
	svc.notePrescreenFallbacks(Scope{UserID: 1, FlowID: 10, TaskID: &task1}, 1)
	svc.notePrescreenFallbacks(Scope{UserID: 1, FlowID: 11, TaskID: &task2}, 4)
	svc.notePrescreenFallbacks(Scope{UserID: 2, FlowID: 10}, 8)
	svc.notePrescreenFallbacks(Scope{UserID: 1, FlowID: 10}, 0)

	assert.Equal(t, 3, svc.PrescreenFallbacks(Scope{UserID: 1, FlowID: 10, TaskID: &task1}))
	assert.Equal(t, 0, svc.PrescreenFallbacks(Scope{UserID: 1, FlowID: 10, TaskID: &task2}), "another round of the same owner")
	assert.Equal(t, 3, svc.PrescreenFallbacks(Scope{UserID: 1, FlowID: 10}), "the flow's rounds")
	assert.Equal(t, 7, svc.PrescreenFallbacks(Scope{UserID: 1}), "all the owner's rounds")
	assert.Equal(t, 0, svc.PrescreenFallbacks(Scope{UserID: 3}))
	assert.Equal(t, "", prescreenFallbackWarning(0, "this round"))
	assert.Equal(t, "the pre-screen (Jev) failed on 3 batch(es) this round: those messages came back unscreened and unscored, "+
		"so the two-stage judgement did not save tokens on them; check the Jev key and model on the settings page",
		prescreenFallbackWarning(3, "this round"))
}

func TestScreen_ScreenMessages_NamesEachSender(t *testing.T) {
	got := screenMessages([]MessageView{
		{MessageID: "3", SenderID: "11", SenderUsername: "ann", SenderName: "Ann", Text: "looking\nfor a guild"},
		{MessageID: "2", SenderID: "12", SenderIsBot: true, Text: "join now"},
	})
	assert.Equal(t, []screening.Message{
		{ID: "3", Sender: "@ann Ann", Text: "looking for a guild"},
		{ID: "2", Sender: "12 (bot)", Text: "join now"},
	}, got)
}

func TestScreen_Batch_AsksTheProfilesDimensionsBesideTheComplianceAndUnwantedQuestions(t *testing.T) {
	p := Profile{
		Pitch:    "A fair MMORPG with no pay-to-win",
		Unwanted: []string{"account sellers", "people in Singapore"},
	}
	p.Scoring = &Scoring{
		Dimensions: []ScoringDimension{
			{Key: "player", Name: "Audience", Description: "The sender uses or follows this kind of product or service themselves.", Points: 3},
			{Key: "agent_team", Name: "Runs a team", Description: "The sender says they lead a team of agents.", Points: 5},
		},
		Qualified: 7, Watchlist: 4,
	}
	sc := &screener{profile: ProfileView{ID: 4, Profile: p}, task: "find 10 agents"}

	got := sc.batch("telegram", "Agents MY", []screening.Message{{ID: "1", Text: "I run a team"}})
	assert.Equal(t, "telegram", got.Platform)
	assert.Equal(t, "Agents MY", got.Source)
	assert.Equal(t, "find 10 agents", got.Task)
	assert.Equal(t, []string{"a stated age under 18", "an explicit request for help to stop gambling, or a stated self-exclusion"}, got.Excluded)
	assert.Equal(t, []string{"account sellers", "people in Singapore"}, got.Unwanted)
	assert.Equal(t, []screening.Message{{ID: "1", Text: "I run a team"}}, got.Messages)
	assert.NotContains(t, got.Business, "account sellers", "the unwanted list travels as its own state")
	assert.NotContains(t, got.Business, "under 18")
	assert.Contains(t, got.Business, "Offer: A fair MMORPG with no pay-to-win")
	require.Len(t, got.Signals, 2)
	assert.Equal(t, screening.Signals[0], got.Signals[0], "a built-in dimension keeps its tuned question")
	assert.Equal(t, "agent_team", got.Signals[1].Key)
	assert.Contains(t, fmt.Sprintf(got.Signals[1].Question, "m0"),
		"`messages.m0` show that its sender fits \"Runs a team\", meaning: The sender says they lead a team of agents?")
}

func TestScreen_Batch_WithoutAScoringAsksTheDefaultFourQuestions(t *testing.T) {
	sc := &screener{profile: ProfileView{Profile: Profile{Unwanted: []string{"account sellers"}}}}
	got := sc.batch("telegram", "Fans", nil)
	assert.Equal(t, screening.Signals, got.Signals)
	assert.Equal(t, []string{"account sellers"}, got.Unwanted)
}

func TestScreen_ScoreMessage_TakesTheUnwantedPenaltyOffAndNeverBelowZero(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	sc := Scoring{
		Dimensions: []ScoringDimension{{Key: "player", Points: 3}, {Key: "agent_team", Points: 5}, {Key: "paying", Points: 2}},
		Recent:     &RecentDimension{Points: 2},
		Qualified:  7, Watchlist: 4,
	}
	tests := []struct {
		name  string
		j     screening.Judgement
		at    time.Time
		score int
		text  string
	}{
		{"dimensions add in the profile's order", screening.Judgement{Unwanted: -1, Signals: map[string]float64{"paying": 0.9, "agent_team": 0.6, "player": 0.4}},
			now.Add(-time.Hour), 9, "agent_team+5, paying+2, 7d+2 = 9"},
		{"an unwanted hit costs the penalty", screening.Judgement{Unwanted: 0.8, Signals: map[string]float64{"player": 0.9, "agent_team": 0.9}},
			now.Add(-20 * 24 * time.Hour), 6, "player+3, agent_team+5, 30d+1, unwanted-3 = 6"},
		{"the penalty alone stops at zero", screening.Judgement{Unwanted: 0.9, Signals: map[string]float64{"paying": 0.9}},
			time.Time{}, 0, "paying+2, unwanted-3 = 0"},
		{"an unanswered unwanted question costs nothing", screening.Judgement{Unwanted: -1, Signals: map[string]float64{"player": 0.9}},
			time.Time{}, 3, "player+3 = 3"},
		{"nothing fired", screening.Judgement{Unwanted: 0.2, Signals: map[string]float64{"player": 0.1}}, time.Time{}, 0, "no signal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, text := scoreMessage(sc, 3, tt.j, tt.at, now)
			assert.Equal(t, tt.score, score)
			assert.Equal(t, tt.text, text)
		})
	}
}

func TestScreen_ScoreParts_ReadsThePenaltyBackAsANegativePart(t *testing.T) {
	assert.Equal(t, []ScorePart{{"player", 3}, {"agent_team", 5}, {"active", 1}, {"unwanted", -3}},
		scoreParts("player+3, agent_team+5, 30d+1, unwanted-3 = 6"))
	assert.Equal(t, []ScorePart{}, scoreParts("no signal"))
}
