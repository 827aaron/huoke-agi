package growth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPace_ParseTelegramPace_ReadsTheFieldsAndDefaultsTheRest(t *testing.T) {
	p, err := parseTelegramPace(map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 0, 1, 1, 2, 2, 3, 3, 3, 3, 3, 3, 3}, p.Ramp)
	assert.Equal(t, 5, p.PerDay)
	assert.Equal(t, 3*time.Minute, p.IntervalMin)
	assert.Equal(t, 10*time.Minute, p.IntervalMax)
	assert.Equal(t, 2, p.PrivatePerDay)
	assert.Equal(t, 5*time.Second, p.ReadIntervalMin)
	assert.Equal(t, 15*time.Second, p.ReadIntervalMax)
	assert.Equal(t, 450, p.WarnAt)
	assert.Equal(t, 7, p.FirstSyncDays)
	assert.Equal(t, 300, p.FirstSyncMessages)
	assert.True(t, p.Reminders)

	p, err = parseTelegramPace(map[string]string{
		"join_ramp": " 1, 2 ", "joins_per_day": "8", "join_interval_minutes": "4",
		"private_joins_per_day": "0", "read_interval_seconds": "2-4", "joined_groups_warning": "400", "first_sync_days": "3", "first_sync_messages": "50",
		"risk_reminders": "OFF",
	})
	require.NoError(t, err)
	assert.Equal(t, TelegramPace{
		Ramp: []int{1, 2}, PerDay: 8, IntervalMin: 4 * time.Minute, IntervalMax: 4 * time.Minute,
		PrivatePerDay: 0, ReadIntervalMin: 2 * time.Second, ReadIntervalMax: 4 * time.Second, WarnAt: 400, FirstSyncDays: 3, FirstSyncMessages: 50,
	}, p)

	for _, bad := range []map[string]string{
		{"join_ramp": "1,two"},
		{"joins_per_day": "-1"},
		{"join_interval_minutes": "10-3"},
		{"join_interval_minutes": "soon"},
		{"read_interval_seconds": "5-"},
		{"risk_reminders": "maybe"},
		{"first_sync_messages": "1.5"},
	} {
		_, err := parseTelegramPace(bad)
		assert.ErrorIs(t, err, ErrInvalidSetting, "%v", bad)
	}
}

func TestPace_Advice_NamesEverySuggestionAJoinGoesPast(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	p := defaultTelegramPace

	tests := []struct {
		name    string
		load    joinLoad
		private bool
		want    []string
	}{
		{"a new account is warming up", joinLoad{signedInAt: now.Add(-time.Hour)}, false, []string{AdviceWarmingUp}},
		{"the fourth day has room for one join", joinLoad{signedInAt: now.Add(-3*day - time.Hour)}, false, nil},
		{"the second join of the fourth day",
			joinLoad{signedInAt: now.Add(-3*day - time.Hour), joins: 1}, false, []string{AdviceDailyJoins}},
		{"a join inside the interval",
			joinLoad{signedInAt: now.Add(-30 * day), joins: 1, nextJoinAt: now.Add(time.Minute)}, false, []string{AdviceJoinInterval}},
		{"after the ramp the daily count applies",
			joinLoad{signedInAt: now.Add(-30 * day), joins: 5}, false, []string{AdviceDailyJoins}},
		{"invite links have their own count",
			joinLoad{signedInAt: now.Add(-30 * day), joins: 2, private: 2}, true, []string{AdvicePrivateJoins}},
		{"a public group is not counted with invites",
			joinLoad{signedInAt: now.Add(-30 * day), joins: 2, private: 2}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.advice(now, tt.load, tt.private))
		})
	}

	p.Reminders = false
	assert.Empty(t, p.advice(now, joinLoad{signedInAt: now}, true), "reminders off")
}

func TestPace_Gap_StaysInsideTheInterval(t *testing.T) {
	p := TelegramPace{IntervalMin: 3 * time.Minute, IntervalMax: 10 * time.Minute}
	seen := map[bool]bool{}
	for range 200 {
		g := p.gap()
		require.GreaterOrEqual(t, g, 3*time.Minute)
		require.LessOrEqual(t, g, 10*time.Minute)
		seen[g < 6*time.Minute] = true
	}
	assert.Len(t, seen, 2, "the gap varies across the interval")
	assert.Equal(t, 4*time.Minute, TelegramPace{IntervalMin: 4 * time.Minute, IntervalMax: 4 * time.Minute}.gap())
}
