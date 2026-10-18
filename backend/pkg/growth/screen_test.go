package growth

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pentagi/pkg/growth/screening"
)

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
