package flowctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSchedules struct {
	starts  []growth.ScheduledStart
	closed  []growth.ClosedRound
	calls   *[]string
	started map[int64]int64
	failed  map[int64]string
	handled []int64
	// openErr fails opening the round of every start.
	openErr error
}

func (f *fakeSchedules) DueStarts(ctx context.Context, _ time.Time) ([]growth.ScheduledStart, error) {
	*f.calls = append(*f.calls, "due")
	return f.starts, ctx.Err()
}

func (f *fakeSchedules) StartedSchedule(ctx context.Context, st growth.ScheduledStart, flowID int64) error {
	if f.openErr != nil {
		return f.openErr
	}
	f.started[st.ScheduleID] = flowID
	return ctx.Err()
}

func (f *fakeSchedules) DeadlineHandled(ctx context.Context, runID int64) error {
	f.handled = append(f.handled, runID)
	return ctx.Err()
}

func (f *fakeSchedules) FailedSchedule(_ context.Context, st growth.ScheduledStart, cause error) {
	f.failed[st.ScheduleID] = cause.Error()
}

func (f *fakeSchedules) CloseOverdueRounds(ctx context.Context, _ time.Time) ([]growth.ClosedRound, error) {
	*f.calls = append(*f.calls, "close")
	return f.closed, ctx.Err()
}

type loggedMsg struct {
	kind database.MsglogType
	text string
}

type recordingMsgLog struct {
	controller.FlowMsgLogWorker
	msgs []loggedMsg
}

func (r *recordingMsgLog) PutFlowMsg(ctx context.Context, kind database.MsglogType, _, msg string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.msgs = append(r.msgs, loggedMsg{kind, msg})
	return 1, nil
}

type contextWorker struct {
	controller.FlowWorker
	fc *controller.FlowContext
}

func (w *contextWorker) GetContext() *controller.FlowContext { return w.fc }

type createdFlow struct {
	userID  int64
	input   string
	prvname provider.ProviderName
	prvtype provider.ProviderType
}

type schedulingController struct {
	controller.FlowController
	created []createdFlow
	running map[int64]controller.FlowWorker
	stopped []int64
	calls   *[]string
	// stopFails is how many StopFlow calls fail before one succeeds.
	stopFails int
}

func (s *schedulingController) CreateFlow(
	ctx context.Context, userID int64, input string, prvname provider.ProviderName, prvtype provider.ProviderType,
	_ *tools.Functions, _ []database.UserResource,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	*s.calls = append(*s.calls, "create")
	s.created = append(s.created, createdFlow{userID, input, prvname, prvtype})
	return 41, nil
}

func (s *schedulingController) GetFlow(_ context.Context, flowID int64) (controller.FlowWorker, error) {
	if fw, ok := s.running[flowID]; ok {
		return fw, nil
	}
	return nil, controller.ErrFlowNotFound
}

func (s *schedulingController) StopFlow(ctx context.Context, flowID int64) error {
	*s.calls = append(*s.calls, "stop")
	if s.stopFails > 0 {
		s.stopFails--
		return errors.New("flow is busy")
	}
	s.stopped = append(s.stopped, flowID)
	return ctx.Err()
}

func knownProviders(_ context.Context, name provider.ProviderName, _ int64) (provider.ProviderType, error) {
	if name == "openai-main" {
		return provider.ProviderOpenAI, nil
	}
	return "", errors.New("provider not found")
}

func TestScheduler_Tick_StartsDueTasksAndRecordsWhyOneDidNot(t *testing.T) {
	var calls []string
	svc := &fakeSchedules{
		starts: []growth.ScheduledStart{
			{ScheduleID: 1, UserID: 7, ProviderName: "openai-main", Input: "<scheduled_round>\n</scheduled_round>\n\nfind 50"},
			{ScheduleID: 2, UserID: 7, ProviderName: "gone", Input: "find 10"},
		},
		calls: &calls, started: map[int64]int64{}, failed: map[int64]string{},
	}
	fc := &schedulingController{calls: &calls}

	NewScheduler(fc, svc, knownProviders).Tick(context.Background())

	assert.Equal(t, []createdFlow{{7, "<scheduled_round>\n</scheduled_round>\n\nfind 50", "openai-main", provider.ProviderOpenAI}}, fc.created)
	assert.Equal(t, map[int64]int64{1: 41}, svc.started)
	assert.Equal(t, map[int64]string{2: `provider "gone" of the task is unavailable: provider not found`}, svc.failed)
}

func TestScheduler_Tick_LogsTheEndTimeDeliveryInTheTasksLanguageThenStopsIt(t *testing.T) {
	var calls []string
	msglog := &recordingMsgLog{}
	svc := &fakeSchedules{
		starts: []growth.ScheduledStart{{ScheduleID: 1, UserID: 7, ProviderName: "openai-main", Input: "find 50"}},
		closed: []growth.ClosedRound{
			{RunID: 3, UserID: 7, FlowID: 9, Target: 5, Delivered: 2, Timezone: "Asia/Bangkok", Language: "chinese",
				Deadline: time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)},
			{RunID: 4, UserID: 7, FlowID: 10, Target: 5, Delivered: 0, Timezone: "UTC", Language: "english"},
		},
		calls: &calls, started: map[int64]int64{}, failed: map[int64]string{},
	}
	fc := &schedulingController{
		calls:   &calls,
		running: map[int64]controller.FlowWorker{9: &contextWorker{fc: &controller.FlowContext{MsgLog: msglog}}},
	}

	NewScheduler(fc, svc, knownProviders).Tick(context.Background())

	require.Len(t, msglog.msgs, 1)
	assert.Equal(t, loggedMsg{database.MsglogTypeReport, "已到结束时间 2026-10-19 06:00：交付 2 条（目标 5 条），任务已停止。"}, msglog.msgs[0])
	assert.Equal(t, []int64{9}, fc.stopped, "a task that is not running is not stopped")
	assert.Equal(t, []int64{3, 4}, svc.handled)
	assert.Equal(t, []string{"close", "stop", "due", "create"}, calls, "a round ends before the next one starts")
}

func TestScheduler_Tick_RetriesAStopThatFailedWithoutLoggingTwice(t *testing.T) {
	var calls []string
	msglog := &recordingMsgLog{}
	svc := &fakeSchedules{
		closed: []growth.ClosedRound{{RunID: 3, UserID: 7, FlowID: 9, Target: 5, Delivered: 2, Timezone: "UTC", Language: "english",
			Deadline: time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)}},
		calls: &calls, started: map[int64]int64{}, failed: map[int64]string{},
	}
	fc := &schedulingController{
		calls:     &calls,
		stopFails: 1,
		running:   map[int64]controller.FlowWorker{9: &contextWorker{fc: &controller.FlowContext{MsgLog: msglog}}},
	}
	s := NewScheduler(fc, svc, knownProviders)

	s.Tick(context.Background())
	assert.Empty(t, fc.stopped)
	assert.Empty(t, svc.handled, "a round whose task is still running stays open")

	s.Tick(context.Background())
	assert.Equal(t, []int64{9}, fc.stopped)
	assert.Equal(t, []int64{3}, svc.handled)
	assert.Equal(t, []loggedMsg{{database.MsglogTypeReport,
		"The end time 2026-10-18 23:00 was reached: 2 of the target 5 leads were delivered, and the task was stopped."}}, msglog.msgs)
}

func TestScheduler_Tick_StopsATaskWhoseRoundCouldNotBeOpened(t *testing.T) {
	var calls []string
	svc := &fakeSchedules{
		starts:  []growth.ScheduledStart{{ScheduleID: 1, UserID: 7, ProviderName: "openai-main", Input: "find 50"}},
		openErr: errors.New("failed to open the round of task 41: connection reset"),
		calls:   &calls, started: map[int64]int64{}, failed: map[int64]string{},
	}
	fc := &schedulingController{calls: &calls}

	NewScheduler(fc, svc, knownProviders).Tick(context.Background())

	assert.Equal(t, []int64{41}, fc.stopped)
	assert.Equal(t, map[int64]string{1: "failed to open the round of task 41: connection reset"}, svc.failed)
}
