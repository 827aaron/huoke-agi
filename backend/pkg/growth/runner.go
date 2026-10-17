package growth

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"pentagi/pkg/database"
)

const (
	RunStatusRunning   = "running"
	RunStatusPartial   = "partial"
	RunStatusCompleted = "completed"

	DeliveryIncomplete = "incomplete"
)

var ErrNoTask = errors.New("this call is not part of a task, so there is no round to count against")

type SourceStats struct {
	Total       int64 `json:"total"`
	Unresolved  int64 `json:"unresolved"`
	Invalid     int64 `json:"invalid"`
	Joined      int64 `json:"joined"`
	Pending     int64 `json:"pending"`
	NotJoinable int64 `json:"not_joinable"`
	Read        int64 `json:"read"`
	Exhausted   int64 `json:"exhausted"`
}

// Progress is the round as the database sees it. NewLeads is what the agents call
// new_leads_this_task: leads this round delivered plus qualified, undelivered leads
// seen in this flow, so it never drops when the round delivers.
type Progress struct {
	RunID            int64  `json:"run_id,omitempty"`
	TaskID           int64  `json:"task_id,omitempty"`
	Status           string `json:"status,omitempty"`
	Target           int    `json:"target"`
	NewLeads         int    `json:"new_leads_this_task"`
	Watchlist        int    `json:"watchlist"`
	DeliveredThisRun int    `json:"delivered_this_round"`
	Remaining        int    `json:"remaining"`
	// Qualified and Delivered count the owner's leads outside a task, from Since when set.
	Qualified int         `json:"qualified,omitempty"`
	Delivered int         `json:"delivered,omitempty"`
	Since     string      `json:"since,omitempty"`
	Sources   SourceStats `json:"sources"`
	// Warning names a blocker only the customer can clear, such as a missing Jev key.
	Warning string `json:"warning,omitempty"`
}

// Line is the one line every lead-collecting agent copies into its result; the
// refiner and the reporter read the round's state from it.
func (p Progress) Line() string {
	return fmt.Sprintf("PROGRESS: new_leads_this_task=%d target=%d", p.NewLeads, p.Target)
}

type Delivery struct {
	Status      string     `json:"status"`
	Target      int        `json:"target"`
	Delivered   int        `json:"delivered_this_round"`
	Gap         int        `json:"gap"`
	Instruction string     `json:"instruction,omitempty"`
	Leads       []LeadView `json:"leads"`
	Progress    Progress   `json:"progress"`
	// Email is the outcome of mailing this call's newly delivered leads: sent, skipped
	// (no SMTP configured) or failed with the reason. Empty when nothing new was delivered.
	Email string `json:"email,omitempty"`
}

func (s *Service) EnsureRun(ctx context.Context, scope Scope) (database.GrowthRun, error) {
	if scope.TaskID == nil {
		return database.GrowthRun{}, ErrNoTask
	}
	if run, err := s.db.GetGrowthRunByTask(ctx, scope.taskID()); err == nil {
		return run, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return run, fmt.Errorf("failed to load the round of task %d: %w", *scope.TaskID, err)
	}
	if scope.FlowID != 0 {
		run, err := s.db.AdoptGrowthRun(ctx, database.AdoptGrowthRunParams{
			UserID: scope.UserID, FlowID: scope.flowID(), TaskID: scope.taskID(),
		})
		if err == nil {
			return run, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return run, fmt.Errorf("failed to adopt the scheduled round of task %d: %w", *scope.TaskID, err)
		}
	}
	run, err := s.db.EnsureGrowthRun(ctx, database.EnsureGrowthRunParams{
		UserID: scope.UserID,
		FlowID: scope.flowID(),
		TaskID: scope.taskID(),
		Target: DefaultTarget,
	})
	if err != nil {
		return run, fmt.Errorf("failed to open the round of task %d: %w", *scope.TaskID, err)
	}
	return run, nil
}

func (s *Service) SetTarget(ctx context.Context, scope Scope, target int) (Progress, error) {
	if target <= 0 {
		return Progress{}, fmt.Errorf("target must be positive, got %d", target)
	}
	run, err := s.EnsureRun(ctx, scope)
	if err != nil {
		return Progress{}, err
	}
	if int(run.Target) != target {
		if _, err := s.db.UpdateGrowthRunTarget(ctx, database.UpdateGrowthRunTargetParams{
			ID: run.ID, Target: int32(target),
		}); err != nil {
			return Progress{}, fmt.Errorf("failed to set target of round %d: %w", run.ID, err)
		}
	}
	return s.Progress(ctx, scope)
}

// Progress reports the round of the calling task. Without a task it reports every lead
// of the owner and the pool, with no target.
func (s *Service) Progress(ctx context.Context, scope Scope) (Progress, error) {
	return s.ProgressSince(ctx, scope, time.Time{})
}

// ProgressSince is Progress counting only leads first found at or after since; the zero
// time counts them all.
func (s *Service) ProgressSince(ctx context.Context, scope Scope, since time.Time) (Progress, error) {
	var p Progress
	if scope.TaskID != nil {
		run, err := s.EnsureRun(ctx, scope)
		if err != nil {
			return p, err
		}
		p.RunID, p.TaskID, p.Status, p.Target = run.ID, run.TaskID.Int64, run.Status, int(run.Target)

		delivered, err := s.db.GetRunDeliveredLeads(ctx, sql.NullInt64{Int64: run.ID, Valid: true})
		if err != nil {
			return p, fmt.Errorf("failed to count leads delivered by round %d: %w", run.ID, err)
		}
		p.DeliveredThisRun = len(delivered)
	}

	params := database.CountUserLeadsParams{UserID: scope.UserID}
	if scope.TaskID != nil {
		params.FlowID = scope.flowID()
	}
	if !since.IsZero() {
		params.Since = sql.NullTime{Time: since, Valid: true}
		p.Since = since.Format(time.RFC3339)
	}
	counts, err := s.db.CountUserLeads(ctx, params)
	if err != nil {
		return p, fmt.Errorf("failed to count leads: %w", err)
	}
	p.NewLeads = p.DeliveredThisRun + int(counts.Deliverable)
	p.Watchlist = int(counts.Watchlist)
	p.Remaining = max(p.Target-p.NewLeads, 0)
	if scope.TaskID == nil {
		p.Qualified, p.Delivered = int(counts.Deliverable+counts.Delivered), int(counts.Delivered)
	}
	if cfg, _ := s.ConfigFor(ctx, scope.UserID); cfg.TypeSafeAPIKey == "" {
		p.Warning = ErrNoJudge.Error()
	}

	if p.Sources, err = s.SourceStats(ctx, scope.UserID); err != nil {
		return p, err
	}
	return p, nil
}

// DeliverOptions: HasContact delivers only leads with a contact beside their platform
// account, so the target counts those alone.
type DeliverOptions struct {
	AllowPartial bool
	HasContact   bool

	// atDeadline closes a scheduled round at its end time, which its email says.
	atDeadline bool
}

func (s *Service) SourceStats(ctx context.Context, userID int64) (SourceStats, error) {
	row, err := s.db.GetUserSourceStats(ctx, userID)
	if err != nil {
		return SourceStats{}, fmt.Errorf("failed to summarise the source pool: %w", err)
	}
	return SourceStats{
		Total:       row.Total,
		Unresolved:  row.Unresolved,
		Invalid:     row.Invalid,
		Joined:      row.Joined,
		Pending:     row.Pending,
		NotJoinable: row.NotJoinable,
		Read:        row.Read,
		Exhausted:   row.Exhausted,
	}, nil
}

// Deliver closes the round. With enough qualified leads it marks the best `target` of
// them delivered and completes the round. Short of the target it delivers nothing and
// tells the caller how many more to collect, unless allowPartial is set (the sources are
// exhausted), in which case it delivers what there is. Calling it again tops a partial
// round up and returns a completed round's delivery unchanged.
func (s *Service) Deliver(ctx context.Context, scope Scope, opt DeliverOptions) (Delivery, error) {
	run, err := s.EnsureRun(ctx, scope)
	if err != nil {
		return Delivery{}, err
	}
	return s.deliverRun(ctx, scope, run, opt)
}

func (s *Service) deliverRun(ctx context.Context, scope Scope, run database.GrowthRun, opt DeliverOptions) (Delivery, error) {
	allowPartial := opt.AllowPartial
	target := int(run.Target)
	runID := sql.NullInt64{Int64: run.ID, Valid: true}

	already, err := s.db.GetRunDeliveredLeads(ctx, runID)
	if err != nil {
		return Delivery{}, fmt.Errorf("failed to load the delivery of round %d: %w", run.ID, err)
	}
	need := target - len(already)
	newlyDelivered := false

	if need > 0 {
		candidates, err := s.db.GetDeliverableLeads(ctx, database.GetDeliverableLeadsParams{
			UserID:     scope.UserID,
			FlowID:     scope.flowID(),
			HasContact: opt.HasContact,
			Lim:        int64(need),
		})
		if err != nil {
			return Delivery{}, fmt.Errorf("failed to select leads for round %d: %w", run.ID, err)
		}

		if len(candidates) < need && !allowPartial {
			p, err := s.Progress(ctx, scope)
			if err != nil {
				return Delivery{}, err
			}
			gap := need - len(candidates)
			return Delivery{
				Status:    DeliveryIncomplete,
				Target:    target,
				Delivered: len(already),
				Gap:       gap,
				Instruction: fmt.Sprintf(
					"Nothing was delivered: %d more qualified leads are needed to reach the target of %d. "+
						"Keep collecting (read more history, find and join more sources), then call this tool again. "+
						"Only if every source is exhausted, call it with allow_partial=true to deliver what exists.",
					gap, target),
				Leads:    []LeadView{},
				Progress: p,
			}, nil
		}

		if len(candidates) > 0 {
			ids := make([]int64, 0, len(candidates))
			for _, c := range candidates {
				ids = append(ids, c.ID)
			}
			if _, err := s.db.MarkLeadsDelivered(ctx, database.MarkLeadsDeliveredParams{RunID: runID, Ids: ids}); err != nil {
				return Delivery{}, fmt.Errorf("failed to mark leads delivered in round %d: %w", run.ID, err)
			}
			newlyDelivered = true
		}
	}

	delivered, err := s.db.GetRunDeliveredLeads(ctx, runID)
	if err != nil {
		return Delivery{}, fmt.Errorf("failed to load the delivery of round %d: %w", run.ID, err)
	}
	status := RunStatusCompleted
	if len(delivered) < target {
		status = RunStatusPartial
	}
	if run.Status != status || int(run.DeliveredCount) != len(delivered) {
		if _, err := s.db.UpdateGrowthRunDelivery(ctx, database.UpdateGrowthRunDeliveryParams{
			ID: run.ID, Status: status, DeliveredCount: int32(len(delivered)),
		}); err != nil {
			return Delivery{}, fmt.Errorf("failed to close round %d: %w", run.ID, err)
		}
	}

	views, err := s.leadViews(ctx, delivered)
	if err != nil {
		return Delivery{}, err
	}
	p, err := s.Progress(ctx, scope)
	if err != nil {
		return Delivery{}, err
	}
	d := Delivery{
		Status:    status,
		Target:    target,
		Delivered: len(delivered),
		Gap:       max(target-len(delivered), 0),
		Leads:     views,
		Progress:  p,
	}
	// At the end time the customer gets a summary even when nothing new was delivered,
	// unless an earlier delivery of the round has mailed one already.
	if newlyDelivered || (opt.atDeadline && run.NotifyStatus == "") {
		var deadline time.Time
		if opt.atDeadline {
			deadline = run.DeadlineAt.Time
		}
		d.Email = s.notifyDelivery(ctx, scope, run, views, target, deadline)
	}
	return d, nil
}

// RunLeads counts a round's leads by tier: those its task found and those it delivered.
type RunLeads struct {
	Found int
	Watch int
}

// RunLeads is keyed by round id; a round with no lead is absent.
func (s *Service) RunLeads(ctx context.Context, userID int64, runIDs []int64) (map[int64]RunLeads, error) {
	rows, err := s.db.CountGrowthRunLeads(ctx, database.CountGrowthRunLeadsParams{UserID: userID, RunIds: runIDs})
	if err != nil {
		return nil, fmt.Errorf("failed to count the leads of the rounds: %w", err)
	}
	out := make(map[int64]RunLeads, len(rows))
	for _, r := range rows {
		out[r.RunID] = RunLeads{Found: int(r.Found), Watch: int(r.Watch)}
	}
	return out, nil
}

// RoundNumbers numbers the rounds of each flow from 1 by id, oldest first; runs need
// hold every round of the flows they cover.
func RoundNumbers(runs []database.GrowthRun) map[int64]int {
	sorted := slices.SortedFunc(slices.Values(runs), func(a, b database.GrowthRun) int { return cmp.Compare(a.ID, b.ID) })
	rounds := make(map[int64]int, len(runs))
	perFlow := make(map[int64]int)
	for _, r := range sorted {
		perFlow[r.FlowID.Int64]++
		rounds[r.ID] = perFlow[r.FlowID.Int64]
	}
	return rounds
}

func (s *Service) ListRuns(ctx context.Context, userID int64, flowID *int64) ([]database.GrowthRun, error) {
	params := database.GetUserGrowthRunsParams{UserID: userID}
	if flowID != nil {
		params.FlowID = sql.NullInt64{Int64: *flowID, Valid: true}
	}
	runs, err := s.db.GetUserGrowthRuns(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list rounds: %w", err)
	}
	return runs, nil
}
