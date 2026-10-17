package growth

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustSchedule(t *testing.T, in ScheduleInput) schedule {
	t.Helper()
	sc, err := in.normalize()
	require.NoError(t, err)
	return sc
}

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func TestSchedules_Next_FindsTheNextStartAndItsEndInTheCustomersZone(t *testing.T) {
	bkk := zone(t, "Asia/Bangkok")
	cases := []struct {
		name      string
		in        ScheduleInput
		after     time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "daily later the same day",
			in:        ScheduleInput{Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 19, 1, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 18, 19, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC),
		},
		{
			name:      "daily at the start itself moves to the next day",
			in:        ScheduleInput{Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 19, 2, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 19, 19, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC),
		},
		{
			name:      "an end before the start is on the next day",
			in:        ScheduleInput{Days: ScheduleDaily, StartTime: "22:00", EndTime: "06:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 19, 12, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC),
		},
		{
			name:      "weekdays skip the weekend",
			in:        ScheduleInput{Days: ScheduleWeekdays, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 23, 3, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 25, 19, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC),
		},
		{
			name:      "weekly runs on the chosen days, Sunday being 0",
			in:        ScheduleInput{Days: ScheduleWeekly, Weekdays: []int{3, 0, 3}, StartTime: "09:30", EndTime: "18:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 19, 12, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 21, 2, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC),
		},
		{
			name:      "the wall clock holds across the end of daylight saving",
			in:        ScheduleInput{Days: ScheduleDaily, StartTime: "08:00", EndTime: "12:00", Timezone: "America/Los_Angeles"},
			after:     time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 11, 1, 16, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC),
		},
		{
			name:      "once on its date",
			in:        ScheduleInput{Days: ScheduleOnce, Date: "2026-10-25", StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"},
			after:     time.Date(2026, 10, 19, 12, 0, 0, 0, bkk),
			wantStart: time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 24, 23, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := mustSchedule(t, tc.in)
			start, ok := sc.next(tc.after)
			require.True(t, ok)
			assert.Equal(t, tc.wantStart, start.UTC())
			_, end := sc.window(start)
			assert.Equal(t, tc.wantEnd, end.UTC())
		})
	}
}

func TestSchedules_Next_HasNothingAheadOfAOncePast(t *testing.T) {
	sc := mustSchedule(t, ScheduleInput{Days: ScheduleOnce, Date: "2026-10-25", StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"})
	_, ok := sc.next(time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC))
	assert.False(t, ok)
}

func TestSchedules_Normalize_RefusesWhatCannotBeScheduled(t *testing.T) {
	ok := ScheduleInput{Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"}
	cases := []struct {
		name   string
		change func(*ScheduleInput)
		want   string
	}{
		{"unknown days", func(in *ScheduleInput) { in.Days = "hourly" }, "days must be one of"},
		{"weekly without days", func(in *ScheduleInput) { in.Days = ScheduleWeekly }, "weekly needs weekdays"},
		{"weekday out of range", func(in *ScheduleInput) { in.Days, in.Weekdays = ScheduleWeekly, []int{1, 7} }, "weekly needs weekdays"},
		{"once without a date", func(in *ScheduleInput) { in.Days = ScheduleOnce }, "once needs date"},
		{"a time without its leading zero", func(in *ScheduleInput) { in.StartTime = "2:00" }, `start_time must be HH:MM, got "2:00"`},
		{"an hour past 23", func(in *ScheduleInput) { in.EndTime = "24:00" }, "end_time must be HH:MM"},
		{"the same start and end", func(in *ScheduleInput) { in.EndTime = "02:00" }, "end_time must differ"},
		{"an unknown zone", func(in *ScheduleInput) { in.Timezone = "Mars/Olympus" }, `unknown timezone "Mars/Olympus"`},
		{"the server's own zone", func(in *ScheduleInput) { in.Timezone = "Local" }, `unknown timezone "Local"`},
		{"no zone", func(in *ScheduleInput) { in.Timezone = "" }, `unknown timezone ""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := ok
			tc.change(&in)
			_, err := in.normalize()
			require.ErrorIs(t, err, ErrInvalidSchedule)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestSchedules_TemplateInput_DropsTheBlocksAStartAdds(t *testing.T) {
	in := "<business_profile>\nGame X\n</business_profile>\n\n<scheduled_round>\nby 06:00\n</scheduled_round>\n\nfind 50 players"
	assert.Equal(t, "find 50 players", templateInput(in))
}

func TestSchedules_ScheduledInput_GivesTheDeadlineInTheZoneAndInUTC(t *testing.T) {
	bkk := zone(t, "Asia/Bangkok")
	got := scheduledInput("find 50 players",
		time.Date(2026, 10, 19, 22, 0, 0, 0, bkk), time.Date(2026, 10, 20, 6, 0, 0, 0, bkk))
	assert.Equal(t, "<scheduled_round>\n"+
		"The customer's schedule started this round at 22:00 on Monday 2026-10-19 (Asia/Bangkok). "+
		"Deliver it by 06:00 on Tuesday 2026-10-20 (Asia/Bangkok; 23:00 2026-10-19 UTC): "+
		"before then call growth_deliver_leads, with allow_partial=true if the target is not reached, and report. "+
		"At that time the qualified leads found so far are delivered and the task is stopped.\n"+
		"</scheduled_round>\n\nfind 50 players", got)
}
