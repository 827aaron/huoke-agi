package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/growth"
	"pentagi/pkg/growth/connectors"
)

// GrowthBackend is the slice of growth.Service the lead-generation tools use.
type GrowthBackend interface {
	SearchSources(ctx context.Context, scope growth.Scope, platform connectors.Platform, query string, limit int) ([]growth.SourceView, error)
	RecommendSources(ctx context.Context, scope growth.Scope, sourceID int64, limit int) ([]growth.SourceView, error)
	AddSources(ctx context.Context, scope growth.Scope, links []string, origin string, discoveredFrom *int64) ([]growth.AddSourceResult, error)
	JoinSources(ctx context.Context, scope growth.Scope, sourceIDs []int64) ([]growth.JoinOutcome, error)
	SyncSources(ctx context.Context, scope growth.Scope, sourceIDs []int64, opt growth.SyncOptions) ([]growth.SyncResult, error)
	GetUser(ctx context.Context, scope growth.Scope, platform connectors.Platform, userID string) (connectors.UserProfile, error)
	UpsertLeads(ctx context.Context, scope growth.Scope, leads []growth.LeadInput) ([]growth.LeadResult, error)
	Progress(ctx context.Context, scope growth.Scope) (growth.Progress, error)
	SetTarget(ctx context.Context, scope growth.Scope, target int) (growth.Progress, error)
	Deliver(ctx context.Context, scope growth.Scope, allowPartial bool) (growth.Delivery, error)
	ListLeads(ctx context.Context, userID int64, filter growth.LeadFilter) ([]growth.LeadView, error)
}

// growthTools are the lead-generation tools, bound per role in tools.go: the searcher
// writes the source pool, the lead researcher (the pentester role) reads sources and
// writes leads, the primary agent delivers, and every planner or reporter reads progress.
type growthTools struct {
	backend GrowthBackend
	scope   growth.Scope
}

func newGrowthTools(backend GrowthBackend, userID, flowID int64, taskID *int64) *growthTools {
	return &growthTools{backend: backend, scope: growth.Scope{UserID: userID, FlowID: flowID, TaskID: taskID}}
}

func (g *growthTools) IsAvailable() bool { return g.backend != nil }

func (g *growthTools) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case GrowthSearchSourcesToolName:
		return g.searchSources(ctx, args)
	case GrowthAddSourcesToolName:
		return g.addSources(ctx, args)
	case GrowthJoinSourceToolName:
		return g.joinSource(ctx, args)
	case GrowthSyncSourceToolName:
		return g.syncSource(ctx, args)
	case GrowthUpsertLeadsToolName:
		return g.upsertLeads(ctx, args)
	case GrowthGetProgressToolName:
		return g.getProgress(ctx, args)
	case GrowthDeliverLeadsToolName:
		return g.deliverLeads(ctx, args)
	case GrowthListLeadsToolName:
		return g.listLeads(ctx, args)
	default:
		return "", fmt.Errorf("unknown growth tool: %s", name)
	}
}

func (g *growthTools) searchSources(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthSearchSourcesAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthSearchSourcesToolName, err)
	}

	limit := action.Limit.Int()
	switch action.Mode {
	case "", "search":
		if strings.TrimSpace(action.Query) == "" {
			return "", errors.New("query is required for mode=search")
		}
		found, err := g.backend.SearchSources(ctx, g.scope, connectors.PlatformTelegram, action.Query, limit)
		if err != nil {
			return g.failure(ctx, err)
		}
		return g.respond(ctx, map[string]any{"mode": "search", "query": action.Query, "sources": found})
	case "resolve":
		if len(action.Links) == 0 {
			return "", errors.New("links is required for mode=resolve")
		}
		added, err := g.backend.AddSources(ctx, g.scope, action.Links, growth.OriginAI, nil)
		if err != nil {
			return g.failure(ctx, err)
		}
		return g.respond(ctx, map[string]any{"mode": "resolve", "results": added})
	case "recommend":
		if action.SourceID.Int64() <= 0 {
			return "", errors.New("source_id is required for mode=recommend")
		}
		found, err := g.backend.RecommendSources(ctx, g.scope, action.SourceID.Int64(), limit)
		if err != nil {
			return g.failure(ctx, err)
		}
		return g.respond(ctx, map[string]any{"mode": "recommend", "similar_to": action.SourceID.Int64(), "sources": found})
	default:
		return "", fmt.Errorf("unknown mode %q: use search, resolve or recommend", action.Mode)
	}
}

func (g *growthTools) addSources(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthAddSourcesAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthAddSourcesToolName, err)
	}
	if len(action.Links) == 0 {
		return "", errors.New("links is required: one or more Telegram links, @usernames or web page URLs")
	}
	// Links a researcher found inside a source's messages are the AI's discovery, not the customer's.
	origin, from := growth.OriginManual, action.DiscoveredFromSourceID.PtrInt64()
	if from != nil && *from <= 0 {
		from = nil
	}
	if from != nil {
		origin = growth.OriginAI
	}
	added, err := g.backend.AddSources(ctx, g.scope, action.Links, origin, from)
	if err != nil {
		return g.failure(ctx, err)
	}
	return g.respond(ctx, map[string]any{"results": added})
}

func (g *growthTools) joinSource(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthJoinSourceAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthJoinSourceToolName, err)
	}
	if len(action.SourceIDs) == 0 {
		return "", errors.New("source_ids is required")
	}
	ids := make([]int64, 0, len(action.SourceIDs))
	for _, id := range action.SourceIDs {
		ids = append(ids, int64(id))
	}
	outcomes, err := g.backend.JoinSources(ctx, g.scope, ids)
	if err != nil && len(outcomes) == 0 {
		return g.failure(ctx, err)
	}
	payload := map[string]any{"results": outcomes}
	if err != nil {
		payload["stopped"] = growthFailureText(err)
	}
	return g.respond(ctx, payload)
}

func (g *growthTools) syncSource(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthSyncSourceAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthSyncSourceToolName, err)
	}

	switch action.Action {
	case "", "history":
		ids := make([]int64, 0, len(action.SourceIDs)+1)
		for _, id := range action.SourceIDs {
			ids = append(ids, int64(id))
		}
		if len(ids) == 0 && action.SourceID.Int64() > 0 {
			ids = append(ids, action.SourceID.Int64())
		}
		if len(ids) == 0 {
			return "", errors.New("source_id or source_ids is required for action=history")
		}
		opt := growth.SyncOptions{Direction: action.Direction.String(), Limit: action.Limit.Int(), All: action.AllMessages.Bool()}
		results, err := g.backend.SyncSources(ctx, g.scope, ids, opt)
		if len(ids) == 1 {
			if len(results) == 1 && results[0].Err != nil {
				err = results[0].Err
			}
			if err != nil || len(results) != 1 {
				return g.failure(ctx, err)
			}
			return g.respond(ctx, syncPayload(results[0]))
		}
		if err != nil && len(results) == 0 {
			return g.failure(ctx, err)
		}
		items := make([]map[string]any, 0, len(results))
		for _, res := range results {
			if res.Err != nil {
				items = append(items, map[string]any{"source_id": res.Source.ID, "error": growthFailureText(res.Err)})
				continue
			}
			items = append(items, syncPayload(res))
		}
		payload := map[string]any{"results": items}
		if err != nil {
			payload["stopped"] = growthFailureText(err)
		}
		return g.respond(ctx, payload)
	case "user":
		if strings.TrimSpace(action.PlatformUserID.String()) == "" {
			return "", errors.New("platform_user_id is required for action=user")
		}
		profile, err := g.backend.GetUser(ctx, g.scope, connectors.PlatformTelegram, action.PlatformUserID.String())
		if err != nil {
			return g.failure(ctx, err)
		}
		return g.respond(ctx, map[string]any{"user": profile})
	default:
		return "", fmt.Errorf("unknown action %q: use history or user", action.Action)
	}
}

func syncPayload(res growth.SyncResult) map[string]any {
	payload := map[string]any{
		"source": res.Source, "messages": res.Messages, "count": len(res.Messages),
		"read": res.Read, "skipped_count": res.Skipped,
	}
	if res.CaughtUp != nil {
		payload["caught_up"] = *res.CaughtUp
	}

	switch {
	case res.Read == 0 && res.CaughtUp != nil:
		payload["note"] = "no new messages since the last read; come back later"
	case res.Read == 0:
		payload["note"] = "the back-read of this source is done; read it with direction=newer from now on"
	case len(res.Messages) == 0:
		payload["note"] = "the pre-screen found none of the messages read worth a look; keep reading"
	}
	if res.CaughtUp != nil && !*res.CaughtUp {
		payload["note"] = "more new messages are waiting: call again with direction=newer"
	}
	return payload
}

func (g *growthTools) upsertLeads(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthUpsertLeadsAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthUpsertLeadsToolName, err)
	}
	if len(action.Leads) == 0 {
		return "", errors.New("leads is required: a batch of one or more leads")
	}

	inputs := make([]growth.LeadInput, 0, len(action.Leads))
	for i, l := range action.Leads {
		in := growth.LeadInput{
			PlatformUserID: l.PlatformUserID.String(),
			Username:       l.Username,
			DisplayName:    l.DisplayName,
			Score:          l.Score.Int(),
			Tier:           strings.ToLower(l.Tier.String()),
			MatchReason:    l.MatchReason,
		}
		for j, e := range l.Evidence {
			ev := growth.EvidenceInput{
				SourceID:    e.SourceID.Int64(),
				MessageID:   e.MessageID.String(),
				MessageLink: e.MessageLink,
				Excerpt:     e.Excerpt,
				MatchReason: e.MatchReason,
			}
			if t, err := parseGrowthTime(e.MessageTime); err != nil {
				return "", fmt.Errorf("leads[%d].evidence[%d].message_time: %w", i, j, err)
			} else if !t.IsZero() {
				ev.MessageTime = &t
			}
			t, err := parseGrowthTime(e.CapturedAt)
			if err != nil {
				return "", fmt.Errorf("leads[%d].evidence[%d].captured_at: %w", i, j, err)
			}
			ev.CapturedAt = t
			in.Evidence = append(in.Evidence, ev)
		}
		inputs = append(inputs, in)
	}

	results, err := g.backend.UpsertLeads(ctx, g.scope, inputs)
	if err != nil {
		return g.failure(ctx, err)
	}

	counts := map[string]int{}
	for _, r := range results {
		counts[strings.ToLower(r.Status)]++
	}
	return g.respond(ctx, map[string]any{"results": results, "summary": counts})
}

func (g *growthTools) getProgress(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthGetProgressAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthGetProgressToolName, err)
	}

	var (
		p   growth.Progress
		err error
	)
	if target := action.Target.Int(); target > 0 {
		p, err = g.backend.SetTarget(ctx, g.scope, target)
	} else {
		p, err = g.backend.Progress(ctx, g.scope)
	}
	if err != nil {
		return g.failure(ctx, err)
	}
	return marshalWithProgress(map[string]any{"progress": p}, &p)
}

func (g *growthTools) deliverLeads(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthDeliverLeadsAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthDeliverLeadsToolName, err)
	}
	d, err := g.backend.Deliver(ctx, g.scope, action.AllowPartial.Bool())
	if err != nil {
		return g.failure(ctx, err)
	}
	return marshalWithProgress(d, &d.Progress)
}

func (g *growthTools) listLeads(ctx context.Context, args json.RawMessage) (string, error) {
	var action GrowthListLeadsAction
	if err := json.Unmarshal(args, &action); err != nil {
		return "", fmt.Errorf("failed to parse %s args: %w", GrowthListLeadsToolName, err)
	}

	filter := growth.LeadFilter{Limit: action.Limit.Int()}
	flowID := g.scope.FlowID
	switch action.Scope {
	case "", "flow":
		filter.FlowID = &flowID
	case "task":
		if g.scope.TaskID == nil {
			return "", errors.New("scope=task needs a task; use scope=flow")
		}
		filter.FlowID, filter.TaskID = &flowID, g.scope.TaskID
	case "delivered":
		filter.FlowID = &flowID
		delivered := true
		filter.Delivered = &delivered
	default:
		return "", fmt.Errorf("unknown scope %q: use flow, task or delivered", action.Scope)
	}
	switch tier := strings.ToLower(action.Tier.String()); tier {
	case "", "all":
	case growth.TierQualified, growth.TierWatchlist:
		filter.Tier = tier
	default:
		return "", fmt.Errorf("unknown tier %q: use qualified, watchlist or all", tier)
	}

	leads, err := g.backend.ListLeads(ctx, g.scope.UserID, filter)
	if err != nil {
		return g.failure(ctx, err)
	}
	return g.respond(ctx, map[string]any{"count": len(leads), "leads": leads})
}

// respond appends the round's progress line when the call belongs to a task, so every
// growth result carries the number the planners and the reporter judge the round by.
func (g *growthTools) respond(ctx context.Context, payload any) (string, error) {
	if g.scope.TaskID == nil {
		return marshalWithProgress(payload, nil)
	}
	p, err := g.backend.Progress(ctx, g.scope)
	if err != nil {
		return marshalWithProgress(payload, nil)
	}
	return marshalWithProgress(payload, &p)
}

// failure turns a domain error into a result the model can act on. Only a cancelled
// context is returned as an error: retrying anything else with the same arguments fails
// the same way.
func (g *growthTools) failure(ctx context.Context, err error) (string, error) {
	if ctx.Err() != nil {
		return "", err
	}
	return growthFailureText(err), nil
}

func growthFailureText(err error) string {
	var flood *connectors.FloodWaitError
	switch {
	case errors.As(err, &flood):
		return fmt.Sprintf("rate limited by the platform: it asks to wait %s. Do not repeat this call now; "+
			"continue with another source or another step and come back later.", flood.Wait.Round(time.Second))
	case errors.Is(err, growth.ErrBrowserSource):
		return fmt.Sprintf("%s. Open the page with the browser tool, read the posts, and save leads with %s "+
			"using this source_id; for platform_user_id use the author's profile URL, or 'host:username' "+
			"when the page shows no profile link. Error detail: %v", growth.ErrBrowserSource, GrowthUpsertLeadsToolName, err)
	case errors.Is(err, growth.ErrNoAccount), errors.Is(err, connectors.ErrUnauthorized):
		return fmt.Sprintf("the platform account is not logged in (%v). Retrying will not help: "+
			"an operator has to log the account in. Work with web sources meanwhile and report this blocker.", err)
	case errors.Is(err, connectors.ErrNoConnector):
		return fmt.Sprintf("%v. This platform is not available in this deployment; work with other sources.", err)
	case errors.Is(err, connectors.ErrUnsupported):
		return fmt.Sprintf("%v. Use another mode or tool for this source.", err)
	case errors.Is(err, growth.ErrSourceNotFound):
		return fmt.Sprintf("%v. Use a source_id returned by %s or %s.", err, GrowthSearchSourcesToolName, GrowthAddSourcesToolName)
	case errors.Is(err, growth.ErrNoJudge):
		return fmt.Sprintf("%v. Retrying will not help: report exactly this to the user and end this subtask.", err)
	case errors.Is(err, growth.ErrNoTask):
		return fmt.Sprintf("%v.", err)
	default:
		return fmt.Sprintf("the growth tool failed: %v", err)
	}
}

func marshalWithProgress(payload any, p *growth.Progress) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal growth tool result: %w", err)
	}
	if p == nil || p.Target == 0 {
		return string(raw), nil
	}
	return string(raw) + "\n" + p.Line(), nil
}

// parseGrowthTime accepts what models write for a timestamp: RFC 3339, a date-time
// without a zone (read as UTC), a bare date, or unix seconds. Empty is the zero time.
func parseGrowthTime(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, nil
	}
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC(), nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q: use RFC 3339, e.g. 2026-10-16T09:30:00Z", raw)
}
