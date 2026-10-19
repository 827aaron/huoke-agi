package growth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"pentagi/pkg/database"

	"github.com/sirupsen/logrus"
)

const (
	ScheduleDaily    = "daily"
	ScheduleWeekdays = "weekdays"
	ScheduleWeekly   = "weekly"
	ScheduleOnce     = "once"
)

var (
	ErrInvalidSchedule  = errors.New("invalid schedule")
	ErrScheduleNotFound = errors.New("schedule not found")
	ErrFlowNotFound     = errors.New("task not found")

	clockTime      = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	scheduledBlock = regexp.MustCompile(`(?s)<scheduled_round>.*?</scheduled_round>\s*`)
)

// ScheduleInput is a schedule as the settings page sends it. Times are HH:MM in Timezone;
// an EndTime not after StartTime ends on the next day. Weekdays count from 0, Sunday, and
// apply to weekly only; Date (YYYY-MM-DD) to once only. An empty Input on create takes the
// flow's first task input, and on update keeps the saved one; a nil Enabled means true on
// create and unchanged on update.
type ScheduleInput struct {
	FlowID    int64  `json:"flow_id"`
	Input     string `json:"input,omitempty"`
	Days      string `json:"days" enums:"daily,weekdays,weekly,once"`
	Weekdays  []int  `json:"weekdays,omitempty"`
	Date      string `json:"date,omitempty"`
	StartTime string `json:"start_time" example:"02:00"`
	EndTime   string `json:"end_time" example:"06:00"`
	Timezone  string `json:"timezone" example:"Asia/Bangkok"`
	Enabled   *bool  `json:"enabled,omitempty"`
	// ProfileID is the business profile each start uses; on create nil takes the flow's,
	// on update nil keeps the saved one, and 0 means the default at each start.
	ProfileID *int64 `json:"profile_id,omitempty"`
}

type ScheduleRun struct {
	RunID          int64      `json:"run_id"`
	Status         string     `json:"status"`
	Target         int        `json:"target"`
	DeliveredCount int        `json:"delivered_count"`
	DeadlineAt     *time.Time `json:"deadline_at,omitempty"`
}

// ScheduleView: NextStartAt is nil when nothing is ahead (disabled, or a once already
// past). LastError says why the latest start could not open its task.
type ScheduleView struct {
	ID          int64        `json:"id"`
	FlowID      *int64       `json:"flow_id,omitempty"`
	Input       string       `json:"input"`
	Days        string       `json:"days"`
	Weekdays    []int        `json:"weekdays"`
	Date        string       `json:"date,omitempty"`
	StartTime   string       `json:"start_time"`
	EndTime     string       `json:"end_time"`
	Timezone    string       `json:"timezone"`
	Enabled     bool         `json:"enabled"`
	NextStartAt *time.Time   `json:"next_start_at,omitempty"`
	LastRun     *ScheduleRun `json:"last_run,omitempty"`
	LastError   string       `json:"last_error,omitempty"`
	ProfileID   *int64       `json:"profile_id,omitempty"`
}

// ScheduledStart is one start the scheduler must open a task for.
type ScheduledStart struct {
	ScheduleID   int64
	UserID       int64
	ProviderName string
	// ProfileID is nil to start with the owner's default profile.
	ProfileID *int64
	Input     string
	Start     time.Time
	Deadline  time.Time
}

// ClosedRound is a round closed at its end time; the scheduler stops its flow.
type ClosedRound struct {
	RunID     int64
	UserID    int64
	FlowID    int64
	Target    int
	Delivered int
	Deadline  time.Time
	Timezone  string
	Language  string
}

type schedule struct {
	days     string
	weekdays []int
	date     time.Time
	start    [2]int
	end      [2]int
	loc      *time.Location
}

func (in *ScheduleInput) normalize() (schedule, error) {
	in.Days = strings.ToLower(strings.TrimSpace(in.Days))
	in.StartTime, in.EndTime = strings.TrimSpace(in.StartTime), strings.TrimSpace(in.EndTime)
	in.Timezone, in.Date = strings.TrimSpace(in.Timezone), strings.TrimSpace(in.Date)
	in.Input = strings.TrimSpace(in.Input)

	var sc schedule
	switch in.Days {
	case ScheduleDaily, ScheduleWeekdays, ScheduleOnce:
		in.Weekdays = nil
	case ScheduleWeekly:
		days := slices.Clone(in.Weekdays)
		slices.Sort(days)
		days = slices.Compact(days)
		if len(days) == 0 || days[0] < 0 || days[len(days)-1] > 6 {
			return sc, fmt.Errorf("%w: weekly needs weekdays between 0 (Sunday) and 6", ErrInvalidSchedule)
		}
		in.Weekdays = days
	default:
		return sc, fmt.Errorf("%w: days must be one of daily, weekdays, weekly, once", ErrInvalidSchedule)
	}
	sc.days, sc.weekdays = in.Days, in.Weekdays

	if in.Days == ScheduleOnce {
		d, err := time.Parse(time.DateOnly, in.Date)
		if err != nil {
			return sc, fmt.Errorf("%w: once needs date as YYYY-MM-DD", ErrInvalidSchedule)
		}
		sc.date = d
	} else {
		in.Date = ""
	}

	for _, t := range []struct {
		name, value string
		dst         *[2]int
	}{{"start_time", in.StartTime, &sc.start}, {"end_time", in.EndTime, &sc.end}} {
		if !clockTime.MatchString(t.value) {
			return sc, fmt.Errorf("%w: %s must be HH:MM, got %q", ErrInvalidSchedule, t.name, t.value)
		}
		_, _ = fmt.Sscanf(t.value, "%d:%d", &t.dst[0], &t.dst[1])
	}
	if in.StartTime == in.EndTime {
		return sc, fmt.Errorf("%w: end_time must differ from start_time", ErrInvalidSchedule)
	}

	loc, err := time.LoadLocation(in.Timezone)
	if err != nil || in.Timezone == "" || strings.EqualFold(in.Timezone, "Local") {
		return sc, fmt.Errorf("%w: unknown timezone %q", ErrInvalidSchedule, in.Timezone)
	}
	sc.loc = loc
	return sc, nil
}

func scheduleOf(row database.GrowthSchedule) (schedule, error) {
	in := ScheduleInput{
		Days: row.Days, Date: row.Date, StartTime: row.StartTime, EndTime: row.EndTime,
		Timezone: row.Timezone, Weekdays: intsOf(row.Weekdays),
	}
	return in.normalize()
}

func (sc schedule) runsOn(d time.Time) bool {
	switch sc.days {
	case ScheduleWeekdays:
		return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
	case ScheduleWeekly:
		return slices.Contains(sc.weekdays, int(d.Weekday()))
	}
	return true
}

// window is the start and end of the run that begins on the local date of day.
func (sc schedule) window(day time.Time) (time.Time, time.Time) {
	y, m, d := day.Date()
	start := time.Date(y, m, d, sc.start[0], sc.start[1], 0, 0, sc.loc)
	end := time.Date(y, m, d, sc.end[0], sc.end[1], 0, 0, sc.loc)
	if !end.After(start) {
		end = time.Date(y, m, d+1, sc.end[0], sc.end[1], 0, 0, sc.loc)
	}
	return start, end
}

// next is the first start strictly after after; false when none is ahead.
func (sc schedule) next(after time.Time) (time.Time, bool) {
	if sc.days == ScheduleOnce {
		start, _ := sc.window(time.Date(sc.date.Year(), sc.date.Month(), sc.date.Day(), 12, 0, 0, 0, sc.loc))
		return start, start.After(after)
	}
	local := after.In(sc.loc)
	for i := range 8 {
		day := time.Date(local.Year(), local.Month(), local.Day()+i, 12, 0, 0, 0, sc.loc)
		if !sc.runsOn(day) {
			continue
		}
		if start, _ := sc.window(day); start.After(after) {
			return start, true
		}
	}
	return time.Time{}, false
}

func intsOf(v []int64) []int {
	out := make([]int, 0, len(v))
	for _, x := range v {
		out = append(out, int(x))
	}
	return out
}

func int64sOf(v []int) []int64 {
	out := make([]int64, 0, len(v))
	for _, x := range v {
		out = append(out, int64(x))
	}
	return out
}

func startAt(t time.Time, ok bool) sql.NullTime {
	return sql.NullTime{Time: t, Valid: ok}
}

func (s *Service) scheduleView(ctx context.Context, row database.GrowthSchedule) ScheduleView {
	v := ScheduleView{
		ID: row.ID, Input: row.Input, Days: row.Days, Weekdays: intsOf(row.Weekdays), Date: row.Date,
		StartTime: row.StartTime, EndTime: row.EndTime, Timezone: row.Timezone, Enabled: row.Enabled,
		LastError: row.LastError,
	}
	if row.FlowID.Valid {
		v.FlowID = &row.FlowID.Int64
	}
	if row.ProfileID.Valid {
		v.ProfileID = &row.ProfileID.Int64
	}
	if row.NextStartAt.Valid {
		t := row.NextStartAt.Time.UTC()
		v.NextStartAt = &t
	}
	if row.LastRunID.Valid {
		if run, err := s.db.GetGrowthRun(ctx, row.LastRunID.Int64); err == nil {
			v.LastRun = &ScheduleRun{
				RunID: run.ID, Status: run.Status, Target: int(run.Target), DeliveredCount: int(run.DeliveredCount),
			}
			if run.DeadlineAt.Valid {
				t := run.DeadlineAt.Time.UTC()
				v.LastRun.DeadlineAt = &t
			}
		}
	}
	return v
}

func (s *Service) ListSchedules(ctx context.Context, userID int64, flowID *int64) ([]ScheduleView, error) {
	params := database.GetUserGrowthSchedulesParams{UserID: userID}
	if flowID != nil {
		params.FlowID = sql.NullInt64{Int64: *flowID, Valid: true}
	}
	rows, err := s.db.GetUserGrowthSchedules(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}
	out := make([]ScheduleView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.scheduleView(ctx, r))
	}
	return out, nil
}

// CreateSchedule repeats the flow's task: its provider, and its first input without the
// blocks a start adds (the business profile and the scheduled-round note).
func (s *Service) CreateSchedule(ctx context.Context, userID int64, in ScheduleInput) (ScheduleView, error) {
	sc, err := in.normalize()
	if err != nil {
		return ScheduleView{}, err
	}
	flow, err := s.db.GetUserFlow(ctx, database.GetUserFlowParams{ID: in.FlowID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleView{}, fmt.Errorf("%w: %d", ErrFlowNotFound, in.FlowID)
	} else if err != nil {
		return ScheduleView{}, fmt.Errorf("failed to load task %d: %w", in.FlowID, err)
	}
	if in.Input == "" {
		tasks, err := s.db.GetFlowTasks(ctx, flow.ID)
		if err != nil {
			return ScheduleView{}, fmt.Errorf("failed to load the input of task %d: %w", flow.ID, err)
		}
		if len(tasks) > 0 {
			in.Input = templateInput(tasks[0].Input)
		}
		if in.Input == "" {
			return ScheduleView{}, fmt.Errorf("%w: task %d has no input to repeat, so input is needed", ErrInvalidSchedule, flow.ID)
		}
	}
	profileID, err := s.scheduleProfile(ctx, userID, in.ProfileID, sql.NullInt64{})
	if err != nil {
		return ScheduleView{}, err
	}
	if in.ProfileID == nil {
		if fp, err := s.db.GetGrowthFlowProfile(ctx, flow.ID); err == nil {
			profileID = fp.ProfileID
		}
	}
	enabled := in.Enabled == nil || *in.Enabled
	next, ok := sc.next(time.Now())
	if sc.days == ScheduleOnce && !ok {
		return ScheduleView{}, fmt.Errorf("%w: %s %s is already past", ErrInvalidSchedule, in.Date, in.StartTime)
	}

	row, err := s.db.CreateGrowthSchedule(ctx, database.CreateGrowthScheduleParams{
		UserID:       userID,
		FlowID:       sql.NullInt64{Int64: flow.ID, Valid: true},
		Input:        in.Input,
		ProviderName: flow.ModelProviderName,
		Days:         in.Days,
		Weekdays:     int64sOf(in.Weekdays),
		Date:         in.Date,
		StartTime:    in.StartTime,
		EndTime:      in.EndTime,
		Timezone:     in.Timezone,
		Enabled:      enabled,
		NextStartAt:  startAt(next, ok && enabled),
		ProfileID:    profileID,
	})
	if err != nil {
		return ScheduleView{}, fmt.Errorf("failed to save the schedule: %w", err)
	}
	return s.scheduleView(ctx, row), nil
}

func (s *Service) UpdateSchedule(ctx context.Context, userID, id int64, in ScheduleInput) (ScheduleView, error) {
	old, err := s.db.GetUserGrowthSchedule(ctx, database.GetUserGrowthScheduleParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleView{}, fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	} else if err != nil {
		return ScheduleView{}, fmt.Errorf("failed to load schedule %d: %w", id, err)
	}
	sc, err := in.normalize()
	if err != nil {
		return ScheduleView{}, err
	}
	if in.Input == "" {
		in.Input = old.Input
	}
	profileID, err := s.scheduleProfile(ctx, userID, in.ProfileID, old.ProfileID)
	if err != nil {
		return ScheduleView{}, err
	}
	enabled := old.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	next, ok := sc.next(time.Now())
	if sc.days == ScheduleOnce && !ok {
		return ScheduleView{}, fmt.Errorf("%w: %s %s is already past", ErrInvalidSchedule, in.Date, in.StartTime)
	}

	row, err := s.db.UpdateGrowthSchedule(ctx, database.UpdateGrowthScheduleParams{
		ID:          id,
		UserID:      userID,
		Input:       in.Input,
		Days:        in.Days,
		Weekdays:    int64sOf(in.Weekdays),
		Date:        in.Date,
		StartTime:   in.StartTime,
		EndTime:     in.EndTime,
		Timezone:    in.Timezone,
		Enabled:     enabled,
		NextStartAt: startAt(next, ok && enabled),
		ProfileID:   profileID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleView{}, fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	} else if err != nil {
		return ScheduleView{}, fmt.Errorf("failed to save schedule %d: %w", id, err)
	}
	return s.scheduleView(ctx, row), nil
}

// scheduleProfile is the profile column for a requested id: kept for nil, cleared for 0,
// else the id once it is known to be the owner's.
func (s *Service) scheduleProfile(ctx context.Context, userID int64, requested *int64, kept sql.NullInt64) (sql.NullInt64, error) {
	switch {
	case requested == nil:
		return kept, nil
	case *requested == 0:
		return sql.NullInt64{}, nil
	}
	if _, err := s.GetProfile(ctx, userID, *requested); err != nil {
		if errors.Is(err, ErrNoProfile) {
			return sql.NullInt64{}, fmt.Errorf("%w: business profile %d is not one of yours", ErrInvalidSchedule, *requested)
		}
		return sql.NullInt64{}, err
	}
	return sql.NullInt64{Int64: *requested, Valid: true}, nil
}

func (s *Service) DeleteSchedule(ctx context.Context, userID, id int64) error {
	n, err := s.db.DeleteUserGrowthSchedule(ctx, database.DeleteUserGrowthScheduleParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("failed to delete schedule %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	}
	return nil
}

func templateInput(input string) string {
	input = profileBlock.ReplaceAllString(input, "")
	return strings.TrimSpace(scheduledBlock.ReplaceAllString(input, ""))
}

// scheduledInput tells the agents when the round must be delivered. Their containers
// run in UTC, so the deadline is given in UTC beside the customer's zone.
func scheduledInput(input string, start, deadline time.Time) string {
	const layout = "15:04 on Monday 2006-01-02"
	return fmt.Sprintf("<scheduled_round>\n"+
		"The customer's schedule started this round at %s (%s). Deliver it by %s (%s; %s UTC): "+
		"before then call growth_deliver_leads, with allow_partial=true if the target is not reached, and report. "+
		"At that time the qualified leads found so far are delivered and the task is stopped.\n"+
		"</scheduled_round>\n\n%s",
		start.Format(layout), start.Location(), deadline.Format(layout), deadline.Location(),
		deadline.UTC().Format("15:04 2006-01-02"), templateInput(input))
}

// DueStarts claims every schedule whose start has come and returns the starts to open.
// A start whose end time has passed as well (the server was down through the window)
// is skipped, not run late.
func (s *Service) DueStarts(ctx context.Context, now time.Time) ([]ScheduledStart, error) {
	rows, err := s.db.GetDueGrowthSchedules(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("failed to load due schedules: %w", err)
	}
	var out []ScheduledStart
	for _, row := range rows {
		logger := logrus.WithContext(ctx).WithFields(logrus.Fields{"schedule_id": row.ID, "user_id": row.UserID})
		sc, err := scheduleOf(row)
		if err != nil {
			logger.WithError(err).Warn("schedule no longer valid, disabled")
			s.advance(ctx, row, time.Time{}, false, false)
			continue
		}
		start := row.NextStartAt.Time.In(sc.loc)
		_, deadline := sc.window(start)
		next, ok := sc.next(now)
		if !s.advance(ctx, row, next, ok, row.Enabled && sc.days != ScheduleOnce) {
			continue
		}
		if !deadline.After(now) {
			logger.WithField("start", start).Warn("scheduled start missed: its end time has passed")
			continue
		}
		// Without Jev the round's first read would fail anyway, after the planner has spent
		// its tokens; refusing here puts the reason on the schedule instead.
		if err := s.JudgeReady(ctx, row.UserID); err != nil {
			logger.WithError(err).Warn("scheduled start refused")
			s.FailedSchedule(ctx, ScheduledStart{ScheduleID: row.ID, UserID: row.UserID}, err)
			continue
		}
		out = append(out, ScheduledStart{
			ScheduleID:   row.ID,
			UserID:       row.UserID,
			ProviderName: row.ProviderName,
			ProfileID:    nullInt64Ptr(row.ProfileID),
			Input:        scheduledInput(row.Input, start, deadline.In(sc.loc)),
			Start:        start,
			Deadline:     deadline,
		})
	}
	return out, nil
}

func (s *Service) advance(ctx context.Context, row database.GrowthSchedule, next time.Time, ok, enabled bool) bool {
	n, err := s.db.AdvanceGrowthSchedule(ctx, database.AdvanceGrowthScheduleParams{
		ID: row.ID, HandledStartAt: row.NextStartAt, NextStartAt: startAt(next, ok && enabled), Enabled: enabled,
	})
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("schedule_id", row.ID).Error("failed to advance schedule")
		return false
	}
	return n == 1
}

// StartedSchedule opens the round of the flow a start created, with its deadline; the
// flow's first task adopts it.
func (s *Service) StartedSchedule(ctx context.Context, st ScheduledStart, flowID int64) error {
	run, err := s.db.OpenScheduledGrowthRun(ctx, database.OpenScheduledGrowthRunParams{
		UserID:     st.UserID,
		FlowID:     sql.NullInt64{Int64: flowID, Valid: true},
		Target:     DefaultTarget,
		DeadlineAt: sql.NullTime{Time: st.Deadline, Valid: true},
		ScheduleID: sql.NullInt64{Int64: st.ScheduleID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to open the round of task %d: %w", flowID, err)
	}
	return s.db.RecordGrowthScheduleStart(ctx, database.RecordGrowthScheduleStartParams{
		ID: st.ScheduleID, LastRunID: sql.NullInt64{Int64: run.ID, Valid: true},
	})
}

func (s *Service) FailedSchedule(ctx context.Context, st ScheduledStart, cause error) {
	if err := s.db.RecordGrowthScheduleStart(ctx, database.RecordGrowthScheduleStartParams{
		ID: st.ScheduleID, LastError: truncateRunes(cause.Error(), 500),
	}); err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("schedule_id", st.ScheduleID).Error("failed to record the schedule error")
	}
}

// CloseOverdueRounds delivers what every round past its end time has qualified, short of
// the target or not, topping up a round the agents delivered in part, and mails it as a
// delivery by the agents would. A round is returned on every pass until DeadlineHandled
// marks it, so a task that could not be stopped is tried again.
func (s *Service) CloseOverdueRounds(ctx context.Context, now time.Time) ([]ClosedRound, error) {
	runs, err := s.db.GetOverdueGrowthRuns(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("failed to load overdue rounds: %w", err)
	}
	var out []ClosedRound
	for _, run := range runs {
		scope := Scope{UserID: run.UserID, FlowID: run.FlowID.Int64}
		if run.TaskID.Valid {
			scope.TaskID = &run.TaskID.Int64
		}
		d, err := s.deliverRun(ctx, scope, run, DeliverOptions{AllowPartial: true, atDeadline: true})
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("run_id", run.ID).Error("failed to close the round at its end time")
			continue
		}
		c := ClosedRound{
			RunID: run.ID, UserID: run.UserID, FlowID: run.FlowID.Int64, Target: d.Target, Delivered: d.Delivered,
			Deadline: run.DeadlineAt.Time, Timezone: "UTC",
		}
		if run.ScheduleID.Valid {
			if sc, err := s.db.GetUserGrowthSchedule(ctx, database.GetUserGrowthScheduleParams{
				ID: run.ScheduleID.Int64, UserID: run.UserID,
			}); err == nil {
				c.Timezone = sc.Timezone
			}
		}
		if run.FlowID.Valid {
			if flow, err := s.db.GetFlow(ctx, run.FlowID.Int64); err == nil {
				c.Language = flow.Language
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// DeadlineHandled marks a round whose task has been stopped at its end time, or was not running.
func (s *Service) DeadlineHandled(ctx context.Context, runID int64) error {
	if err := s.db.CloseGrowthRunDeadline(ctx, runID); err != nil {
		return fmt.Errorf("failed to mark round %d closed at its end time: %w", runID, err)
	}
	return nil
}

func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
