//go:build postgres

package growth

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staleDue hands out the due schedules a pass read before another pass claimed them.
type staleDue struct {
	database.Querier
	rows []database.GrowthSchedule
}

func (q staleDue) GetDueGrowthSchedules(ctx context.Context, _ time.Time) ([]database.GrowthSchedule, error) {
	return q.rows, ctx.Err()
}

func (w *world) seedFlowWithInput(t *testing.T, input string) int64 {
	t.Helper()
	scope := w.seedTask(t)
	_, err := w.db.Exec(`UPDATE tasks SET input = $2 WHERE id = $1`, *scope.TaskID, input)
	require.NoError(t, err)
	return scope.FlowID
}

func (w *world) mustCreateSchedule(t *testing.T, in ScheduleInput) ScheduleView {
	t.Helper()
	v, err := w.svc.CreateSchedule(context.Background(), w.user, in)
	require.NoError(t, err)
	return v
}

func decodeMailText(t *testing.T, data string) string {
	t.Helper()
	_, rest, ok := strings.Cut(data, "Content-Transfer-Encoding: base64\r\n")
	require.True(t, ok, "message has no text part")
	_, body, ok := strings.Cut(rest, "\r\n\r\n")
	require.True(t, ok)
	body, _, _ = strings.Cut(body, "\r\n--")
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	require.NoError(t, err)
	return string(raw)
}

func TestSchedules_CreateSchedule_RepeatsTheTasksInputAndProvider(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	flowID := w.seedFlowWithInput(t, "<business_profile>\nGame X\n</business_profile>\n\nfind 50 players")

	v := w.mustCreateSchedule(t, ScheduleInput{
		FlowID: flowID, Days: ScheduleOnce, Date: "2099-01-05", StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok",
	})
	assert.Equal(t, "find 50 players", v.Input)
	require.NotNil(t, v.NextStartAt)
	assert.Equal(t, time.Date(2099, 1, 4, 19, 0, 0, 0, time.UTC), *v.NextStartAt)
	assert.True(t, v.Enabled)
	assert.Equal(t, []int{}, v.Weekdays)

	var provider string
	require.NoError(t, w.db.QueryRow(`SELECT provider_name FROM growth_schedules WHERE id = $1`, v.ID).Scan(&provider))
	assert.Equal(t, "p", provider)

	off := false
	paused, err := w.svc.UpdateSchedule(ctx, w.user, v.ID, ScheduleInput{
		Days: ScheduleWeekly, Weekdays: []int{6}, StartTime: "22:00", EndTime: "06:00", Timezone: "Asia/Bangkok", Enabled: &off,
	})
	require.NoError(t, err)
	assert.Equal(t, "find 50 players", paused.Input, "an empty input keeps the saved one")
	assert.Equal(t, []int{6}, paused.Weekdays)
	assert.Empty(t, paused.Date)
	assert.False(t, paused.Enabled)
	assert.Nil(t, paused.NextStartAt, "a paused schedule has no next start")

	list, err := w.svc.ListSchedules(ctx, w.user, &flowID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, v.ID, list[0].ID)
}

func TestSchedules_CreateSchedule_RefusesAPastOnceAndAnotherCustomersTask(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	flowID := w.seedFlowWithInput(t, "find 50 players")

	_, err := w.svc.CreateSchedule(ctx, w.user, ScheduleInput{
		FlowID: flowID, Days: ScheduleOnce, Date: "2020-01-05", StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok",
	})
	require.ErrorIs(t, err, ErrInvalidSchedule)
	assert.ErrorContains(t, err, "2020-01-05 02:00 is already past")

	other := seedUser(t, w.db, "other@example.test")
	daily := ScheduleInput{FlowID: flowID, Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok"}
	_, err = w.svc.CreateSchedule(ctx, other, daily)
	require.ErrorIs(t, err, ErrFlowNotFound)

	mine := w.mustCreateSchedule(t, daily)
	_, err = w.svc.UpdateSchedule(ctx, other, mine.ID, daily)
	require.ErrorIs(t, err, ErrScheduleNotFound)
	require.ErrorIs(t, w.svc.DeleteSchedule(ctx, other, mine.ID), ErrScheduleNotFound)
	theirs, err := w.svc.ListSchedules(ctx, other, nil)
	require.NoError(t, err)
	assert.Empty(t, theirs)

	require.NoError(t, w.svc.DeleteSchedule(ctx, w.user, mine.ID))
	require.ErrorIs(t, w.svc.DeleteSchedule(ctx, w.user, mine.ID), ErrScheduleNotFound)
}

func TestSchedules_DueStarts_ClaimsEachStartOnceAndSkipsAMissedOne(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	flowID := w.seedFlowWithInput(t, "find 50 players")
	v := w.mustCreateSchedule(t, ScheduleInput{
		FlowID: flowID, Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok",
	})
	first := *v.NextStartAt

	starts, err := w.svc.DueStarts(ctx, first.Add(-time.Minute))
	require.NoError(t, err)
	assert.Empty(t, starts, "nothing is due before the start")

	due, err := database.New(w.db).GetDueGrowthSchedules(ctx, first.Add(time.Minute))
	require.NoError(t, err)
	starts, err = w.svc.DueStarts(ctx, first.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, starts, 1)
	st := starts[0]
	late := NewService(staleDue{Querier: database.New(w.db), rows: due}, w.svc.cfg)
	raced, err := late.DueStarts(ctx, first.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, raced, "a pass that read the start before it was claimed does not run it again")
	assert.Equal(t, v.ID, st.ScheduleID)
	assert.Equal(t, "p", st.ProviderName)
	assert.Equal(t, first.Add(4*time.Hour), st.Deadline.UTC())
	assert.True(t, strings.HasPrefix(st.Input, "<scheduled_round>\n"))
	assert.True(t, strings.HasSuffix(st.Input, "</scheduled_round>\n\nfind 50 players"))

	again, err := w.svc.DueStarts(ctx, first.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, again, "a claimed start is not handed out twice")

	list, err := w.svc.ListSchedules(ctx, w.user, nil)
	require.NoError(t, err)
	second := first.Add(24 * time.Hour)
	assert.Equal(t, second, *list[0].NextStartAt)

	missed, err := w.svc.DueStarts(ctx, second.Add(5*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, missed, "a start whose end time has passed is not run late")
	list, err = w.svc.ListSchedules(ctx, w.user, nil)
	require.NoError(t, err)
	assert.Equal(t, second.Add(24*time.Hour), *list[0].NextStartAt)
}

func TestSchedules_DueStarts_DisablesAOnceAfterItsStart(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	flowID := w.seedFlowWithInput(t, "find 50 players")
	v := w.mustCreateSchedule(t, ScheduleInput{
		FlowID: flowID, Days: ScheduleOnce, Date: "2099-01-05", StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok",
	})

	starts, err := w.svc.DueStarts(ctx, time.Date(2099, 1, 4, 19, 1, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, starts, 1)

	list, err := w.svc.ListSchedules(ctx, w.user, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, v.ID, list[0].ID)
	assert.False(t, list[0].Enabled)
	assert.Nil(t, list[0].NextStartAt)
}

func TestSchedules_CloseOverdueRounds_DeliversWhatTheAdoptedRoundQualified(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	sink := startSMTPSink(t)
	w.saveSink(t, sink, "boss@example.test")
	template := w.seedFlowWithInput(t, "find 5 players")
	v := w.mustCreateSchedule(t, ScheduleInput{
		FlowID: template, Days: ScheduleDaily, StartTime: "02:00", EndTime: "06:00", Timezone: "Asia/Bangkok",
	})
	starts, err := w.svc.DueStarts(ctx, v.NextStartAt.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, starts, 1)
	deadline := starts[0].Deadline

	var flowID int64
	require.NoError(t, w.db.QueryRow(`
		INSERT INTO flows (title, model, model_provider_name, language, user_id, model_provider_type)
		VALUES ('scheduled', 'm', 'p', 'en', $1, 'openai') RETURNING id`, w.user).Scan(&flowID))
	require.NoError(t, w.svc.StartedSchedule(ctx, starts[0], flowID))

	scope := w.seedTaskIn(t, flowID)
	p, err := w.svc.SetTarget(ctx, scope, 5)
	require.NoError(t, err)
	var rounds int
	require.NoError(t, w.db.QueryRow(`SELECT count(*) FROM growth_runs WHERE flow_id = $1`, flowID).Scan(&rounds))
	assert.Equal(t, 1, rounds, "the task adopts the scheduled round instead of opening its own")

	src := w.mustAddTelegramGroup(t, scope, tgGroup("720", "arpg", "ARPG"))
	w.mustSaveQualified(t, scope, src, 1, 2)

	early, err := w.svc.CloseOverdueRounds(ctx, deadline.Add(-time.Minute))
	require.NoError(t, err)
	assert.Empty(t, early)

	closed, err := w.svc.CloseOverdueRounds(ctx, deadline)
	require.NoError(t, err)
	require.Len(t, closed, 1)
	closed[0].Deadline = closed[0].Deadline.UTC()
	assert.Equal(t, ClosedRound{
		RunID: p.RunID, UserID: w.user, FlowID: flowID, Target: 5, Delivered: 2,
		Deadline: deadline.UTC(), Timezone: "Asia/Bangkok", Language: "en",
	}, closed[0])

	list, err := w.svc.ListSchedules(ctx, w.user, nil)
	require.NoError(t, err)
	require.NotNil(t, list[0].LastRun)
	assert.Equal(t, RunStatusPartial, list[0].LastRun.Status)
	assert.Equal(t, 2, list[0].LastRun.DeliveredCount)
	assert.Equal(t, deadline.UTC(), *list[0].LastRun.DeadlineAt)

	msgs := sink.Messages()
	require.Len(t, msgs, 1)
	assert.Contains(t, decodeMailText(t, msgs[0].Data), "Delivered at the schedule's end time, ")

	again, err := w.svc.CloseOverdueRounds(ctx, deadline.Add(time.Hour))
	require.NoError(t, err)
	assert.Empty(t, again, "a closed round is closed once")
}
