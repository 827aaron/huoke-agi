package controller

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth"
	"pentagi/pkg/providers"
	"pentagi/pkg/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resumeFakeQuerier is the subtask row, its tool calls and the writes Run makes.
type resumeFakeQuerier struct {
	database.Querier

	subtask  database.Subtask
	calls    []database.Toolcall
	callsErr error
	results  []string
	statuses []database.SubtaskStatus
}

func (q *resumeFakeQuerier) GetSubtask(ctx context.Context, id int64) (database.Subtask, error) {
	if err := ctx.Err(); err != nil {
		return database.Subtask{}, err
	}
	got := q.subtask
	got.ID = id
	return got, nil
}

func (q *resumeFakeQuerier) UpdateSubtaskResult(
	ctx context.Context, arg database.UpdateSubtaskResultParams,
) (database.Subtask, error) {
	if err := ctx.Err(); err != nil {
		return database.Subtask{}, err
	}
	q.results = append(q.results, arg.Result)
	q.subtask.Result = arg.Result
	return q.subtask, nil
}

func (q *resumeFakeQuerier) UpdateSubtaskStatus(
	ctx context.Context, arg database.UpdateSubtaskStatusParams,
) (database.Subtask, error) {
	if err := ctx.Err(); err != nil {
		return database.Subtask{}, err
	}
	q.statuses = append(q.statuses, arg.Status)
	q.subtask.Status = arg.Status
	return q.subtask, nil
}

func (q *resumeFakeQuerier) GetSubtaskToolcalls(ctx context.Context, _ sql.NullInt64) ([]database.Toolcall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.callsErr != nil {
		return nil, q.callsErr
	}
	return q.calls, nil
}

func (q *resumeFakeQuerier) GetTaskSubtasks(context.Context, int64) ([]database.Subtask, error) {
	return []database.Subtask{q.subtask}, nil
}

func (q *resumeFakeQuerier) DeleteSubtasks(ctx context.Context, _ []int64) error {
	return ctx.Err()
}

func (q *resumeFakeQuerier) CreateSubtask(
	ctx context.Context, arg database.CreateSubtaskParams,
) (database.Subtask, error) {
	if err := ctx.Err(); err != nil {
		return database.Subtask{}, err
	}
	created := database.Subtask{
		ID: q.subtask.ID + 1, TaskID: arg.TaskID, Status: arg.Status,
		Title: arg.Title, Description: arg.Description,
	}
	q.subtask = created
	return created, nil
}

type resumeFakeProvider struct {
	providers.FlowProvider

	result providers.PerformResult
	err    error
	plan   []tools.SubtaskInfo
}

func (resumeFakeProvider) EnsureChainConsistency(context.Context, int64) error { return nil }

func (p resumeFakeProvider) PerformAgentChain(context.Context, int64, int64, int64) (providers.PerformResult, error) {
	return p.result, p.err
}

func (p resumeFakeProvider) RefineSubtasks(context.Context, int64) ([]tools.SubtaskInfo, error) {
	return p.plan, nil
}

type resumeFakeGrowth struct {
	cursors     []growth.SourceCursor
	cursorsErr  error
	progress    growth.Progress
	progressErr error
	asked       [][]int64
}

func (g *resumeFakeGrowth) SourceCursors(ctx context.Context, _ int64, ids []int64) ([]growth.SourceCursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.asked = append(g.asked, append([]int64(nil), ids...))
	if g.cursorsErr != nil {
		return nil, g.cursorsErr
	}
	if len(ids) == 0 {
		return g.cursors, nil
	}
	var out []growth.SourceCursor
	for _, id := range ids {
		for _, c := range g.cursors {
			if c.SourceID == id {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (g *resumeFakeGrowth) ProgressSince(ctx context.Context, _ growth.Scope, _ time.Time) (growth.Progress, error) {
	if err := ctx.Err(); err != nil {
		return growth.Progress{}, err
	}
	return g.progress, g.progressErr
}

type resumeTaskUpdater struct {
	status database.TaskStatus
}

func (u *resumeTaskUpdater) SetStatus(ctx context.Context, status database.TaskStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	u.status = status
	return nil
}

func resumeWorker(q *resumeFakeQuerier, p resumeFakeProvider, g *resumeFakeGrowth) *subtaskWorker {
	return &subtaskWorker{
		mx: &sync.RWMutex{},
		subtaskCtx: &SubtaskContext{
			MsgChainID: 9,
			SubtaskID:  72,
			TaskContext: TaskContext{
				TaskID: 5,
				FlowContext: FlowContext{
					DB: q, UserID: 3, FlowID: 7, Provider: p, Growth: g,
				},
			},
		},
		updater: &resumeTaskUpdater{},
	}
}

func TestSubtask_Run_WritesTheReadCursorWhenTheAgentFails(t *testing.T) {
	readAt := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	call := database.Toolcall{
		Name:      tools.GrowthSyncSourceToolName,
		Args:      []byte(`{"action":"history","source_id":12,"message":"reading the group"}`),
		CreatedAt: sql.NullTime{Time: readAt, Valid: true},
	}
	cursor := growth.SourceCursor{
		SourceID: 12, Title: "RPG Fans", NewestMessageID: 48213, OldestMessageID: 47001,
		MessagesRead: 1212, SyncedAt: readAt,
	}
	q := &resumeFakeQuerier{
		subtask: database.Subtask{ID: 72, Result: "read until the vendor timed out\nPROGRESS: new_leads_this_task=4 target=50"},
		calls:   []database.Toolcall{call},
	}
	g := &resumeFakeGrowth{
		cursors:  []growth.SourceCursor{cursor},
		progress: growth.Progress{NewLeads: 4, Watchlist: 2, DeliveredThisRun: 1},
	}

	err := resumeWorker(q, resumeFakeProvider{result: providers.PerformResultError}, g).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []database.SubtaskStatus{database.SubtaskStatusRunning, database.SubtaskStatusFailed}, q.statuses)
	require.NotEmpty(t, q.results)
	state, ok := ParseResumeState(q.results[len(q.results)-1])
	require.True(t, ok, "the failed result must carry a parseable resume line: %s", q.subtask.Result)
	assert.Contains(t, q.subtask.Result, "read until the vendor timed out")
	assert.Equal(t, ResumeState{
		SubtaskID: 72, Status: resumeStatusFailed, Reason: "read until the vendor timed out\nPROGRESS: new_leads_this_task=4 target=50",
		LeadsSaved: 5, Sources: []growth.SourceCursor{cursor},
	}, state)
	require.Len(t, g.asked, 1)
	assert.Equal(t, []int64{12}, g.asked[0], "the cursor is read for the source this subtask actually synced")
}

func TestSubtask_Run_WritesTheReadCursorWhenTheRunIsInterrupted(t *testing.T) {
	readAt := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	q := &resumeFakeQuerier{
		calls: []database.Toolcall{{
			Name:      tools.GrowthSyncSourceToolName,
			Args:      []byte(`{"source_ids":[12],"message":"reading"}`),
			CreatedAt: sql.NullTime{Time: readAt, Valid: true},
		}},
	}
	g := &resumeFakeGrowth{cursors: []growth.SourceCursor{{
		SourceID: 12, Title: "RPG Fans", NewestMessageID: 48213, SyncedAt: readAt,
	}}}

	err := resumeWorker(q, resumeFakeProvider{err: errors.New("vendor timed out after 9.8h")}, g).Run(context.Background())

	require.ErrorContains(t, err, "vendor timed out after 9.8h")
	assert.Equal(t, database.SubtaskStatusWaiting, q.statuses[len(q.statuses)-1])
	state, ok := ParseResumeState(q.subtask.Result)
	require.True(t, ok)
	assert.Equal(t, resumeStatusInterrupted, state.Status)
	assert.Equal(t, "vendor timed out after 9.8h", state.Reason)
	require.Len(t, state.Sources, 1)
	assert.Equal(t, int64(48213), state.Sources[0].NewestMessageID)
}

func TestSubtask_Run_RecordsNothingWhenTheSubtaskReadNoSource(t *testing.T) {
	q := &resumeFakeQuerier{subtask: database.Subtask{ID: 72, Result: "search found nothing"}}
	g := &resumeFakeGrowth{}

	err := resumeWorker(q, resumeFakeProvider{result: providers.PerformResultError}, g).Run(context.Background())

	require.NoError(t, err)
	assert.Empty(t, q.results)
	assert.Equal(t, "search found nothing", q.subtask.Result)
	_, ok := ParseResumeState(q.subtask.Result)
	assert.False(t, ok)
}

func TestSubtask_RefineSubtasks_CarriesTheDatabaseCursorIntoTheNextDescription(t *testing.T) {
	readAt := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	stale := `{"subtask_id":72,"status":"failed","reason":"vendor timed out","leads_saved":3,"sources":[{"source_id":12,"title":"RPG Fans","newest_message_id":1000,"oldest_message_id":100,"messages_read":10,"synced_at":"2026-10-18T09:00:00Z"}]}`
	q := &resumeFakeQuerier{subtask: database.Subtask{
		ID: 72, Status: database.SubtaskStatusFailed,
		Result: "stopped after 9.8h\n" + resumeStatePrefix + stale,
	}}
	g := &resumeFakeGrowth{cursors: []growth.SourceCursor{{
		SourceID: 12, Title: "RPG Fans", NewestMessageID: 48213, OldestMessageID: 47001,
		MessagesRead: 1212, SyncedAt: readAt,
	}}}
	stc := NewSubtaskController(&TaskContext{
		TaskID: 5,
		FlowContext: FlowContext{
			DB: q, UserID: 3,
			Provider: resumeFakeProvider{plan: []tools.SubtaskInfo{{
				Title: "keep reading the group", Description: "Read the next page of source 12.",
			}}},
			Growth: g,
		},
	})

	require.NoError(t, stc.RefineSubtasks(context.Background()))

	assert.Equal(t, "keep reading the group", q.subtask.Title)
	assert.Contains(t, q.subtask.Description, "Read the next page of source 12.")
	assert.Contains(t, q.subtask.Description, "RESUME FROM FAILED SUBTASK #72")
	assert.Contains(t, q.subtask.Description, "continue reading after message_id 48213")
	assert.Contains(t, q.subtask.Description, "Do not read this source from the beginning.")
	assert.NotContains(t, q.subtask.Description, "message_id 1000", "the next subtask resumes from the database cursor, not the stale copy in the failed result")
	require.Len(t, g.asked, 1)
	assert.Equal(t, []int64{12}, g.asked[0])
	assert.False(t, slices.Contains(q.statuses, database.SubtaskStatusFailed))
}
