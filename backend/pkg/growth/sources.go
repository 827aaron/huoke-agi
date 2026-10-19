package growth

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
)

const (
	OriginAI     = "ai"
	OriginManual = "manual"

	SourceStatusActive     = "active"
	SourceStatusUnresolved = "unresolved"
	SourceStatusInvalid    = "invalid"

	DirectionOlder = "older"
	DirectionNewer = "newer"

	staleAfterEmptyReads = 3
	deadAfterEmptyReads  = 6

	maxSearchLimit   = 50
	maxHistoryLimit  = 60
	maxMessageRunes  = 400
	maxDescRunes     = 300
	defaultPageLimit = 30
	// maxFullMessageRunes bounds one message fetched for an evidence excerpt. A page read
	// stays at maxMessageRunes, the opening lines the pre-screen judges.
	maxFullMessageRunes = 8000
)

var (
	ErrSourceNotFound = errors.New("source not found in the pool")
	// ErrBrowserSource is returned for operations a web source does not support:
	// its pages are read with the browser tool, not through a connector.
	ErrBrowserSource   = errors.New("web sources are read with the browser tool")
	ErrNoJoinedSource  = errors.New("no source in the pool is joined yet")
	ErrMessageNotFound = errors.New("message not found in this source")
)

type SourceView struct {
	ID               int64  `json:"source_id"`
	Platform         string `json:"platform"`
	Title            string `json:"title,omitempty"`
	Username         string `json:"username,omitempty"`
	Link             string `json:"link,omitempty"`
	Kind             string `json:"kind"`
	MemberCount      int64  `json:"member_count,omitempty"`
	Description      string `json:"description,omitempty"`
	Origin           string `json:"origin"`
	Status           string `json:"status"`
	StatusReason     string `json:"status_reason,omitempty"`
	JoinStatus       string `json:"join_status"`
	Joinable         bool   `json:"joinable"`
	MessagesRead     int64  `json:"messages_read"`
	HistoryExhausted bool   `json:"history_exhausted"`
	QualifiedLeads   int    `json:"qualified_leads"`
	EmptyStreak      int    `json:"empty_streak"`
	Health           string `json:"health"`
	LastSyncedAt     string `json:"last_synced_at,omitempty"`
	DiscoveredFrom   *int64 `json:"discovered_from,omitempty"`
	New              bool   `json:"new,omitempty"`
	Note             string `json:"note"`
}

// SourceUpdate changes what is set; the link is the source's identity and stays.
type SourceUpdate struct {
	Title *string `json:"title,omitempty"`
	Note  *string `json:"note,omitempty"`
}

type AddSourceResult struct {
	Input  string      `json:"input"`
	Result string      `json:"result"`
	Reason string      `json:"reason,omitempty"`
	Source *SourceView `json:"source,omitempty"`
}

const (
	AddResultAdded      = "added"
	AddResultExisting   = "existing"
	AddResultUnresolved = "unresolved"
	AddResultInvalid    = "invalid"
)

// JoinOutcome is one source's join. Advice holds the reminder codes of a join that went
// past the account's suggested pace, and Warning says the same in a sentence.
type JoinOutcome struct {
	SourceID   int64    `json:"source_id"`
	Title      string   `json:"title,omitempty"`
	JoinStatus string   `json:"join_status"`
	Reason     string   `json:"reason,omitempty"`
	Advice     []string `json:"advice,omitempty"`
	Warning    string   `json:"warning,omitempty"`
}

type MessageView struct {
	MessageID      string    `json:"message_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username,omitempty"`
	SenderName     string    `json:"sender_name,omitempty"`
	SenderIsBot    bool      `json:"sender_is_bot,omitempty"`
	Time           time.Time `json:"time"`
	Link           string    `json:"link,omitempty"`
	ReplyTo        string    `json:"reply_to,omitempty"`
	Text           string    `json:"text"`
	// Score, Tier and Signals are the judgement model's verdict on a screened message:
	// a lead saved with this message as evidence takes this score.
	Score   int    `json:"score,omitempty"`
	Tier    string `json:"tier,omitempty"`
	Signals string `json:"signals,omitempty"`
}

// SyncResult is one source's read. Read counts every message the cursor moved past,
// Skipped those of them the pre-screen left out. Pending counts the messages fetched
// after the point a bounded read stopped at, which the next read continues with, and
// NextFrom is the first of them. CaughtUp is set on a forward read. Error is set instead
// when a batch read of this source failed.
type SyncResult struct {
	Source   SourceView    `json:"source"`
	Messages []MessageView `json:"messages"`
	Read     int           `json:"read"`
	Skipped  int           `json:"skipped_count"`
	Pending  int           `json:"pending,omitempty"`
	NextFrom string        `json:"next_from_message_id,omitempty"`
	CaughtUp *bool         `json:"caught_up,omitempty"`
	Error    string        `json:"error,omitempty"`
	Err      error         `json:"-"`
	// PrescreenFallbacks counts the batches of this read Jev failed on, whose messages
	// are returned unscreened and unscored; Warning says so in a sentence.
	PrescreenFallbacks int    `json:"prescreen_fallback_batches,omitempty"`
	Warning            string `json:"warning,omitempty"`
	// bytes is the serialized size of Messages, charged to the budget of a bounded read.
	bytes int
}

// DetectPlatform tells a Telegram reference and a Facebook group from a web page;
// anything else is treated as a web page. Anything on a Telegram host, or an @username,
// is Telegram even when it is malformed, so a broken t.me link is reported invalid
// rather than kept as a web page. A Facebook group is read through a signed-in account.
func DetectPlatform(raw string) connectors.Platform {
	raw = strings.TrimSpace(raw)
	if _, err := canonicalTelegramLink(raw); err == nil || strings.HasPrefix(raw, "@") {
		return connectors.PlatformTelegram
	}
	host := strings.ToLower(raw)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	var path string
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host, path = host[:i], host[i:]
	}
	switch host = strings.TrimPrefix(host, "www."); {
	case host == "t.me", host == "telegram.me", host == "telegram.dog":
		return connectors.PlatformTelegram
	case (host == "facebook.com" || host == "m.facebook.com" || host == "web.facebook.com") &&
		strings.HasPrefix(path, "/groups/") && len(path) > len("/groups/"):
		return connectors.PlatformFacebook
	}
	return connectors.PlatformWeb
}

// CanonicalLink normalises a reference to one spelling per source, so the same group
// or thread pasted twice, or pasted after being discovered, dedups by link.
func CanonicalLink(platform connectors.Platform, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty link")
	}
	if platform == connectors.PlatformTelegram {
		return canonicalTelegramLink(raw)
	}
	link, err := canonicalWebURL(raw)
	if err == nil && platform == connectors.PlatformFacebook {
		for _, mirror := range []string{"m.facebook.com/", "web.facebook.com/"} {
			if rest, ok := strings.CutPrefix(link, mirror); ok {
				link = "facebook.com/" + rest
			}
		}
	}
	return link, err
}

// canonicalWebURL drops the scheme, a leading www., the fragment, tracking parameters
// and a trailing slash, and sorts the remaining query, which forums use for thread ids.
func canonicalWebURL(raw string) (string, error) {
	s := raw
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !strings.Contains(u.Host, ".") {
		return "", fmt.Errorf("not a web link: %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("not a web link: %q", raw)
	}

	q := u.Query()
	for key := range q {
		lk := strings.ToLower(key)
		if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "ref" {
			q.Del(key)
		}
	}

	out := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		out += ":" + port
	}
	out += strings.TrimRight(u.EscapedPath(), "/")
	if encoded := q.Encode(); encoded != "" {
		out += "?" + encoded
	}
	return out, nil
}

var tgUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)

func canonicalTelegramLink(raw string) (string, error) {
	s := raw
	if strings.HasPrefix(s, "@") {
		s = "t.me/" + s[1:]
	}
	if !strings.Contains(s, "/") && !strings.Contains(s, ".") {
		s = "t.me/" + s
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("not a Telegram link: %q", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	switch host {
	case "t.me", "telegram.me", "telegram.dog":
	default:
		return "", fmt.Errorf("not a Telegram link: %q", raw)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("the Telegram link has no group or channel: %q", raw)
	}
	if parts[0] == "s" && len(parts) > 1 {
		parts = parts[1:]
	}

	switch {
	case strings.HasPrefix(parts[0], "+"):
		if len(parts[0]) < 2 {
			return "", fmt.Errorf("the Telegram invite link has no hash: %q", raw)
		}
		return "t.me/" + parts[0], nil
	case parts[0] == "joinchat":
		if len(parts) < 2 || parts[1] == "" {
			return "", fmt.Errorf("the Telegram invite link has no hash: %q", raw)
		}
		return "t.me/+" + parts[1], nil
	case tgUsername.MatchString(parts[0]):
		return "t.me/" + strings.ToLower(parts[0]), nil
	default:
		return "", fmt.Errorf("not a Telegram group or channel link: %q", raw)
	}
}

func linkKey(canonical string) string { return "link:" + canonical }

func idKey(externalID string) string { return "id:" + externalID }

func (s *Service) viewSource(ctx context.Context, src database.GrowthSource, accountID int64) SourceView {
	v := SourceView{
		ID:           src.ID,
		Platform:     src.Platform,
		Title:        src.Title,
		Username:     src.Username,
		Link:         src.Link,
		Kind:         src.Kind,
		MemberCount:  src.MemberCount,
		Description:  truncateRunes(src.Description, maxDescRunes),
		Origin:       src.Origin,
		Status:       src.Status,
		StatusReason: src.StatusReason,
		JoinStatus:   string(connectors.JoinStatusNotJoined),
		Joinable:     connectors.Joinable(connectors.Platform(src.Platform)),

		QualifiedLeads: int(src.QualifiedLeads),
		EmptyStreak:    int(src.EmptyStreak),
		Health:         src.Health,
		DiscoveredFrom: nullInt(src.DiscoveredFromID),
		Note:           src.Note,
	}
	if src.LastSyncedAt.Valid {
		v.LastSyncedAt = src.LastSyncedAt.Time.UTC().Format(time.RFC3339)
	}
	if accountID == 0 {
		return v
	}
	as, err := s.db.GetAccountSource(ctx, database.GetAccountSourceParams{AccountID: accountID, SourceID: src.ID})
	if err == nil {
		v.JoinStatus = as.JoinStatus
		v.MessagesRead = as.MessagesRead
		v.HistoryExhausted = as.HistoryExhausted
	}
	return v
}

// upsertFound stores a source the platform reported and its per-account peer.
func (s *Service) upsertFound(
	ctx context.Context,
	scope Scope,
	acc database.ChannelAccount,
	src connectors.Source,
	origin, query string,
	discoveredFrom *int64,
) (database.GrowthSource, bool, error) {
	if src.ExternalID == "" {
		return database.GrowthSource{}, false, errors.New("connector returned a source without an id")
	}
	link := src.Link
	if link == "" && src.Username != "" {
		link, _ = CanonicalLink(connectors.Platform(acc.Platform), src.Username)
	}
	kind := string(src.Kind)
	if kind == "" {
		kind = string(connectors.SourceKindUnknown)
	}

	row, err := s.db.UpsertGrowthSource(ctx, database.UpsertGrowthSourceParams{
		UserID:           scope.UserID,
		Platform:         acc.Platform,
		SourceKey:        idKey(src.ExternalID),
		ExternalID:       src.ExternalID,
		Username:         src.Username,
		Title:            src.Title,
		Link:             link,
		Kind:             kind,
		MemberCount:      src.MemberCount,
		Description:      src.Description,
		Origin:           origin,
		Query:            query,
		Status:           SourceStatusActive,
		DiscoveredFromID: optInt(discoveredFrom),
		DiscoveredFlowID: scope.flowID(),
		DiscoveredTaskID: scope.taskID(),
	})
	if err != nil {
		return database.GrowthSource{}, false, fmt.Errorf("failed to store source %s: %w", src.ExternalID, err)
	}
	gs := sourceFromUpsert(row)

	if link != "" {
		if canonical, err := CanonicalLink(connectors.Platform(acc.Platform), link); err == nil {
			s.dropLinkPlaceholder(ctx, scope.UserID, acc.Platform, canonical, gs.ID)
		}
	}

	if _, err := s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{
		AccountID: acc.ID,
		SourceID:  gs.ID,
		Peer:      rawPeer(src.Peer),
	}); err != nil {
		return gs, row.Inserted, fmt.Errorf("failed to store account view of source %d: %w", gs.ID, err)
	}

	return gs, row.Inserted, nil
}

// dropLinkPlaceholder removes the unresolved row a pasted link left behind once the
// same source is known by id. An unresolved row was never read, so it owns no evidence.
func (s *Service) dropLinkPlaceholder(ctx context.Context, userID int64, platform, canonical string, keepID int64) {
	placeholder, err := s.db.GetGrowthSourceByKey(ctx, database.GetGrowthSourceByKeyParams{
		UserID:    userID,
		Platform:  platform,
		SourceKey: linkKey(canonical),
	})
	if err != nil || placeholder.ID == keepID {
		return
	}
	_, _ = s.db.DeleteUserGrowthSource(ctx, database.DeleteUserGrowthSourceParams{ID: placeholder.ID, UserID: userID})
}

func (s *Service) SearchSources(
	ctx context.Context,
	scope Scope,
	platform connectors.Platform,
	query string,
	limit int,
) ([]SourceView, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	var views []SourceView
	err := s.withAccount(ctx, scope.UserID, platform, 0, nil, func(conn connectors.Connector, acc database.ChannelAccount) error {
		if !connectors.Has(conn, connectors.CapSearchSources) {
			return fmt.Errorf("%w: search on %s", connectors.ErrUnsupported, platform)
		}
		found, err := conn.SearchSources(ctx, query, clampLimit(limit, 20, maxSearchLimit))
		if err != nil {
			return err
		}
		views, err = s.storeFound(ctx, scope, acc, found, OriginAI, query)
		return err
	})
	return views, err
}

func (s *Service) RecommendSources(ctx context.Context, scope Scope, sourceID int64, limit int) ([]SourceView, error) {
	src, err := s.userSource(ctx, scope.UserID, sourceID)
	if err != nil {
		return nil, err
	}
	if src.Platform == string(connectors.PlatformWeb) {
		return nil, fmt.Errorf("%w: recommendations exist only for Telegram sources", connectors.ErrUnsupported)
	}
	var views []SourceView
	err = s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID, nil,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			if !connectors.Has(conn, connectors.CapRecommendSources) {
				return fmt.Errorf("%w: recommendations on %s", connectors.ErrUnsupported, src.Platform)
			}
			src, err := s.ensureResolved(ctx, scope, conn, acc, src)
			if err != nil {
				return err
			}
			found, err := conn.RecommendSources(ctx, s.sourceRef(ctx, acc.ID, src), clampLimit(limit, 20, maxSearchLimit))
			if err != nil {
				return err
			}
			views, err = s.storeFound(ctx, scope, acc, found, OriginAI, "similar to "+sourceLabel(src))
			return err
		})
	return views, err
}

func (s *Service) storeFound(
	ctx context.Context,
	scope Scope,
	acc database.ChannelAccount,
	found []connectors.Source,
	origin, query string,
) ([]SourceView, error) {
	views := make([]SourceView, 0, len(found))
	for _, f := range found {
		gs, inserted, err := s.upsertFound(ctx, scope, acc, f, origin, query, nil)
		if err != nil {
			return views, err
		}
		v := s.viewSource(ctx, gs, acc.ID)
		v.New = inserted
		views = append(views, v)
	}
	// Sources that already produced qualified leads come first.
	sort.SliceStable(views, func(i, j int) bool { return views[i].QualifiedLeads > views[j].QualifiedLeads })
	return views, nil
}

// AddSources puts links into the pool. Telegram links are resolved when an account is
// available; a link that cannot be resolved now is kept as unresolved and resolved on
// first join or read. Web links are stored as they are.
// discoveredFrom names the pool source whose messages mentioned the links, if any.
func (s *Service) AddSources(
	ctx context.Context,
	scope Scope,
	links []string,
	origin string,
	discoveredFrom *int64,
) ([]AddSourceResult, error) {
	if discoveredFrom != nil {
		if _, err := s.userSource(ctx, scope.UserID, *discoveredFrom); err != nil {
			return nil, err
		}
	}

	results := make([]AddSourceResult, 0, len(links))
	for _, raw := range links {
		res := AddSourceResult{Input: raw}
		platform := DetectPlatform(raw)
		canonical, err := CanonicalLink(platform, raw)
		if err != nil {
			res.Result, res.Reason = AddResultInvalid, err.Error()
			results = append(results, res)
			continue
		}

		if platform == connectors.PlatformWeb {
			res, err = s.addWebSource(ctx, scope, raw, canonical, origin, discoveredFrom)
			if err != nil {
				return results, err
			}
			results = append(results, res)
			continue
		}

		var storeErr error
		err = s.withAccount(ctx, scope.UserID, platform, 0, nil, func(conn connectors.Connector, acc database.ChannelAccount) error {
			if !connectors.Has(conn, connectors.CapResolveSource) {
				return fmt.Errorf("%w: resolving links on %s", connectors.ErrUnsupported, platform)
			}
			found, err := conn.ResolveSource(ctx, canonical)
			if err != nil {
				return err
			}
			if found.Link == "" {
				found.Link = canonical
			}
			gs, inserted, err := s.upsertFound(ctx, scope, acc, found, origin, "", discoveredFrom)
			if err != nil {
				storeErr = err
				return nil
			}
			v := s.viewSource(ctx, gs, acc.ID)
			res.Source = &v
			res.Result = AddResultExisting
			if inserted {
				res.Result = AddResultAdded
			}
			return nil
		})
		switch {
		case storeErr != nil:
			return results, storeErr
		case err == nil:
			results = append(results, res)
			continue
		case errors.Is(err, connectors.ErrNotFound):
			res.Result, res.Reason = AddResultInvalid, "the platform reports no such group or channel"
			results = append(results, res)
			continue
		case ctx.Err() != nil:
			return results, ctx.Err()
		default:
			res.Reason = "kept unresolved: " + err.Error()
		}

		row, err := s.db.UpsertGrowthSource(ctx, database.UpsertGrowthSourceParams{
			UserID:           scope.UserID,
			Platform:         string(platform),
			SourceKey:        linkKey(canonical),
			Link:             canonical,
			Kind:             string(connectors.SourceKindUnknown),
			Origin:           origin,
			Status:           SourceStatusUnresolved,
			DiscoveredFromID: optInt(discoveredFrom),
			DiscoveredFlowID: scope.flowID(),
			DiscoveredTaskID: scope.taskID(),
		})
		if err != nil {
			return results, fmt.Errorf("failed to store link %q: %w", canonical, err)
		}
		v := s.viewSource(ctx, sourceFromUpsert(row), 0)
		res.Source = &v
		res.Result = AddResultUnresolved
		if !row.Inserted {
			res.Result = AddResultExisting
		}
		results = append(results, res)
	}

	return results, nil
}

func (s *Service) addWebSource(
	ctx context.Context,
	scope Scope,
	raw, canonical, origin string,
	discoveredFrom *int64,
) (AddSourceResult, error) {
	link := strings.TrimSpace(raw)
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	row, err := s.db.UpsertGrowthSource(ctx, database.UpsertGrowthSourceParams{
		UserID:           scope.UserID,
		Platform:         string(connectors.PlatformWeb),
		SourceKey:        linkKey(canonical),
		Link:             link,
		Kind:             string(connectors.SourceKindForum),
		Origin:           origin,
		Status:           SourceStatusActive,
		DiscoveredFromID: optInt(discoveredFrom),
		DiscoveredFlowID: scope.flowID(),
		DiscoveredTaskID: scope.taskID(),
	})
	if err != nil {
		return AddSourceResult{}, fmt.Errorf("failed to store link %q: %w", canonical, err)
	}
	v := s.viewSource(ctx, sourceFromUpsert(row), 0)
	res := AddSourceResult{Input: raw, Source: &v, Result: AddResultExisting}
	if row.Inserted {
		res.Result = AddResultAdded
	}
	return res, nil
}

func sourceFromUpsert(row database.UpsertGrowthSourceRow) database.GrowthSource {
	return database.GrowthSource{
		ID:               row.ID,
		UserID:           row.UserID,
		Platform:         row.Platform,
		SourceKey:        row.SourceKey,
		ExternalID:       row.ExternalID,
		Username:         row.Username,
		Title:            row.Title,
		Link:             row.Link,
		Kind:             row.Kind,
		MemberCount:      row.MemberCount,
		Language:         row.Language,
		Description:      row.Description,
		Origin:           row.Origin,
		Query:            row.Query,
		Status:           row.Status,
		StatusReason:     row.StatusReason,
		QualifiedLeads:   row.QualifiedLeads,
		QualifiedAtSync:  row.QualifiedAtSync,
		EmptyStreak:      row.EmptyStreak,
		Health:           row.Health,
		LastSyncedAt:     row.LastSyncedAt,
		DiscoveredFromID: row.DiscoveredFromID,
		DiscoveredFlowID: row.DiscoveredFlowID,
		DiscoveredTaskID: row.DiscoveredTaskID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func rawPeer(peer json.RawMessage) pqtype.NullRawMessage {
	if len(peer) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: peer, Valid: true}
}

func (s *Service) JoinSources(ctx context.Context, scope Scope, sourceIDs []int64) ([]JoinOutcome, error) {
	outcomes := make([]JoinOutcome, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		src, err := s.userSource(ctx, scope.UserID, id)
		if err != nil {
			outcomes = append(outcomes, JoinOutcome{SourceID: id, JoinStatus: "error", Reason: err.Error()})
			continue
		}
		outcome, err := s.joinOne(ctx, scope, src)
		if err != nil {
			var flood *connectors.FloodWaitError
			if errors.As(err, &flood) || errors.Is(err, ErrNoAccount) || errors.Is(err, connectors.ErrUnauthorized) ||
				errors.Is(err, connectors.ErrNoConnector) || ctx.Err() != nil {
				// Every later source would hit the same wall; stop and say so.
				outcomes = append(outcomes, JoinOutcome{SourceID: id, Title: src.Title, JoinStatus: "error", Reason: err.Error()})
				return outcomes, err
			}
			outcomes = append(outcomes, JoinOutcome{SourceID: id, Title: src.Title, JoinStatus: "error", Reason: err.Error()})
			continue
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func (s *Service) joinOne(ctx context.Context, scope Scope, src database.GrowthSource) (JoinOutcome, error) {
	if src.Platform == string(connectors.PlatformWeb) {
		return JoinOutcome{SourceID: src.ID, Title: src.Title, JoinStatus: "not_applicable",
			Reason: "web sources are public pages and need no joining"}, nil
	}
	if src.Status == SourceStatusInvalid {
		return JoinOutcome{SourceID: src.ID, Title: src.Title, JoinStatus: string(connectors.JoinStatusNotJoinable),
			Reason: "source is invalid: " + src.StatusReason}, nil
	}

	var outcome JoinOutcome
	join := s.joinIntentFor(ctx, scope.UserID, src)
	err := s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID, join,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			var err error
			outcome, err = s.joinWith(ctx, scope, conn, acc, src, join)
			return err
		})
	return outcome, err
}

func (s *Service) joinWith(
	ctx context.Context,
	scope Scope,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
	join *joinIntent,
) (JoinOutcome, error) {
	if !connectors.Has(conn, connectors.CapJoinSource) {
		return JoinOutcome{}, fmt.Errorf("%w: join on %s", connectors.ErrUnsupported, src.Platform)
	}

	// An invite link of a private group often resolves only by joining it.
	if resolved, err := s.ensureResolved(ctx, scope, conn, acc, src); err == nil {
		src = resolved
	} else if src.Status != SourceStatusUnresolved || errors.Is(err, connectors.ErrNotFound) {
		return JoinOutcome{}, err
	}

	as, err := s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{AccountID: acc.ID, SourceID: src.ID})
	if err != nil {
		return JoinOutcome{}, fmt.Errorf("failed to load account view of source %d: %w", src.ID, err)
	}
	if as.JoinStatus == string(connectors.JoinStatusJoined) {
		return JoinOutcome{SourceID: src.ID, Title: src.Title, JoinStatus: as.JoinStatus, Reason: "already joined"}, nil
	}

	res, err := conn.JoinSource(ctx, s.sourceRef(ctx, acc.ID, src))
	if err != nil {
		return JoinOutcome{}, err
	}
	if join != nil {
		_ = s.db.ScheduleChannelAccountJoin(context.WithoutCancel(ctx), database.ScheduleChannelAccountJoinParams{
			ID: acc.ID, NextJoinAt: sql.NullTime{Time: time.Now().Add(join.pace.gap()), Valid: true},
		})
	}

	if res.Source != nil && src.Status == SourceStatusUnresolved {
		gs, _, err := s.upsertFound(ctx, scope, acc, *res.Source, src.Origin, src.Query, nullInt(src.DiscoveredFromID))
		if err != nil {
			return JoinOutcome{}, err
		}
		src = gs
		if as, err = s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{
			AccountID: acc.ID, SourceID: src.ID,
		}); err != nil {
			return JoinOutcome{}, fmt.Errorf("failed to load account view of source %d: %w", src.ID, err)
		}
	}

	status := string(res.Status)
	if status == "" {
		status = string(connectors.JoinStatusNotJoinable)
	}
	if _, err := s.db.UpdateAccountSourceJoin(ctx, database.UpdateAccountSourceJoinParams{
		ID:          as.ID,
		JoinStatus:  status,
		JoinError:   res.Reason,
		PrivateJoin: join != nil && join.private,
	}); err != nil {
		return JoinOutcome{}, fmt.Errorf("failed to record join of source %d: %w", src.ID, err)
	}

	outcome := JoinOutcome{SourceID: src.ID, Title: src.Title, JoinStatus: status, Reason: res.Reason}
	if join != nil && len(join.advice[acc.ID]) > 0 {
		outcome.Advice, outcome.Warning = join.advice[acc.ID], adviceSentence(join.advice[acc.ID])
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"user_id": scope.UserID, "account_id": acc.ID, "source_id": src.ID, "advice": outcome.Advice,
		}).Info("join went past the account's suggested pace")
	}
	return outcome, nil
}

// SyncOptions shape a read. All skips the pre-screen. MaxBytes bounds what one call
// returns over all its sources, in bytes of serialized messages: a read stops where the
// bound is reached and its cursor stays before the first message left out, so the next
// call continues with it and nothing is skipped; each message's text is then cut to the
// opening lines the pre-screen judges. Zero is no bound.
type SyncOptions struct {
	Direction string
	Limit     int
	All       bool
	MaxBytes  int
}

// syncSourceOverhead is the part of a bounded read's budget set aside per source for
// what the caller wraps around the messages: the source line and the counts.
const syncSourceOverhead = 512

// syncBudget is the room a bounded read still has, shared by the sources of one call in
// equal shares of what is left, so a source with little to show leaves its share to the
// next ones.
type syncBudget struct {
	left    int
	sources int
}

func newSyncBudget(maxBytes, sources int) *syncBudget {
	if maxBytes <= 0 {
		return nil
	}
	return &syncBudget{left: max(maxBytes-sources*syncSourceOverhead, 0), sources: max(sources, 1)}
}

func (b *syncBudget) share() int {
	return b.left / b.sources
}

func (b *syncBudget) spend(used int) {
	b.left = max(b.left-used, 0)
	b.sources = max(b.sources-1, 1)
}

// SyncSource reads a source's history and advances the per-account cursor: "older"
// continues one page backwards from the oldest message read so far, "newer" reads
// forward from the newest one until it has caught up.
func (s *Service) SyncSource(
	ctx context.Context,
	scope Scope,
	sourceID int64,
	direction string,
	limit int,
) (SyncResult, error) {
	return s.syncOne(ctx, scope, sourceID, SyncOptions{Direction: direction, Limit: limit}, nil)
}

// SyncSources reads each source in turn; no ids reads every source an account of the
// owner has joined. It stops early, keeping what it read, when the platform rate-limits
// or cannot be reached, or no account can read.
func (s *Service) SyncSources(ctx context.Context, scope Scope, sourceIDs []int64, opt SyncOptions) ([]SyncResult, error) {
	if len(sourceIDs) == 0 {
		joined, err := s.joinedSourceIDs(ctx, scope.UserID)
		if err != nil {
			return nil, err
		}
		sourceIDs = joined
	}
	budget := newSyncBudget(opt.MaxBytes, len(sourceIDs))
	results := make([]SyncResult, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		res, err := s.syncOne(ctx, scope, id, opt, budget)
		if budget != nil {
			budget.spend(res.bytes)
		}
		if err != nil {
			res.Source.ID, res.Error, res.Err, res.Messages = id, err.Error(), err, []MessageView{}
			results = append(results, res)
			var flood *connectors.FloodWaitError
			if errors.As(err, &flood) || errors.Is(err, ErrNoAccount) || errors.Is(err, connectors.ErrUnauthorized) ||
				errors.Is(err, ErrNoJudge) || errors.Is(err, connectors.ErrUnreachable) ||
				errors.Is(err, connectors.ErrNoConnector) || ctx.Err() != nil {
				return results, err
			}
			continue
		}
		results = append(results, res)
	}
	return results, nil
}

func (s *Service) joinedSourceIDs(ctx context.Context, userID int64) ([]int64, error) {
	states, err := s.db.GetUserAccountSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list account sources: %w", err)
	}
	var ids []int64
	for _, st := range states {
		if connectors.JoinStatus(st.JoinStatus) == connectors.JoinStatusJoined && !slices.Contains(ids, st.SourceID) {
			ids = append(ids, st.SourceID)
		}
	}
	if len(ids) == 0 {
		return nil, ErrNoJoinedSource
	}
	slices.Sort(ids)
	return ids, nil
}

func (s *Service) syncOne(ctx context.Context, scope Scope, sourceID int64, opt SyncOptions, budget *syncBudget) (SyncResult, error) {
	src, err := s.userSource(ctx, scope.UserID, sourceID)
	if err != nil {
		return SyncResult{}, err
	}
	if src.Platform == string(connectors.PlatformWeb) {
		return SyncResult{Source: s.viewSource(ctx, src, 0)}, fmt.Errorf("%w: open %s", ErrBrowserSource, src.Link)
	}
	sc, err := s.screenerFor(ctx, scope)
	if err != nil {
		return SyncResult{Source: s.viewSource(ctx, src, 0)}, err
	}
	var (
		result   SyncResult
		judgeErr error
	)
	err = s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID, nil,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			var err error
			result, err = s.syncWith(ctx, scope, conn, acc, src, opt, sc, budget)
			// A refused judgement key says nothing about the platform account.
			if errors.Is(err, ErrNoJudge) {
				judgeErr, err = err, nil
			}
			return err
		})
	if judgeErr != nil {
		return SyncResult{Source: s.viewSource(ctx, src, 0)}, judgeErr
	}
	if err == nil && sc.fallbacks > 0 {
		s.notePrescreenFallbacks(scope, sc.fallbacks)
		result.PrescreenFallbacks = sc.fallbacks
		result.Warning = prescreenFallbackWarning(sc.fallbacks, "in this read")
	}
	return result, err
}

// maxCatchUpPages bounds one forward read; a source further behind says so and the next
// call continues from where this one stopped.
const maxCatchUpPages = 1

// syncWith reads with one account. The cursor is per account, so an account that takes
// over a source starts from its newest message; leads and evidence it finds again are
// deduplicated by the store.
func (s *Service) syncWith(
	ctx context.Context,
	scope Scope,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
	opt SyncOptions,
	sc *screener,
	budget *syncBudget,
) (SyncResult, error) {
	var err error
	if !connectors.Has(conn, connectors.CapReadHistory) {
		return SyncResult{}, fmt.Errorf("%w: reading history on %s", connectors.ErrUnsupported, src.Platform)
	}
	if src, err = s.ensureResolved(ctx, scope, conn, acc, src); err != nil {
		return SyncResult{}, err
	}

	as, err := s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{AccountID: acc.ID, SourceID: src.ID})
	if err != nil {
		return SyncResult{}, fmt.Errorf("failed to load account view of source %d: %w", src.ID, err)
	}

	limit := clampLimit(opt.Limit, defaultPageLimit, maxHistoryLimit)
	telegram := src.Platform == string(connectors.PlatformTelegram)
	pace := defaultTelegramPace
	if telegram {
		pace = s.telegramPace(ctx, scope.UserID)
	}

	label := src.Title
	if label == "" {
		label = src.Link
	}
	take := &syncTake{s: s, ctx: ctx, scope: scope, sc: sc, src: src, label: label, all: opt.All, runes: maxMessageRunes}
	if budget != nil {
		take.bounded, take.share, take.runes = true, budget.share(), screenRunes
	}
	var (
		exhausted bool
		caughtUp  bool
	)
	switch opt.Direction {
	case "", DirectionOlder:
		if as.HistoryExhausted {
			return SyncResult{Source: s.viewSource(ctx, src, acc.ID), Messages: []MessageView{}}, nil
		}
		if telegram {
			if err := s.awaitReadGap(ctx, acc.ID, src.ID, pace); err != nil {
				return SyncResult{}, err
			}
		}
		page, done, err := s.readOlder(ctx, conn, acc, src, as, limit, telegram, pace)
		if err != nil {
			return SyncResult{}, err
		}
		// A back-read continues from its oldest message, so the newest of the page is nearest the cursor.
		slices.SortFunc(page, func(a, b connectors.Message) int { return cmp.Compare(b.ID, a.ID) })
		if err := take.page(page); err != nil {
			return SyncResult{}, err
		}
		exhausted = done && take.pending == 0
	case DirectionNewer:
		if telegram {
			if err := s.awaitReadGap(ctx, acc.ID, src.ID, pace); err != nil {
				return SyncResult{}, err
			}
		}
		caughtUp, err = s.readNewer(ctx, conn, acc, src, as, limit, func(page []connectors.Message) (bool, error) {
			slices.SortFunc(page, func(a, b connectors.Message) int { return cmp.Compare(a.ID, b.ID) })
			if err := take.page(page); err != nil {
				return false, err
			}
			return take.full, nil
		})
		if err != nil {
			return SyncResult{}, err
		}
	default:
		return SyncResult{}, fmt.Errorf("unknown direction %q: use %q or %q", opt.Direction, DirectionOlder, DirectionNewer)
	}

	var newest, oldest int64
	for _, m := range take.consumed {
		newest = max(newest, m.ID)
		if oldest == 0 || m.ID < oldest {
			oldest = m.ID
		}
	}
	// A newer-direction read of a never-read source is its first page; it also anchors the older cursor.
	if opt.Direction == DirectionNewer && as.OldestMessageID != 0 {
		oldest = 0
	}
	if _, err := s.db.UpdateAccountSourceSync(ctx, database.UpdateAccountSourceSyncParams{
		ID:               as.ID,
		NewestMessageID:  newest,
		OldestMessageID:  oldest,
		HistoryExhausted: exhausted,
		MessagesRead:     int64(len(take.consumed)),
	}); err != nil {
		return SyncResult{}, fmt.Errorf("failed to advance cursor of source %d: %w", src.ID, err)
	}
	if src, err = s.db.RecordSourceRead(ctx, database.RecordSourceReadParams{
		ID:         src.ID,
		StaleAfter: staleAfterEmptyReads,
		DeadAfter:  deadAfterEmptyReads,
	}); err != nil {
		return SyncResult{}, fmt.Errorf("failed to record read of source %d: %w", src.ID, err)
	}

	kept := take.messages()
	res := SyncResult{
		Source: s.viewSource(ctx, src, acc.ID), Messages: kept, Read: len(take.consumed),
		Skipped: len(take.consumed) - len(kept), Pending: take.pending, bytes: take.used,
	}
	if take.pending > 0 {
		res.NextFrom = fmt.Sprint(take.next)
	}
	if opt.Direction == DirectionNewer {
		res.CaughtUp = &caughtUp
	}
	return res, nil
}

// syncTake walks the pages of one read in cursor order, the message nearest the cursor
// first, and keeps what the pre-screen picks while it fits the share. The cursor moves
// past every message examined; a kept message that does not fit stops the read before
// it, and it and the rest of the fetched pages are pending for the next call. The first
// kept message of a source always fits, so a read always makes progress. The pre-screen
// runs before the cursor moves, so a read it refuses is read again.
type syncTake struct {
	s     *Service
	ctx   context.Context
	scope Scope
	sc    *screener
	src   database.GrowthSource
	label string
	all   bool
	runes int

	bounded bool
	share   int
	used    int

	consumed []connectors.Message
	kept     []keptMessage
	pending  int
	next     int64
	full     bool
}

type keptMessage struct {
	id   int64
	view MessageView
}

func (t *syncTake) page(msgs []connectors.Message) error {
	for start := 0; start < len(msgs); start += screenBatch {
		end := min(start+screenBatch, len(msgs))
		if t.full {
			t.pending += end - start
			continue
		}
		batch := msgs[start:end]
		views := make([]MessageView, 0, len(batch))
		for _, m := range batch {
			views = append(views, messageView(m, t.runes))
		}
		picked := views
		if !t.all {
			var err error
			picked, err = t.s.screen(t.ctx, t.scope.UserID, t.sc, t.src.ID, t.src.Platform, t.label, views)
			if err != nil {
				return err
			}
		}
		next := 0
		for i, m := range batch {
			if next < len(picked) && picked[next].MessageID == views[i].MessageID {
				if !t.fits(picked[next]) {
					t.full, t.next = true, m.ID
					t.pending += len(batch) - i
					break
				}
				t.kept = append(t.kept, keptMessage{id: m.ID, view: picked[next]})
				next++
			}
			t.consumed = append(t.consumed, m)
		}
	}
	return nil
}

func (t *syncTake) fits(m MessageView) bool {
	if !t.bounded {
		return true
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return true
	}
	n := len(raw) + 1
	if len(t.kept) > 0 && t.used+n > t.share {
		return false
	}
	t.used += n
	return true
}

// messages returns what was kept, newest first.
func (t *syncTake) messages() []MessageView {
	slices.SortFunc(t.kept, func(a, b keptMessage) int { return cmp.Compare(b.id, a.id) })
	out := make([]MessageView, 0, len(t.kept))
	for _, k := range t.kept {
		out = append(out, k.view)
	}
	return out
}

// readOlder reads one page backwards. On Telegram the back-read of a source stops at the
// pace's first-sync window: so many days before the account first read it, or so many messages.
func (s *Service) readOlder(
	ctx context.Context,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
	as database.AccountSource,
	limit int,
	telegram bool,
	pace TelegramPace,
) ([]connectors.Message, bool, error) {
	req := connectors.HistoryRequest{OffsetID: as.OldestMessageID, Limit: limit}
	var (
		cutoff  time.Time
		bounded bool
	)
	if telegram {
		if n := int64(pace.FirstSyncMessages); n > 0 {
			left := n - as.MessagesRead
			if left <= 0 {
				return nil, true, nil
			}
			req.Limit = int(min(int64(req.Limit), left))
		}
		if pace.FirstSyncDays > 0 && as.CreatedAt.Valid {
			cutoff = as.CreatedAt.Time.AddDate(0, 0, -pace.FirstSyncDays)
		}
	}

	page, err := conn.ReadHistory(ctx, s.sourceRef(ctx, acc.ID, src), req)
	if err != nil {
		return nil, false, err
	}
	messages := page.Messages
	if !cutoff.IsZero() {
		kept := messages[:0:0]
		for _, m := range messages {
			if !m.Time.IsZero() && m.Time.Before(cutoff) {
				bounded = true
				continue
			}
			kept = append(kept, m)
		}
		messages = kept
	}
	return messages, page.Exhausted || len(messages) == 0 || bounded, nil
}

// readNewer pages forward from the cursor, each page the messages just above the last,
// handing each page to take as it arrives, until the page allowance runs out, a short
// page shows it has caught up, or take reports it is full.
func (s *Service) readNewer(
	ctx context.Context,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
	as database.AccountSource,
	limit int,
	take func(page []connectors.Message) (full bool, err error),
) (bool, error) {
	ref := s.sourceRef(ctx, acc.ID, src)
	cursor := as.NewestMessageID
	for range maxCatchUpPages {
		page, err := conn.ReadHistory(ctx, ref, connectors.HistoryRequest{MinID: cursor, Limit: limit})
		if err != nil {
			return false, err
		}
		next := cursor
		for _, m := range page.Messages {
			next = max(next, m.ID)
		}
		full, err := take(page.Messages)
		if err != nil {
			return false, err
		}
		if full {
			return false, nil
		}
		// A never-read source starts from its newest page: there is nothing above it.
		if cursor == 0 || len(page.Messages) < limit || next == cursor {
			return true, nil
		}
		cursor = next
	}
	return false, nil
}

func (s *Service) GetUser(
	ctx context.Context,
	scope Scope,
	platform connectors.Platform,
	userID string,
) (connectors.UserProfile, error) {
	var profile connectors.UserProfile
	err := s.withAccount(ctx, scope.UserID, platform, 0, nil, func(conn connectors.Connector, _ database.ChannelAccount) error {
		if !connectors.Has(conn, connectors.CapUserProfile) {
			return fmt.Errorf("%w: user profiles on %s", connectors.ErrUnsupported, platform)
		}
		var err error
		profile, err = conn.GetUser(ctx, userID)
		return err
	})
	return profile, err
}

// ReadMessage returns one message's full text. It does not move the source's read cursor
// and does not screen the message again: an evidence excerpt quotes this text.
func (s *Service) ReadMessage(ctx context.Context, scope Scope, sourceID int64, messageID string) (MessageView, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(messageID), 10, 64)
	if err != nil || id <= 0 {
		return MessageView{}, fmt.Errorf("message_id %q is not a message id", messageID)
	}
	src, err := s.userSource(ctx, scope.UserID, sourceID)
	if err != nil {
		return MessageView{}, err
	}
	if src.Platform == string(connectors.PlatformWeb) {
		return MessageView{}, fmt.Errorf("%w: open %s", ErrBrowserSource, src.Link)
	}

	var view MessageView
	err = s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID, nil,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			if !connectors.Has(conn, connectors.CapReadHistory) {
				return fmt.Errorf("%w: reading history on %s", connectors.ErrUnsupported, src.Platform)
			}
			var err error
			if src, err = s.ensureResolved(ctx, scope, conn, acc, src); err != nil {
				return err
			}
			if src.Platform == string(connectors.PlatformTelegram) {
				if err := s.awaitReadGap(ctx, acc.ID, src.ID, s.telegramPace(ctx, scope.UserID)); err != nil {
					return err
				}
			}
			page, err := conn.ReadHistory(ctx, s.sourceRef(ctx, acc.ID, src), connectors.HistoryRequest{OffsetID: id + 1, Limit: 1})
			if err != nil {
				return err
			}
			for _, m := range page.Messages {
				if m.ID == id {
					view = messageView(m, maxFullMessageRunes)
					return nil
				}
			}
			return fmt.Errorf("%w: %s", ErrMessageNotFound, messageID)
		})
	return view, err
}

// ListSources returns the pool with the membership of the user's accounts folded in:
// a source counts as joined if any account joined it.
func (s *Service) ListSources(ctx context.Context, userID int64) ([]SourceView, error) {
	sources, err := s.db.GetUserGrowthSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}
	states, err := s.db.GetUserAccountSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list account sources: %w", err)
	}
	best := make(map[int64]database.AccountSource, len(states))
	for _, st := range states {
		if cur, ok := best[st.SourceID]; !ok || joinRank(st.JoinStatus) > joinRank(cur.JoinStatus) {
			best[st.SourceID] = st
		}
	}

	views := make([]SourceView, 0, len(sources))
	for _, src := range sources {
		v := s.viewSource(ctx, src, 0)
		if st, ok := best[src.ID]; ok {
			v.JoinStatus = st.JoinStatus
			v.MessagesRead = st.MessagesRead
			v.HistoryExhausted = st.HistoryExhausted
		}
		views = append(views, v)
	}
	return views, nil
}

// SourceCursor is where reading a source stands, as the next sync would continue it:
// direction=newer reads after NewestMessageID, direction=older before OldestMessageID.
// The cursor is per account, so this is the account that read the source last; zero ids
// mean no account has read it yet.
type SourceCursor struct {
	SourceID         int64     `json:"source_id"`
	Title            string    `json:"title,omitempty"`
	NewestMessageID  int64     `json:"newest_message_id"`
	OldestMessageID  int64     `json:"oldest_message_id"`
	HistoryExhausted bool      `json:"history_exhausted"`
	MessagesRead     int64     `json:"messages_read"`
	SyncedAt         time.Time `json:"synced_at,omitzero"`
}

// SourceCursors reads the cursors of the owner's sources from the database, in the order
// of sourceIDs; ids outside the owner's pool are left out. No ids returns every source an
// account has read, most recently read first.
func (s *Service) SourceCursors(ctx context.Context, userID int64, sourceIDs []int64) ([]SourceCursor, error) {
	sources, err := s.db.GetUserGrowthSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}
	states, err := s.db.GetUserAccountSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list account sources: %w", err)
	}
	last := make(map[int64]database.AccountSource, len(states))
	for _, st := range states {
		cur, ok := last[st.SourceID]
		if !ok || st.SyncedAt.Time.After(cur.SyncedAt.Time) {
			last[st.SourceID] = st
		}
	}

	byID := make(map[int64]SourceCursor, len(sources))
	for _, src := range sources {
		c := SourceCursor{SourceID: src.ID, Title: sourceLabel(src)}
		if st, ok := last[src.ID]; ok {
			c.NewestMessageID, c.OldestMessageID = st.NewestMessageID, st.OldestMessageID
			c.HistoryExhausted, c.MessagesRead = st.HistoryExhausted, st.MessagesRead
			if st.SyncedAt.Valid {
				c.SyncedAt = st.SyncedAt.Time
			}
		}
		byID[src.ID] = c
	}

	var cursors []SourceCursor
	if len(sourceIDs) == 0 {
		for _, c := range byID {
			if !c.SyncedAt.IsZero() {
				cursors = append(cursors, c)
			}
		}
		slices.SortFunc(cursors, func(a, b SourceCursor) int {
			return cmp.Or(b.SyncedAt.Compare(a.SyncedAt), cmp.Compare(a.SourceID, b.SourceID))
		})
		return cursors, nil
	}
	seen := make(map[int64]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		if c, ok := byID[id]; ok && !seen[id] {
			seen[id] = true
			cursors = append(cursors, c)
		}
	}
	return cursors, nil
}

func (s *Service) DeleteSource(ctx context.Context, userID, sourceID int64) error {
	_, err := s.db.DeleteUserGrowthSource(ctx, database.DeleteUserGrowthSourceParams{ID: sourceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrSourceNotFound, sourceID)
	}
	return err
}

func (s *Service) UpdateSource(ctx context.Context, userID, sourceID int64, u SourceUpdate) (SourceView, error) {
	src, err := s.db.UpdateUserGrowthSource(ctx, database.UpdateUserGrowthSourceParams{
		ID: sourceID, UserID: userID, Title: trimmedOrNull(u.Title), Note: trimmedOrNull(u.Note),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SourceView{}, fmt.Errorf("%w: %d", ErrSourceNotFound, sourceID)
	}
	if err != nil {
		return SourceView{}, fmt.Errorf("failed to update source %d: %w", sourceID, err)
	}
	views, err := s.ListSources(ctx, userID)
	if err != nil {
		return SourceView{}, err
	}
	for _, v := range views {
		if v.ID == src.ID {
			return v, nil
		}
	}
	return s.viewSource(ctx, src, 0), nil
}

func trimmedOrNull(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*v), Valid: true}
}

func joinRank(status string) int {
	switch connectors.JoinStatus(status) {
	case connectors.JoinStatusJoined:
		return 3
	case connectors.JoinStatusPending:
		return 2
	case connectors.JoinStatusNotJoinable:
		return 1
	default:
		return 0
	}
}

func (s *Service) userSource(ctx context.Context, userID, sourceID int64) (database.GrowthSource, error) {
	src, err := s.db.GetUserGrowthSource(ctx, database.GetUserGrowthSourceParams{ID: sourceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return src, fmt.Errorf("%w: %d", ErrSourceNotFound, sourceID)
	}
	if err != nil {
		return src, fmt.Errorf("failed to load source %d: %w", sourceID, err)
	}
	return src, nil
}

// ensureResolved turns an unresolved row into a resolved one. When the resolved id
// already has a row (the group was also found by search), the placeholder is dropped
// and the existing row returned.
func (s *Service) ensureResolved(
	ctx context.Context,
	scope Scope,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
) (database.GrowthSource, error) {
	if src.Status != SourceStatusUnresolved {
		return src, nil
	}
	if !connectors.Has(conn, connectors.CapResolveSource) {
		return src, fmt.Errorf("%w: resolving links on %s", connectors.ErrUnsupported, src.Platform)
	}

	found, err := conn.ResolveSource(ctx, src.Link)
	if errors.Is(err, connectors.ErrNotFound) {
		_, _ = s.db.UpdateGrowthSourceStatus(ctx, database.UpdateGrowthSourceStatusParams{
			ID: src.ID, Status: SourceStatusInvalid, StatusReason: "the platform reports no such group or channel",
		})
		return src, err
	}
	if err != nil {
		return src, err
	}

	existing, err := s.db.GetGrowthSourceByKey(ctx, database.GetGrowthSourceByKeyParams{
		UserID: scope.UserID, Platform: src.Platform, SourceKey: idKey(found.ExternalID),
	})
	if err == nil && existing.ID != src.ID {
		_, _ = s.db.DeleteUserGrowthSource(ctx, database.DeleteUserGrowthSourceParams{ID: src.ID, UserID: scope.UserID})
		if _, err := s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{
			AccountID: acc.ID, SourceID: existing.ID, Peer: rawPeer(found.Peer),
		}); err != nil {
			return existing, fmt.Errorf("failed to store account view of source %d: %w", existing.ID, err)
		}
		return existing, nil
	}

	kind := string(found.Kind)
	if kind == "" {
		kind = string(connectors.SourceKindUnknown)
	}
	resolved, err := s.db.ResolveGrowthSource(ctx, database.ResolveGrowthSourceParams{
		ID:          src.ID,
		SourceKey:   idKey(found.ExternalID),
		ExternalID:  found.ExternalID,
		Username:    found.Username,
		Title:       found.Title,
		Link:        found.Link,
		Kind:        kind,
		MemberCount: found.MemberCount,
		Description: found.Description,
	})
	if err != nil {
		return src, fmt.Errorf("failed to resolve source %d: %w", src.ID, err)
	}
	if _, err := s.db.UpsertAccountSource(ctx, database.UpsertAccountSourceParams{
		AccountID: acc.ID, SourceID: resolved.ID, Peer: rawPeer(found.Peer),
	}); err != nil {
		return resolved, fmt.Errorf("failed to store account view of source %d: %w", resolved.ID, err)
	}
	return resolved, nil
}

func (s *Service) sourceRef(ctx context.Context, accountID int64, src database.GrowthSource) connectors.SourceRef {
	ref := connectors.SourceRef{ExternalID: src.ExternalID, Username: src.Username, Link: src.Link}
	if as, err := s.db.GetAccountSource(ctx, database.GetAccountSourceParams{
		AccountID: accountID, SourceID: src.ID,
	}); err == nil && as.Peer.Valid {
		ref.Peer = as.Peer.RawMessage
	}
	return ref
}

func messageView(m connectors.Message, textRunes int) MessageView {
	v := MessageView{
		MessageID:      fmt.Sprint(m.ID),
		SenderID:       m.SenderID,
		SenderUsername: m.SenderUsername,
		SenderName:     m.SenderName,
		SenderIsBot:    m.SenderIsBot,
		Time:           m.Time.UTC(),
		Link:           m.Link,
		Text:           truncateRunes(m.Text, textRunes),
	}
	if m.ReplyToID != 0 {
		v.ReplyTo = fmt.Sprint(m.ReplyToID)
	}
	return v
}

func sourceLabel(src database.GrowthSource) string {
	switch {
	case src.Title != "":
		return src.Title
	case src.Username != "":
		return "@" + src.Username
	default:
		return src.Link
	}
}

func optInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func clampLimit(limit, def, maxLimit int) int {
	if limit <= 0 {
		return def
	}
	return min(limit, maxLimit)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
