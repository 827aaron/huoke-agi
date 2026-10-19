package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/providers"
	"pentagi/pkg/tools"

	"github.com/sirupsen/logrus"
)

type TaskUpdater interface {
	SetStatus(ctx context.Context, status database.TaskStatus) error
}

type SubtaskWorker interface {
	GetMsgChainID() int64
	GetSubtaskID() int64
	GetTaskID() int64
	GetFlowID() int64
	GetUserID() int64
	GetTitle() string
	GetDescription() string
	IsCompleted() bool
	IsWaiting() bool
	GetStatus(ctx context.Context) (database.SubtaskStatus, error)
	SetStatus(ctx context.Context, status database.SubtaskStatus) error
	GetResult(ctx context.Context) (string, error)
	SetResult(ctx context.Context, result string) error
	PutInput(ctx context.Context, input string) error
	Run(ctx context.Context) error
	Finish(ctx context.Context) error
}

type subtaskWorker struct {
	mx         *sync.RWMutex
	subtaskCtx *SubtaskContext
	updater    TaskUpdater
	completed  bool
	waiting    bool
}

func NewSubtaskWorker(
	ctx context.Context,
	taskCtx *TaskContext,
	id int64,
	title,
	description string,
	updater TaskUpdater,
) (SubtaskWorker, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "controller.NewSubtaskWorker")
	defer span.End()

	msgChainID, err := taskCtx.Provider.PrepareAgentChain(ctx, taskCtx.TaskID, id)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare primary agent chain for subtask %d: %w", id, err)
	}

	return &subtaskWorker{
		mx: &sync.RWMutex{},
		subtaskCtx: &SubtaskContext{
			MsgChainID:         msgChainID,
			SubtaskID:          id,
			SubtaskTitle:       title,
			SubtaskDescription: description,
			TaskContext:        *taskCtx,
		},
		updater:   updater,
		completed: false,
		waiting:   false,
	}, nil
}

func LoadSubtaskWorker(
	ctx context.Context,
	subtask database.Subtask,
	taskCtx *TaskContext,
	updater TaskUpdater,
) (SubtaskWorker, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "controller.LoadSubtaskWorker")
	defer span.End()

	var completed, waiting bool
	switch subtask.Status {
	case database.SubtaskStatusFinished, database.SubtaskStatusFailed:
		completed = true
	case database.SubtaskStatusWaiting:
		waiting = true
	case database.SubtaskStatusRunning:
		var err error
		// if subtask is running, it means that it was not finished by previous run
		// so we need to set it to created and continue from the beginning
		subtask, err = taskCtx.DB.UpdateSubtaskStatus(ctx, database.UpdateSubtaskStatusParams{
			Status: database.SubtaskStatusCreated,
			ID:     subtask.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update subtask %d status to created: %w", subtask.ID, err)
		}
	case database.SubtaskStatusCreated:
		return nil, fmt.Errorf("subtask %d has created yet: %w", subtask.ID, ErrNothingToLoad)
	default:
		return nil, fmt.Errorf("unexpected subtask status: %s", subtask.Status)
	}

	msgChains, err := taskCtx.DB.GetSubtaskPrimaryMsgChains(ctx, database.Int64ToNullInt64(&subtask.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to get subtask primary msg chains for subtask %d: %w", subtask.ID, err)
	}

	if len(msgChains) == 0 {
		return nil, fmt.Errorf("subtask %d has no msg chains: %w", subtask.ID, ErrNothingToLoad)
	}

	return &subtaskWorker{
		mx: &sync.RWMutex{},
		subtaskCtx: &SubtaskContext{
			MsgChainID:         msgChains[0].ID,
			SubtaskID:          subtask.ID,
			SubtaskTitle:       subtask.Title,
			SubtaskDescription: subtask.Description,
			TaskContext:        *taskCtx,
		},
		updater:   updater,
		completed: completed,
		waiting:   waiting,
	}, nil
}

func (stw *subtaskWorker) GetMsgChainID() int64 {
	return stw.subtaskCtx.MsgChainID
}

func (stw *subtaskWorker) GetSubtaskID() int64 {
	return stw.subtaskCtx.SubtaskID
}

func (stw *subtaskWorker) GetTaskID() int64 {
	return stw.subtaskCtx.TaskID
}

func (stw *subtaskWorker) GetFlowID() int64 {
	return stw.subtaskCtx.FlowID
}

func (stw *subtaskWorker) GetUserID() int64 {
	return stw.subtaskCtx.UserID
}

func (stw *subtaskWorker) GetTitle() string {
	return stw.subtaskCtx.SubtaskTitle
}

func (stw *subtaskWorker) GetDescription() string {
	return stw.subtaskCtx.SubtaskDescription
}

func (stw *subtaskWorker) IsCompleted() bool {
	stw.mx.RLock()
	defer stw.mx.RUnlock()

	return stw.completed
}

func (stw *subtaskWorker) IsWaiting() bool {
	stw.mx.RLock()
	defer stw.mx.RUnlock()

	return stw.waiting
}

func (stw *subtaskWorker) GetStatus(ctx context.Context) (database.SubtaskStatus, error) {
	subtask, err := stw.subtaskCtx.DB.GetSubtask(ctx, stw.subtaskCtx.SubtaskID)
	if err != nil {
		return database.SubtaskStatusFailed, err
	}

	return subtask.Status, nil
}

func (stw *subtaskWorker) SetStatus(ctx context.Context, status database.SubtaskStatus) error {
	_, err := stw.subtaskCtx.DB.UpdateSubtaskStatus(ctx, database.UpdateSubtaskStatusParams{
		Status: status,
		ID:     stw.subtaskCtx.SubtaskID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Replacement can delete the row before this stale worker finishes.
			// Treat it as complete to keep task shutdown idempotent.
			logrus.WithContext(ctx).WithFields(logrus.Fields{
				"subtask_id":       stw.subtaskCtx.SubtaskID,
				"requested_status": status,
			}).Warn("subtask no longer exists in the database, treating as already finished")

			stw.mx.Lock()
			stw.completed = true
			stw.waiting = false
			stw.mx.Unlock()

			return nil
		}

		return fmt.Errorf("failed to set subtask %d status: %w", stw.subtaskCtx.SubtaskID, err)
	}

	stw.mx.Lock()
	defer stw.mx.Unlock()

	switch status {
	case database.SubtaskStatusRunning:
		stw.completed = false
		stw.waiting = false
		err = stw.updater.SetStatus(ctx, database.TaskStatusRunning)
	case database.SubtaskStatusWaiting:
		stw.completed = false
		stw.waiting = true
		err = stw.updater.SetStatus(ctx, database.TaskStatusWaiting)
	case database.SubtaskStatusFinished, database.SubtaskStatusFailed:
		stw.completed = true
		stw.waiting = false
		// statuses Finished and Failed will be produced by stack from Run function call
	default:
		// status Created is not possible to set by this call
		return fmt.Errorf("unsupported subtask status: %s", status)
	}
	if err != nil {
		return fmt.Errorf("failed to set task status in back propagation: %w", err)
	}

	return nil
}

func (stw *subtaskWorker) GetResult(ctx context.Context) (string, error) {
	subtask, err := stw.subtaskCtx.DB.GetSubtask(ctx, stw.subtaskCtx.SubtaskID)
	if err != nil {
		return "", err
	}

	return subtask.Result, nil
}

func (stw *subtaskWorker) SetResult(ctx context.Context, result string) error {
	_, err := stw.subtaskCtx.DB.UpdateSubtaskResult(ctx, database.UpdateSubtaskResultParams{
		Result: result,
		ID:     stw.subtaskCtx.SubtaskID,
	})
	if err != nil {
		return fmt.Errorf("failed to set subtask %d result: %w", stw.subtaskCtx.SubtaskID, err)
	}

	return nil
}

func (stw *subtaskWorker) PutInput(ctx context.Context, input string) error {
	if stw.IsCompleted() {
		return fmt.Errorf("subtask has already completed")
	}

	if !stw.IsWaiting() {
		return fmt.Errorf("subtask is not waiting, run first")
	}

	err := stw.subtaskCtx.Provider.PutInputToAgentChain(ctx, stw.subtaskCtx.MsgChainID, input)
	if err != nil {
		return fmt.Errorf("failed to put input for subtask %d: %w", stw.subtaskCtx.SubtaskID, err)
	}

	_, err = stw.subtaskCtx.MsgLog.PutSubtaskMsg(
		ctx,
		database.MsglogTypeInput,
		stw.subtaskCtx.TaskID,
		stw.subtaskCtx.SubtaskID,
		"", // thinking is empty because this is input
		input,
	)
	if err != nil {
		return fmt.Errorf("failed to put input for subtask %d: %w", stw.subtaskCtx.SubtaskID, err)
	}

	stw.mx.Lock()
	defer stw.mx.Unlock()

	stw.waiting = false

	return nil
}

func (stw *subtaskWorker) Run(ctx context.Context) error {
	if stw.IsCompleted() {
		return fmt.Errorf("subtask has already completed")
	}

	if stw.IsWaiting() {
		return fmt.Errorf("subtask is waiting, put input first")
	}

	if err := stw.SetStatus(ctx, database.SubtaskStatusRunning); err != nil {
		stw.handleInterrupting(err)
		return err
	}

	var (
		taskID     = stw.subtaskCtx.TaskID
		subtaskID  = stw.subtaskCtx.SubtaskID
		msgChainID = stw.subtaskCtx.MsgChainID
	)

	if err := stw.subtaskCtx.Provider.EnsureChainConsistency(ctx, msgChainID); err != nil {
		stw.handleInterrupting(err)
		return fmt.Errorf("failed to ensure chain consistency for subtask %d: %w", subtaskID, err)
	}

	performResult, err := stw.subtaskCtx.Provider.PerformAgentChain(ctx, taskID, subtaskID, msgChainID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			ctx = context.Background()
		}
		errChainConsistency := stw.subtaskCtx.Provider.EnsureChainConsistency(ctx, msgChainID)
		if errChainConsistency != nil {
			err = errors.Join(err, errChainConsistency)
		}
		stw.recordResumeState(ctx, resumeStatusInterrupted, err.Error())
		_ = stw.SetStatus(ctx, database.SubtaskStatusWaiting)
		return fmt.Errorf("failed to perform agent chain for subtask %d: %w", subtaskID, err)
	}

	switch performResult {
	case providers.PerformResultWaiting:
		if err := stw.SetStatus(ctx, database.SubtaskStatusWaiting); err != nil {
			stw.handleInterrupting(err)
			return err
		}
	case providers.PerformResultDone:
		if err := stw.SetStatus(ctx, database.SubtaskStatusFinished); err != nil {
			stw.handleInterrupting(err)
			return fmt.Errorf("failed to set subtask %d status to finished: %w", subtaskID, err)
		}
	case providers.PerformResultError:
		stw.recordResumeState(ctx, resumeStatusFailed, "")
		if err := stw.SetStatus(ctx, database.SubtaskStatusFailed); err != nil {
			stw.handleInterrupting(err)
			return fmt.Errorf("failed to set subtask %d status to failed: %w", subtaskID, err)
		}
	default:
		return fmt.Errorf("unknown perform result: %d", performResult)
	}

	return nil
}

// handleInterrupting sets this subtask (and task/flow via SetStatus back-propagation)
// to Waiting when err is context.Canceled or context.DeadlineExceeded. Use after the subtask
// was advanced past Waiting (e.g. Running) but the run aborts before PerformAgentChain's
// normal error handler, or when a late SetStatus fails with a context interruption.
func (stw *subtaskWorker) handleInterrupting(err error) {
	if err == nil {
		return
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if stw.IsCompleted() {
		return
	}

	resetCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if errSt := stw.SetStatus(resetCtx, database.SubtaskStatusWaiting); errSt != nil {
		logrus.WithError(errSt).Warn("failed to set subtask waiting after run interrupt")
	}
}

func (stw *subtaskWorker) Finish(ctx context.Context) error {
	if stw.IsCompleted() {
		return fmt.Errorf("subtask has already completed")
	}

	if err := stw.SetStatus(ctx, database.SubtaskStatusFinished); err != nil {
		return err
	}

	return nil
}

const (
	resumeStatePrefix       = "RESUME_STATE: "
	resumeStatusFailed      = "failed"
	resumeStatusInterrupted = "interrupted"
	resumeMaxSources        = 20
	resumeMaxReasonRunes    = 300
	resumeStateTimeout      = 15 * time.Second
)

// ResumeSource is the slice of growth.Service that hands a failed subtask's reading
// position to the subtask planned in its place.
type ResumeSource interface {
	SourceCursors(ctx context.Context, userID int64, sourceIDs []int64) ([]growth.SourceCursor, error)
	ProgressSince(ctx context.Context, scope growth.Scope, since time.Time) (growth.Progress, error)
}

// resumeSource is assigned only when set: a nil *growth.Service in the interface would read as available.
func resumeSource(cfg *config.Config) ResumeSource {
	if svc := growth.Default(); cfg.GrowthEnabled && svc != nil {
		return svc
	}
	return nil
}

// ResumeState is the last line of a failed or interrupted subtask's result:
// "RESUME_STATE: " and one JSON object, so it survives in the refiner's view of the
// result and parses back with ParseResumeState.
type ResumeState struct {
	SubtaskID int64  `json:"subtask_id"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	// LeadsSaved counts the qualified and watchlist leads first saved during the
	// subtask that are not delivered yet.
	LeadsSaved     int                   `json:"leads_saved"`
	Sources        []growth.SourceCursor `json:"sources"`
	SourcesOmitted int                   `json:"sources_omitted,omitempty"`
}

func ParseResumeState(result string) (ResumeState, bool) {
	lines := strings.Split(result, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		raw, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), resumeStatePrefix)
		if !ok {
			continue
		}
		var state ResumeState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return ResumeState{}, false
		}
		return state, true
	}
	return ResumeState{}, false
}

// recordResumeState never fails the subtask: losing the resume line costs a re-plan
// its hint, not the run.
func (stw *subtaskWorker) recordResumeState(ctx context.Context, status, reason string) {
	if stw.subtaskCtx.Growth == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resumeStateTimeout)
	defer cancel()

	logger := logrus.WithContext(ctx).WithField("subtask_id", stw.subtaskCtx.SubtaskID)
	result, err := stw.GetResult(ctx)
	if err != nil {
		logger.WithError(err).Warn("failed to read the subtask result to record its resume state")
		return
	}
	if reason == "" {
		reason = stripResumeState(result)
	}
	state, ok, err := stw.resumeState(ctx, status, reason)
	if err != nil {
		logger.WithError(err).Warn("failed to collect the resume state of the subtask")
		return
	}
	if !ok {
		return
	}

	var line strings.Builder
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(state); err != nil {
		logger.WithError(err).Warn("failed to encode the resume state of the subtask")
		return
	}
	body := stripResumeState(result)
	if body != "" {
		body += "\n\n"
	}
	if err := stw.SetResult(ctx, body+resumeStatePrefix+strings.TrimSpace(line.String())); err != nil {
		logger.WithError(err).Warn("failed to record the resume state of the subtask")
	}
}

// resumeState reports false when the subtask read no source: there is nothing to resume.
func (stw *subtaskWorker) resumeState(ctx context.Context, status, reason string) (ResumeState, bool, error) {
	sc := stw.subtaskCtx
	calls, err := sc.DB.GetSubtaskToolcalls(ctx, database.Int64ToNullInt64(&sc.SubtaskID))
	if err != nil {
		return ResumeState{}, false, fmt.Errorf("failed to list the tool calls of subtask %d: %w", sc.SubtaskID, err)
	}

	var (
		ids     []int64
		readAll bool
		started time.Time
	)
	for _, call := range calls {
		if call.CreatedAt.Valid && (started.IsZero() || call.CreatedAt.Time.Before(started)) {
			started = call.CreatedAt.Time
		}
		if call.Name != tools.GrowthSyncSourceToolName {
			continue
		}
		var action tools.GrowthSyncSourceAction
		if err := json.Unmarshal(call.Args, &action); err != nil {
			continue
		}
		if a := action.Action.String(); a != "" && a != "history" {
			continue
		}
		named := make([]int64, 0, len(action.SourceIDs)+1)
		for _, id := range action.SourceIDs {
			named = append(named, int64(id))
		}
		if len(named) == 0 && action.SourceID.Int64() > 0 {
			named = append(named, action.SourceID.Int64())
		}
		if len(named) == 0 {
			readAll = true
		}
		for _, id := range named {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 && !readAll {
		return ResumeState{}, false, nil
	}

	query := ids
	if readAll {
		// A sync without ids read every joined source; the ones it reached carry a later sync time.
		query = nil
	}
	cursors, err := sc.Growth.SourceCursors(ctx, sc.UserID, query)
	if err != nil {
		return ResumeState{}, false, err
	}
	if readAll {
		cursors = slices.DeleteFunc(cursors, func(c growth.SourceCursor) bool {
			return c.SyncedAt.Before(started) && !slices.Contains(ids, c.SourceID)
		})
	}

	state := ResumeState{
		SubtaskID: sc.SubtaskID,
		Status:    status,
		Reason:    truncateRunes(strings.TrimSpace(reason), resumeMaxReasonRunes),
		Sources:   cursors,
	}
	if state.Sources == nil {
		state.Sources = []growth.SourceCursor{}
	}
	if n := len(state.Sources); n > resumeMaxSources {
		state.Sources, state.SourcesOmitted = state.Sources[:resumeMaxSources], n-resumeMaxSources
	}

	taskID := sc.TaskID
	p, err := sc.Growth.ProgressSince(ctx, growth.Scope{UserID: sc.UserID, FlowID: sc.FlowID, TaskID: &taskID}, started)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("subtask_id", sc.SubtaskID).
			Warn("failed to count the leads of the subtask for its resume state")
	} else {
		state.LeadsSaved = p.NewLeads - p.DeliveredThisRun + p.Watchlist
	}

	return state, true, nil
}

func stripResumeState(result string) string {
	lines := strings.Split(result, "\n")
	lines = slices.DeleteFunc(lines, func(l string) bool {
		return strings.HasPrefix(strings.TrimSpace(l), resumeStatePrefix)
	})
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// resumeDirective is the block appended to every subtask the refiner plans right after a
// failed one, so the next subtask continues each source rather than relying on the model
// to copy message ids. Positions are re-read from the database when it is reachable: the
// ones in the failed result are behind if anything read the source since.
func (tc *TaskContext) resumeDirective(ctx context.Context, subtasks []database.Subtask) string {
	var last *database.Subtask
	for i := range subtasks {
		st := &subtasks[i]
		if st.Status != database.SubtaskStatusFinished && st.Status != database.SubtaskStatusFailed {
			continue
		}
		if last == nil || st.ID > last.ID {
			last = st
		}
	}
	if last == nil || last.Status != database.SubtaskStatusFailed {
		return ""
	}
	state, ok := ParseResumeState(last.Result)
	if !ok || len(state.Sources) == 0 {
		return ""
	}

	cursors := state.Sources
	if tc.Growth != nil {
		ids := make([]int64, 0, len(cursors))
		for _, c := range cursors {
			ids = append(ids, c.SourceID)
		}
		fresh, err := tc.Growth.SourceCursors(ctx, tc.UserID, ids)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("task_id", tc.TaskID).
				Warn("failed to re-read source cursors; resuming from the failed subtask's copy")
		} else if len(fresh) > 0 {
			cursors = fresh
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "RESUME FROM FAILED SUBTASK #%d: continue, do not start over. "+
		"The system keeps each source's read position and %s continues from it; "+
		"do not search for, add, join or re-read these sources from the beginning.\n",
		state.SubtaskID, tools.GrowthSyncSourceToolName)
	for _, c := range cursors {
		fmt.Fprintf(&b, "- source_id=%d %q: ", c.SourceID, c.Title)
		switch {
		case c.NewestMessageID == 0:
			b.WriteString("not read yet\n")
		case c.HistoryExhausted:
			fmt.Fprintf(&b, "back-read is done; continue reading after message_id %d with direction=newer only "+
				"(%d messages already read). Do not read this source from the beginning.\n",
				c.NewestMessageID, c.MessagesRead)
		default:
			fmt.Fprintf(&b, "continue reading after message_id %d (direction=newer), and before message_id %d "+
				"(direction=older); %d messages already read. Do not read this source from the beginning.\n",
				c.NewestMessageID, c.OldestMessageID, c.MessagesRead)
		}
	}
	if state.SourcesOmitted > 0 {
		fmt.Fprintf(&b, "- %d more sources it read keep their positions too\n", state.SourcesOmitted)
	}
	fmt.Fprintf(&b, "Subtask #%d saved %d leads before it %s", state.SubtaskID, state.LeadsSaved, state.Status)
	if state.Reason != "" {
		fmt.Fprintf(&b, ": %s", state.Reason)
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
