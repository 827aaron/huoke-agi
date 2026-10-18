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
	DeadlineHandled(ctx context.Context, runID int64) error
}

// ProviderResolver resolves the provider a schedule names for its owner; a provider
// that is gone resolves to the owner's current one, as a typed message does.
type ProviderResolver func(ctx context.Context, name provider.ProviderName, userID int64) (provider.Provider, error)

// Scheduler opens the task of every due schedule and stops the tasks whose round has
// been closed at its end time. It runs in the API process: a deployment is one instance.
type Scheduler struct {
	fc       controller.FlowController
	svc      Schedules
	provider ProviderResolver
	now      func() time.Time

	// logged holds the rounds whose end-time line is written while their task has not
	// been stopped yet, so a retry on the next tick does not write it twice.
	logged map[int64]bool
}

// NewScheduler takes fc wrapped by Wrap, so a scheduled task starts with the business
// profile as one a customer starts does.
func NewScheduler(fc controller.FlowController, svc Schedules, prv ProviderResolver) *Scheduler {
	return &Scheduler{fc: fc, svc: svc, provider: prv, now: time.Now, logged: make(map[int64]bool)}
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
	prv, err := s.provider(ctx, provider.ProviderName(st.ProviderName), st.UserID)
	if err != nil {
		return fmt.Errorf("provider %q of the task is unavailable: %w", st.ProviderName, err)
	}
	if st.ProfileID != nil {
		ctx = growth.WithSelectedProfile(ctx, *st.ProfileID)
	}
	flowID, err := s.fc.CreateFlow(ctx, st.UserID, st.Input, prv.Name(), prv.Type(), nil, nil)
	if err != nil {
		return err
	}
	if err := s.svc.StartedSchedule(ctx, st, flowID); err != nil {
		// Without its round the task would run past the end time, so it does not run at all.
		if stopErr := s.fc.StopFlow(ctx, flowID); stopErr != nil {
			return fmt.Errorf("%w; the task was left running: %v", err, stopErr)
		}
		return err
	}
	return nil
}

func (s *Scheduler) stop(ctx context.Context, c growth.ClosedRound) {
	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{"flow_id": c.FlowID, "run_id": c.RunID})
	fw, err := s.fc.GetFlow(ctx, c.FlowID)
	if c.FlowID == 0 || err != nil || fw == nil {
		s.handled(ctx, c.RunID)
		return
	}
	if !s.logged[c.RunID] {
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
		s.logged[c.RunID] = true
	}
	if err := s.fc.StopFlow(ctx, c.FlowID); err != nil {
		logger.WithError(err).Warn("failed to stop the task at its end time; retrying on the next tick")
		return
	}
	s.handled(ctx, c.RunID)
}

func (s *Scheduler) handled(ctx context.Context, runID int64) {
	if err := s.svc.DeadlineHandled(ctx, runID); err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("run_id", runID).Error("round stays open past its end time")
		return
	}
	delete(s.logged, runID)
}
