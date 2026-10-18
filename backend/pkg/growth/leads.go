package growth

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	"github.com/sqlc-dev/pqtype"
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

var (
	ErrInvalidLead = errors.New("invalid lead")
	ErrLeadExists  = errors.New("this person is already a lead")
)

var manualLeadPlatforms = []connectors.Platform{
	connectors.PlatformTelegram, connectors.PlatformWeb, connectors.PlatformFacebook,
	connectors.PlatformInstagram, connectors.PlatformYouTube, connectors.PlatformTikTok,
}

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
	// Score is the agent's. The judgement model scores every lead's evidence, from the
	// read that screened its messages or else from its excerpts, and its score replaces
	// this one; the agent's stands only when the model could not answer.
	Score int
	// Tier is what the agent concluded; the stored tier always follows from the score.
	Tier        string
	MatchReason string
	Contacts    []Contact
	Evidence    []EvidenceInput
}

// Contact is a way to reach a lead beside its platform account; Type is one of
// ContactTypes other than the platform handle, or a name the profile asks for.
type Contact struct {
	Type  string `json:"type"`
	Value string `json:"value"`
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
	Contacts       []Contact      `json:"contacts"`
	Evidence       []EvidenceView `json:"evidence"`
	Note           string         `json:"note"`
	Origin         string         `json:"origin"`
	// Signals and ScoreBreakdown come from the best judged message of the evidence; both
	// are empty for a lead no judgement scored, such as one added by hand.
	Signals        []string    `json:"signals"`
	ScoreBreakdown []ScorePart `json:"score_breakdown"`
	// Flags mark what the score does not show; LeadFlagUnwanted is a match of the
	// profile's unwanted list.
	Flags []string `json:"flags"`
}

const LeadFlagUnwanted = "unwanted"

// LeadUpdate changes what is set; Contacts replaces the whole list. The score and the
// delivery follow from the work and are not edited.
type LeadUpdate struct {
	Note     *string    `json:"note,omitempty"`
	Tier     *string    `json:"tier,omitempty"`
	Contacts *[]Contact `json:"contacts,omitempty"`
}

// ManualLead is a person the customer adds by hand. Handle is the platform id, username
// or profile link; on the web, a profile link or 'host:username'. It enters at the
// profile's qualified line without a judgement, and FlowID makes it part of that task's
// next delivery.
type ManualLead struct {
	Platform    string    `json:"platform"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name,omitempty"`
	FlowID      *int64    `json:"flow_id,omitempty"`
	Tier        string    `json:"tier,omitempty"`
	Contacts    []Contact `json:"contacts,omitempty"`
	Note        string    `json:"note,omitempty"`
}

type LeadFilter struct {
	FlowID    *int64
	TaskID    *int64
	RunID     *int64
	Tier      string
	Delivered *bool
	// Since keeps leads first found at or after it; the zero time keeps all.
	Since time.Time
	// HasContact keeps leads with (true) or without (false) a contact beside the platform account.
	HasContact *bool
	// Flagged keeps leads with (true) or without (false) a flag.
	Flagged *bool
	Limit   int
	Offset  int
}

// UpsertLeads stores candidates with their evidence. A person is one lead per customer
// and platform however many times they are submitted; a repeat only adds evidence and can
// raise the score, which can move a watchlist lead to qualified.
func (s *Service) UpsertLeads(ctx context.Context, scope Scope, inputs []LeadInput) ([]LeadResult, error) {
	sources := map[int64]database.GrowthSource{}
	results := make([]LeadResult, 0, len(inputs))
	profile, err := s.judgingProfile(ctx, scope)
	if err != nil {
		return nil, err
	}

	var sc *screener
	judge := func(src database.GrowthSource, in LeadInput) (int, bool, bool, error) {
		if sc == nil {
			var err error
			if sc, err = s.screenerFor(ctx, scope); err != nil {
				return 0, false, false, err
			}
		}
		return s.judgeEvidence(ctx, scope.UserID, sc, src, in)
	}
	for i, in := range inputs {
		res, err := s.upsertLead(ctx, scope, in, sources, profile, judge)
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
	profile ProfileView,
	judge func(database.GrowthSource, LeadInput) (int, bool, bool, error),
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

	score, unwanted, judged, err := s.judgedScore(ctx, scope.UserID, profile.ID, in.Evidence)
	if err != nil {
		return res, err
	}
	if !judged {
		if score, unwanted, judged, err = judge(firstSource, in); err != nil {
			return res, err
		}
	}
	if !judged {
		score = in.Score
	} else if in.Score != 0 && in.Score != score {
		res.Reason = fmt.Sprintf("score %d is the judgement model's for this evidence, not %d", score, in.Score)
	}
	scoring := profile.ScoringOrDefault()
	res.Score = score
	tier := scoring.tier(score)
	if tier == "" {
		return reject(fmt.Sprintf("score %d is below the watchlist threshold %d; such candidates are not saved",
			score, scoring.Watchlist))
	}
	if in.Tier != "" && in.Tier != tier {
		res.Reason = joinReason(res.Reason, fmt.Sprintf("tier %q follows from score %d, not %q", tier, score, in.Tier))
	}
	contacts, badContacts := cleanContacts(in.Contacts)
	if badContacts != "" {
		res.Reason = joinReason(res.Reason, badContacts)
	}

	prev, err := s.db.GetLeadByPlatformUser(ctx, database.GetLeadByPlatformUserParams{
		UserID: scope.UserID, Platform: platform, PlatformUserID: userID,
	})
	existed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return res, fmt.Errorf("failed to look up lead %s: %w", userID, err)
	}
	if existed {
		contacts = mergeContacts(storedContacts(prev.Contacts), contacts)
	}
	contactsJSON, err := json.Marshal(contacts)
	if err != nil {
		return res, err
	}

	lead, err := s.db.UpsertLead(ctx, database.UpsertLeadParams{
		UserID:         scope.UserID,
		Platform:       platform,
		PlatformUserID: userID,
		IdentityKind:   identity,
		Username:       strings.TrimPrefix(strings.TrimSpace(in.Username), "@"),
		DisplayName:    strings.TrimSpace(in.DisplayName),
		Score:          int32(score),
		Tier:           tier,
		MatchReason:    strings.TrimSpace(in.MatchReason),
		Contacts:       contactsJSON,
		FlowID:         scope.flowID(),
		TaskID:         scope.taskID(),
		Flags:          leadFlags(unwanted),
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

// judgedScore is the highest score the judgement model gave a message of the evidence,
// whether it found the sender unwanted in any of them, and false when it scored none.
func (s *Service) judgedScore(ctx context.Context, userID, profileID int64, evidence []EvidenceInput) (int, bool, bool, error) {
	bySource := map[int64][]string{}
	for _, ev := range evidence {
		if id := strings.TrimSpace(ev.MessageID); id != "" {
			bySource[ev.SourceID] = append(bySource[ev.SourceID], id)
		}
	}
	best, unwanted, judged := 0, false, false
	for sourceID, ids := range bySource {
		rows, err := s.db.GetMessageScores(ctx, database.GetMessageScoresParams{
			UserID: userID, ProfileID: profileID, SourceID: sourceID, MessageIds: ids,
		})
		if err != nil {
			return 0, false, false, fmt.Errorf("failed to load message scores: %w", err)
		}
		for _, r := range rows {
			best, judged = max(best, int(r.Score)), true
			unwanted = unwanted || slices.ContainsFunc(scoreParts(r.Signals), isUnwantedPart)
		}
	}
	return best, unwanted, judged, nil
}

func isUnwantedPart(p ScorePart) bool { return p.Signal == LeadFlagUnwanted }

// leadFlags are the flags a judgement sets on a lead; stored ones are kept.
func leadFlags(unwanted bool) []string {
	if unwanted {
		return []string{LeadFlagUnwanted}
	}
	return []string{}
}

// cleanContacts keeps the contacts of a known type with a value, once each, and says
// what it left out.
func cleanContacts(in []Contact) ([]Contact, string) {
	out := make([]Contact, 0, len(in))
	var dropped []string
	for _, c := range in {
		c.Type, c.Value = contactType(strings.TrimSpace(c.Type)), strings.TrimSpace(c.Value)
		switch {
		case c.Value == "" || c.Type == ContactPlatformHandle:
			continue
		case c.Type == "" || utf8.RuneCountInString(c.Type) > maxContactTypeRunes:
			dropped = append(dropped, fmt.Sprintf("contact type %q is empty or longer than %d characters", c.Type, maxContactTypeRunes))
			continue
		}
		out = mergeContacts(out, []Contact{c})
	}
	return out, strings.Join(dropped, "; ")
}

func mergeContacts(have, add []Contact) []Contact {
	for _, c := range add {
		if !slices.ContainsFunc(have, func(h Contact) bool { return h.Type == c.Type && strings.EqualFold(h.Value, c.Value) }) {
			have = append(have, c)
		}
	}
	return have
}

func storedContacts(raw json.RawMessage) []Contact {
	out := []Contact{}
	_ = json.Unmarshal(raw, &out)
	return out
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
	if !f.Since.IsZero() {
		params.Since = sql.NullTime{Time: f.Since, Valid: true}
	}
	if f.HasContact != nil {
		params.HasContact = sql.NullBool{Bool: *f.HasContact, Valid: true}
	}
	if f.Flagged != nil {
		params.Flagged = sql.NullBool{Bool: *f.Flagged, Valid: true}
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
	scores, err := s.db.GetLeadsBestScores(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to load the scores of the evidence: %w", err)
	}
	breakdown := make(map[int64][]ScorePart, len(scores))
	for _, sc := range scores {
		breakdown[sc.LeadID] = scoreParts(sc.Signals)
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
			Contacts:       storedContacts(l.Contacts),
			Evidence:       byLead[l.ID],
			Note:           l.Note,
			Origin:         l.Origin,
			Signals:        []string{},
			ScoreBreakdown: breakdown[l.ID],
			Flags:          l.Flags,
		}
		if v.Flags == nil {
			v.Flags = []string{}
		}
		if v.ScoreBreakdown == nil {
			v.ScoreBreakdown = []ScorePart{}
		}
		for _, part := range v.ScoreBreakdown {
			if part.Points > 0 {
				v.Signals = append(v.Signals, part.Signal)
			}
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

func (s *Service) UpdateLead(ctx context.Context, userID, leadID int64, u LeadUpdate) (LeadView, error) {
	params := database.UpdateUserLeadParams{ID: leadID, UserID: userID, Note: trimmedOrNull(u.Note)}
	if u.Tier != nil {
		if *u.Tier != TierQualified && *u.Tier != TierWatchlist {
			return LeadView{}, fmt.Errorf("%w: tier is %s or %s", ErrInvalidLead, TierQualified, TierWatchlist)
		}
		params.Tier = sql.NullString{String: *u.Tier, Valid: true}
	}
	if u.Contacts != nil {
		raw, err := leadContacts(*u.Contacts)
		if err != nil {
			return LeadView{}, err
		}
		params.Contacts = pqtype.NullRawMessage{RawMessage: raw, Valid: true}
	}
	lead, err := s.db.UpdateUserLead(ctx, params)
	if err != nil {
		return LeadView{}, err
	}
	views, err := s.leadViews(ctx, []database.Lead{lead})
	if err != nil {
		return LeadView{}, err
	}
	return views[0], nil
}

// DeleteLead removes a lead with its evidence and the scores of its messages; a
// delivered lead goes too, though the lists already sent keep it.
func (s *Service) DeleteLead(ctx context.Context, userID, leadID int64) error {
	_, err := s.db.DeleteUserLead(ctx, database.DeleteUserLeadParams{ID: leadID, UserID: userID})
	return err
}

func (s *Service) CreateLead(ctx context.Context, userID int64, in ManualLead) (LeadView, error) {
	platform := connectors.Platform(strings.ToLower(strings.TrimSpace(in.Platform)))
	if !slices.Contains(manualLeadPlatforms, platform) {
		return LeadView{}, fmt.Errorf("%w: platform %q is not one this system reads", ErrInvalidLead, in.Platform)
	}
	id, kind, err := normaliseIdentity(platform, strings.TrimSpace(in.Handle), "", "")
	if err != nil {
		return LeadView{}, fmt.Errorf("%w: %v", ErrInvalidLead, err)
	}
	tier := cmp.Or(in.Tier, TierQualified)
	if tier != TierQualified && tier != TierWatchlist {
		return LeadView{}, fmt.Errorf("%w: tier is %s or %s", ErrInvalidLead, TierQualified, TierWatchlist)
	}
	contacts, err := leadContacts(in.Contacts)
	if err != nil {
		return LeadView{}, err
	}
	scope := Scope{UserID: userID}
	if in.FlowID != nil {
		scope.FlowID = *in.FlowID
	}
	profile, err := s.judgingProfile(ctx, scope)
	if err != nil {
		return LeadView{}, err
	}
	scoring := profile.ScoringOrDefault()
	score := scoring.Qualified
	if tier == TierWatchlist {
		score = scoring.Watchlist
	}
	params := database.InsertManualLeadParams{
		UserID: userID, Platform: string(platform), PlatformUserID: id, IdentityKind: kind,
		DisplayName: strings.TrimSpace(in.DisplayName), Score: int32(score), Tier: tier,
		Contacts: contacts, Note: strings.TrimSpace(in.Note),
	}
	if in.FlowID != nil {
		if _, err := s.db.GetUserFlow(ctx, database.GetUserFlowParams{ID: *in.FlowID, UserID: userID}); err != nil {
			return LeadView{}, fmt.Errorf("%w: task %d is not one of yours", ErrInvalidLead, *in.FlowID)
		}
		params.FlowID = sql.NullInt64{Int64: *in.FlowID, Valid: true}
	}
	lead, err := s.db.InsertManualLead(ctx, params)
	if errors.Is(err, sql.ErrNoRows) {
		return LeadView{}, fmt.Errorf("%w: %s %s", ErrLeadExists, platform, id)
	}
	if err != nil {
		return LeadView{}, fmt.Errorf("failed to add the lead: %w", err)
	}
	views, err := s.leadViews(ctx, []database.Lead{lead})
	if err != nil {
		return LeadView{}, err
	}
	return views[0], nil
}

// leadContacts is a list of contacts edited by hand, refused whole if one is unusable.
func leadContacts(in []Contact) (json.RawMessage, error) {
	contacts, dropped := cleanContacts(in)
	if dropped != "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidLead, dropped)
	}
	return json.Marshal(contacts)
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
