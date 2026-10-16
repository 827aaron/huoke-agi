package growth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"
)

const (
	IdentityPlatformID    = "platform_id"
	IdentityProfileURL    = "profile_url"
	IdentityForumUsername = "forum_username"

	LeadStatusNew      = "NEW"
	LeadStatusExisting = "EXISTING"
	LeadStatusUpgraded = "UPGRADED"
	LeadStatusRejected = "REJECTED"

	maxExcerptRunes = 2000
	maxListLimit    = 500
)

type EvidenceInput struct {
	SourceID    int64
	MessageID   string
	MessageLink string
	MessageTime *time.Time
	// CapturedAt is when the agent read the page; zero means now.
	CapturedAt  time.Time
	Excerpt     string
	MatchReason string
}

type LeadInput struct {
	PlatformUserID string
	Username       string
	DisplayName    string
	Score          int
	// Tier is what the agent concluded; the stored tier always follows from Score.
	Tier        string
	MatchReason string
	Evidence    []EvidenceInput
}

type LeadResult struct {
	Index          int    `json:"index"`
	LeadID         int64  `json:"lead_id,omitempty"`
	Platform       string `json:"platform,omitempty"`
	PlatformUserID string `json:"platform_user_id"`
	Status         string `json:"status"`
	Tier           string `json:"tier,omitempty"`
	Score          int    `json:"score,omitempty"`
	EvidenceAdded  int    `json:"evidence_added"`
	AlreadyDeliver bool   `json:"already_delivered,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type EvidenceView struct {
	SourceID    int64      `json:"source_id"`
	MessageID   string     `json:"message_id"`
	MessageLink string     `json:"message_link,omitempty"`
	MessageTime *time.Time `json:"message_time,omitempty"`
	CapturedAt  time.Time  `json:"captured_at"`
	Excerpt     string     `json:"excerpt"`
	MatchReason string     `json:"match_reason,omitempty"`
}

type LeadView struct {
	ID             int64          `json:"lead_id"`
	Platform       string         `json:"platform"`
	PlatformUserID string         `json:"platform_user_id"`
	IdentityKind   string         `json:"identity_kind"`
	Username       string         `json:"username,omitempty"`
	DisplayName    string         `json:"display_name,omitempty"`
	Score          int            `json:"score"`
	Tier           string         `json:"tier"`
	MatchReason    string         `json:"match_reason,omitempty"`
	FlowID         *int64         `json:"flow_id,omitempty"`
	TaskID         *int64         `json:"task_id,omitempty"`
	DeliveredAt    *time.Time     `json:"delivered_at,omitempty"`
	DeliveredRunID *int64         `json:"delivered_run_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	Evidence       []EvidenceView `json:"evidence"`
}

type LeadFilter struct {
	FlowID    *int64
	TaskID    *int64
	RunID     *int64
	Tier      string
	Delivered *bool
	Limit     int
	Offset    int
}

// UpsertLeads stores candidates with their evidence. A person is one lead per customer
// and platform however many times they are submitted; a repeat only adds evidence and can
// raise the score, which can move a watchlist lead to qualified.
func (s *Service) UpsertLeads(ctx context.Context, scope Scope, inputs []LeadInput) ([]LeadResult, error) {
	sources := map[int64]database.GrowthSource{}
	results := make([]LeadResult, 0, len(inputs))

	for i, in := range inputs {
		res, err := s.upsertLead(ctx, scope, in, sources)
		res.Index = i
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}

	if len(sources) > 0 {
		ids := make([]int64, 0, len(sources))
		for id := range sources {
			ids = append(ids, id)
		}
		if err := s.db.RefreshSourceYield(ctx, ids); err != nil {
			return results, fmt.Errorf("failed to update source yield: %w", err)
		}
	}
	return results, nil
}

func (s *Service) upsertLead(
	ctx context.Context,
	scope Scope,
	in LeadInput,
	sources map[int64]database.GrowthSource,
) (LeadResult, error) {
	res := LeadResult{PlatformUserID: in.PlatformUserID, Score: in.Score}
	reject := func(reason string) (LeadResult, error) {
		res.Status, res.Reason = LeadStatusRejected, reason
		return res, nil
	}

	if len(in.Evidence) == 0 {
		return reject("at least one evidence item is required")
	}

	var platform string
	for _, ev := range in.Evidence {
		src, ok := sources[ev.SourceID]
		if !ok {
			var err error
			src, err = s.userSource(ctx, scope.UserID, ev.SourceID)
			if errors.Is(err, ErrSourceNotFound) {
				return reject(fmt.Sprintf("evidence source_id %d is not in the source pool", ev.SourceID))
			}
			if err != nil {
				return res, err
			}
			sources[ev.SourceID] = src
		}
		if platform == "" {
			platform = src.Platform
		} else if platform != src.Platform {
			return reject("evidence of one lead must come from one platform")
		}
	}
	res.Platform = platform

	firstSource := sources[in.Evidence[0].SourceID]
	userID, identity, err := normaliseIdentity(connectors.Platform(platform), in.PlatformUserID, in.Username, firstSource.Link)
	if err != nil {
		return reject(err.Error())
	}
	res.PlatformUserID = userID

	tier := TierForScore(in.Score)
	if tier == "" {
		return reject(fmt.Sprintf("score %d is below the watchlist threshold %d; such candidates are not saved",
			in.Score, WatchlistMinScore))
	}
	if in.Tier != "" && in.Tier != tier {
		res.Reason = fmt.Sprintf("tier %q follows from score %d, not %q", tier, in.Score, in.Tier)
	}

	prev, err := s.db.GetLeadByPlatformUser(ctx, database.GetLeadByPlatformUserParams{
		UserID: scope.UserID, Platform: platform, PlatformUserID: userID,
	})
	existed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return res, fmt.Errorf("failed to look up lead %s: %w", userID, err)
	}

	lead, err := s.db.UpsertLead(ctx, database.UpsertLeadParams{
		UserID:         scope.UserID,
		Platform:       platform,
		PlatformUserID: userID,
		IdentityKind:   identity,
		Username:       strings.TrimPrefix(strings.TrimSpace(in.Username), "@"),
		DisplayName:    strings.TrimSpace(in.DisplayName),
		Score:          int32(in.Score),
		Tier:           tier,
		MatchReason:    strings.TrimSpace(in.MatchReason),
		FlowID:         scope.flowID(),
		TaskID:         scope.taskID(),
	})
	if err != nil {
		return res, fmt.Errorf("failed to store lead %s: %w", userID, err)
	}

	res.LeadID, res.Tier, res.Score = lead.ID, lead.Tier, int(lead.Score)
	res.AlreadyDeliver = lead.DeliveredAt.Valid
	switch {
	case !existed:
		res.Status = LeadStatusNew
	case prev.Tier != TierQualified && lead.Tier == TierQualified:
		res.Status = LeadStatusUpgraded
	default:
		res.Status = LeadStatusExisting
	}

	for _, ev := range in.Evidence {
		params, err := evidenceParams(lead.ID, sources[ev.SourceID], ev, scope)
		if err != nil {
			res.Reason = joinReason(res.Reason, err.Error())
			continue
		}
		n, err := s.db.InsertLeadEvidence(ctx, params)
		if err != nil {
			return res, fmt.Errorf("failed to store evidence of lead %d: %w", lead.ID, err)
		}
		res.EvidenceAdded += int(n)
	}

	return res, nil
}

// normaliseIdentity returns the lead key. Telegram keys on the platform user id. A web
// source rarely has one, so its key is a normalised profile URL, or 'host:username' with
// the host of the page the person posted on.
func normaliseIdentity(platform connectors.Platform, rawID, username, sourceLink string) (string, string, error) {
	id := strings.TrimSpace(rawID)
	if platform != connectors.PlatformWeb {
		if id == "" {
			return "", "", errors.New("platform_user_id is required: a stable id from the tool output")
		}
		return id, IdentityPlatformID, nil
	}

	if id == "" {
		id = strings.TrimPrefix(strings.TrimSpace(username), "@")
	}
	if id == "" {
		return "", "", errors.New("platform_user_id is required: a profile URL or the forum username")
	}

	if strings.Contains(id, "://") || strings.Contains(id, "/") {
		canonical, err := canonicalWebURL(id)
		if err != nil {
			return "", "", err
		}
		return canonical, IdentityProfileURL, nil
	}

	if host, name, ok := strings.Cut(id, ":"); ok && strings.Contains(host, ".") && name != "" {
		return strings.TrimPrefix(strings.ToLower(host), "www.") + ":" + name, IdentityForumUsername, nil
	}

	host := hostOf(sourceLink)
	if host == "" {
		return "", "", errors.New("a forum username needs the host of its page: pass 'host:username'")
	}
	return host + ":" + id, IdentityForumUsername, nil
}

func evidenceParams(
	leadID int64,
	src database.GrowthSource,
	ev EvidenceInput,
	scope Scope,
) (database.InsertLeadEvidenceParams, error) {
	messageID := strings.TrimSpace(ev.MessageID)
	link := strings.TrimSpace(ev.MessageLink)

	if src.Platform == string(connectors.PlatformWeb) {
		if link == "" {
			link = src.Link
		}
		if messageID == "" {
			canonical, err := canonicalWebURL(link)
			if err != nil {
				return database.InsertLeadEvidenceParams{}, fmt.Errorf("evidence from source %d has no usable link", src.ID)
			}
			messageID = canonical
		}
	}
	if messageID == "" {
		return database.InsertLeadEvidenceParams{}, fmt.Errorf("evidence from source %d has no message_id", src.ID)
	}

	captured := ev.CapturedAt
	if captured.IsZero() {
		captured = time.Now()
	}
	var msgTime sql.NullTime
	if ev.MessageTime != nil && !ev.MessageTime.IsZero() {
		msgTime = sql.NullTime{Time: *ev.MessageTime, Valid: true}
	}

	return database.InsertLeadEvidenceParams{
		LeadID:      leadID,
		SourceID:    sql.NullInt64{Int64: src.ID, Valid: true},
		MessageID:   messageID,
		MessageLink: link,
		MessageTime: msgTime,
		CapturedAt:  captured,
		Excerpt:     truncateRunes(strings.TrimSpace(ev.Excerpt), maxExcerptRunes),
		MatchReason: strings.TrimSpace(ev.MatchReason),
		FlowID:      scope.flowID(),
		TaskID:      scope.taskID(),
	}, nil
}

func (s *Service) ListLeads(ctx context.Context, userID int64, f LeadFilter) ([]LeadView, error) {
	params := database.GetUserLeadsParams{
		UserID: userID,
		Lim:    int64(clampLimit(f.Limit, 100, maxListLimit)),
		Off:    int64(max(f.Offset, 0)),
	}
	if f.FlowID != nil {
		params.FlowID = sql.NullInt64{Int64: *f.FlowID, Valid: true}
	}
	if f.TaskID != nil {
		params.TaskID = sql.NullInt64{Int64: *f.TaskID, Valid: true}
	}
	if f.RunID != nil {
		params.RunID = sql.NullInt64{Int64: *f.RunID, Valid: true}
	}
	if f.Tier != "" {
		params.Tier = sql.NullString{String: f.Tier, Valid: true}
	}
	if f.Delivered != nil {
		params.Delivered = sql.NullBool{Bool: *f.Delivered, Valid: true}
	}

	leads, err := s.db.GetUserLeads(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list leads: %w", err)
	}
	return s.leadViews(ctx, leads)
}

func (s *Service) GetLead(ctx context.Context, userID, leadID int64) (LeadView, error) {
	lead, err := s.db.GetUserLead(ctx, database.GetUserLeadParams{ID: leadID, UserID: userID})
	if err != nil {
		return LeadView{}, err
	}
	views, err := s.leadViews(ctx, []database.Lead{lead})
	if err != nil {
		return LeadView{}, err
	}
	return views[0], nil
}

func (s *Service) leadViews(ctx context.Context, leads []database.Lead) ([]LeadView, error) {
	views := make([]LeadView, 0, len(leads))
	if len(leads) == 0 {
		return views, nil
	}

	ids := make([]int64, 0, len(leads))
	for _, l := range leads {
		ids = append(ids, l.ID)
	}
	evidence, err := s.db.GetLeadsEvidence(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to load evidence: %w", err)
	}
	byLead := make(map[int64][]EvidenceView, len(leads))
	for _, e := range evidence {
		ev := EvidenceView{
			SourceID:    e.SourceID.Int64,
			MessageID:   e.MessageID,
			MessageLink: e.MessageLink,
			CapturedAt:  e.CapturedAt.UTC(),
			Excerpt:     e.Excerpt,
			MatchReason: e.MatchReason,
		}
		if e.MessageTime.Valid {
			t := e.MessageTime.Time.UTC()
			ev.MessageTime = &t
		}
		byLead[e.LeadID] = append(byLead[e.LeadID], ev)
	}

	for _, l := range leads {
		v := LeadView{
			ID:             l.ID,
			Platform:       l.Platform,
			PlatformUserID: l.PlatformUserID,
			IdentityKind:   l.IdentityKind,
			Username:       l.Username,
			DisplayName:    l.DisplayName,
			Score:          int(l.Score),
			Tier:           l.Tier,
			MatchReason:    l.MatchReason,
			FlowID:         nullInt(l.LastFlowID),
			TaskID:         nullInt(l.LastTaskID),
			DeliveredRunID: nullInt(l.DeliveredRunID),
			Evidence:       byLead[l.ID],
		}
		if v.Evidence == nil {
			v.Evidence = []EvidenceView{}
		}
		if l.CreatedAt.Valid {
			v.CreatedAt = l.CreatedAt.Time.UTC()
		}
		if l.DeliveredAt.Valid {
			t := l.DeliveredAt.Time.UTC()
			v.DeliveredAt = &t
		}
		views = append(views, v)
	}
	return views, nil
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func hostOf(link string) string {
	s := link
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func joinReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
