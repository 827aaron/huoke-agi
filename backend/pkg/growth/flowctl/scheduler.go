package flowctl

import (
	"context"
	"fmt"
	"time"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/templates"

	"github.com/sirupsen/logrus"
)

const scheduleTick = time.Minute

// Schedules is what growth.Service offers the scheduler.
type Schedules interface {
	DueStarts(ctx context.Context, now time.Time) ([]growth.ScheduledStart, error)
	StartedSchedule(ctx context.Context, st growth.ScheduledStart, flowID int64) error
	FailedSchedule(ctx context.Context, st growth.ScheduledStart, cause error)
	CloseOverdueRounds(ctx context.Context, now time.Time) ([]growth.ClosedRound, error)
}

// ProviderType resolves a provider name for its owner, as a client's createFlow does.
type ProviderType func(ctx context.Context, name provider.ProviderName, userID int64) (provider.ProviderType, error)

// Scheduler opens the task of every due schedule and stops the tasks whose round has
// been closed at its end time. It runs in the API process: a deployment is one instance.
type Scheduler struct {
	fc       controller.FlowController
	svc      Schedules
	provider ProviderType
	now      func() time.Time
}

// NewScheduler takes fc wrapped by Wrap, so a scheduled task starts with the business
// profile as one a customer starts does.
func NewScheduler(fc controller.FlowController, svc Schedules, prv ProviderType) *Scheduler {
	return &Scheduler{fc: fc, svc: svc, provider: prv, now: time.Now}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick closes overdue rounds before it starts new ones, so a schedule whose end time
// meets its next start hands over in that order.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.now()
	closed, err := s.svc.CloseOverdueRounds(ctx, now)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to close the rounds past their end time")
	}
	for _, c := range closed {
		s.stop(ctx, c)
	}

	starts, err := s.svc.DueStarts(ctx, now)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to load the due schedules")
	}
	for _, st := range starts {
		if err := s.start(ctx, st); err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("schedule_id", st.ScheduleID).Error("scheduled task not started")
			s.svc.FailedSchedule(ctx, st, err)
		}
	}
}

func (s *Scheduler) start(ctx context.Context, st growth.ScheduledStart) error {
	name := provider.ProviderName(st.ProviderName)
	prvtype, err := s.provider(ctx, name, st.UserID)
	if err != nil {
		return fmt.Errorf("provider %q of the task is unavailable: %w", st.ProviderName, err)
	}
	flowID, err := s.fc.CreateFlow(ctx, st.UserID, st.Input, name, prvtype, nil, nil)
	if err != nil {
		return err
	}
	return s.svc.StartedSchedule(ctx, st, flowID)
}

func (s *Scheduler) stop(ctx context.Context, c growth.ClosedRound) {
	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{"flow_id": c.FlowID, "run_id": c.RunID})
	if c.FlowID == 0 {
		return
	}
	fw, err := s.fc.GetFlow(ctx, c.FlowID)
	if err != nil || fw == nil {
		logger.WithError(err).Info("round closed at its end time; its task is not running")
		return
	}
	at := c.Deadline
	if loc, err := time.LoadLocation(c.Timezone); err == nil {
		at = at.In(loc)
	}
	text := fmt.Sprintf(templates.NoticeText(templates.NoticeDeadlineDelivered, c.Language),
		at.Format("2006-01-02 15:04"), c.Delivered, c.Target)
	if fc := fw.GetContext(); fc != nil && fc.MsgLog != nil {
		if _, err := fc.MsgLog.PutFlowMsg(ctx, database.MsglogTypeReport, "", text); err != nil {
			logger.WithError(err).Warn("failed to log the delivery at the end time")
		}
	}
	if err := s.fc.StopFlow(ctx, c.FlowID); err != nil {
		logger.WithError(err).Warn("failed to stop the task at its end time")
	}
}
