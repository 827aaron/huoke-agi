package growth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

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
	maxHistoryLimit  = 200
	maxMessageRunes  = 1500
	maxDescRunes     = 300
	defaultPageLimit = 100
)

var (
	ErrSourceNotFound = errors.New("source not found in the pool")
	// ErrBrowserSource is returned for operations a web source does not support:
	// its pages are read with the browser tool, not through a connector.
	ErrBrowserSource = errors.New("web sources are read with the browser tool")
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
	MessagesRead     int64  `json:"messages_read"`
	HistoryExhausted bool   `json:"history_exhausted"`
	QualifiedLeads   int    `json:"qualified_leads"`
	EmptyStreak      int    `json:"empty_streak"`
	Health           string `json:"health"`
	LastSyncedAt     string `json:"last_synced_at,omitempty"`
	DiscoveredFrom   *int64 `json:"discovered_from,omitempty"`
	New              bool   `json:"new,omitempty"`
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

type JoinOutcome struct {
	SourceID   int64  `json:"source_id"`
	Title      string `json:"title,omitempty"`
	JoinStatus string `json:"join_status"`
	Reason     string `json:"reason,omitempty"`
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
}

type SyncResult struct {
	Source   SourceView    `json:"source"`
	Messages []MessageView `json:"messages"`
}

// DetectPlatform tells a Telegram reference from a web page; anything that is not
// a Telegram link, @username or bare username is treated as a web page.
// DetectPlatform treats anything on a Telegram host, or an @username, as Telegram even
// when it is malformed, so a broken t.me link is reported invalid rather than kept as a web page.
func DetectPlatform(raw string) connectors.Platform {
	raw = strings.TrimSpace(raw)
	if _, err := canonicalTelegramLink(raw); err == nil || strings.HasPrefix(raw, "@") {
		return connectors.PlatformTelegram
	}
	host := strings.ToLower(raw)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	switch strings.TrimPrefix(host, "www.") {
	case "t.me", "telegram.me", "telegram.dog":
		return connectors.PlatformTelegram
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
	return canonicalWebURL(raw)
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

		QualifiedLeads: int(src.QualifiedLeads),
		EmptyStreak:    int(src.EmptyStreak),
		Health:         src.Health,
		DiscoveredFrom: nullInt(src.DiscoveredFromID),
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
	err := s.withAccount(ctx, scope.UserID, platform, 0, func(conn connectors.Connector, acc database.ChannelAccount) error {
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
	err = s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID,
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
		err = s.withAccount(ctx, scope.UserID, platform, 0, func(conn connectors.Connector, acc database.ChannelAccount) error {
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
	err := s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			var err error
			outcome, err = s.joinWith(ctx, scope, conn, acc, src)
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
		ID:         as.ID,
		JoinStatus: status,
		JoinError:  res.Reason,
	}); err != nil {
		return JoinOutcome{}, fmt.Errorf("failed to record join of source %d: %w", src.ID, err)
	}

	return JoinOutcome{SourceID: src.ID, Title: src.Title, JoinStatus: status, Reason: res.Reason}, nil
}

// SyncSource reads one page of a source's history and advances the per-account cursor:
// "older" continues backwards from the oldest message read so far, "newer" fetches what
// arrived after the newest one.
func (s *Service) SyncSource(
	ctx context.Context,
	scope Scope,
	sourceID int64,
	direction string,
	limit int,
) (SyncResult, error) {
	src, err := s.userSource(ctx, scope.UserID, sourceID)
	if err != nil {
		return SyncResult{}, err
	}
	if src.Platform == string(connectors.PlatformWeb) {
		return SyncResult{Source: s.viewSource(ctx, src, 0)}, fmt.Errorf("%w: open %s", ErrBrowserSource, src.Link)
	}
	var result SyncResult
	err = s.withAccount(ctx, scope.UserID, connectors.Platform(src.Platform), src.ID,
		func(conn connectors.Connector, acc database.ChannelAccount) error {
			var err error
			result, err = s.syncWith(ctx, scope, conn, acc, src, direction, limit)
			return err
		})
	return result, err
}

// syncWith reads with one account. The cursor is per account, so an account that takes
// over a source starts from its newest message; leads and evidence it finds again are
// deduplicated by the store.
func (s *Service) syncWith(
	ctx context.Context,
	scope Scope,
	conn connectors.Connector,
	acc database.ChannelAccount,
	src database.GrowthSource,
	direction string,
	limit int,
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

	req := connectors.HistoryRequest{Limit: clampLimit(limit, defaultPageLimit, maxHistoryLimit)}
	switch direction {
	case "", DirectionOlder:
		direction = DirectionOlder
		if as.HistoryExhausted {
			return SyncResult{Source: s.viewSource(ctx, src, acc.ID), Messages: []MessageView{}}, nil
		}
		req.OffsetID = as.OldestMessageID
	case DirectionNewer:
		req.MinID = as.NewestMessageID
	default:
		return SyncResult{}, fmt.Errorf("unknown direction %q: use %q or %q", direction, DirectionOlder, DirectionNewer)
	}

	page, err := conn.ReadHistory(ctx, s.sourceRef(ctx, acc.ID, src), req)
	if err != nil {
		return SyncResult{}, err
	}

	var newest, oldest int64
	views := make([]MessageView, 0, len(page.Messages))
	for _, m := range page.Messages {
		newest = max(newest, m.ID)
		if oldest == 0 || m.ID < oldest {
			oldest = m.ID
		}
		views = append(views, messageView(m))
	}

	// A newer-direction read of a never-read source is its first page; it also anchors the older cursor.
	exhausted := direction == DirectionOlder && (page.Exhausted || len(page.Messages) == 0)
	if direction == DirectionNewer && as.OldestMessageID != 0 {
		oldest = 0
	}
	if _, err := s.db.UpdateAccountSourceSync(ctx, database.UpdateAccountSourceSyncParams{
		ID:               as.ID,
		NewestMessageID:  newest,
		OldestMessageID:  oldest,
		HistoryExhausted: exhausted,
		MessagesRead:     int64(len(page.Messages)),
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

	return SyncResult{Source: s.viewSource(ctx, src, acc.ID), Messages: views}, nil
}

func (s *Service) GetUser(
	ctx context.Context,
	scope Scope,
	platform connectors.Platform,
	userID string,
) (connectors.UserProfile, error) {
	var profile connectors.UserProfile
	err := s.withAccount(ctx, scope.UserID, platform, 0, func(conn connectors.Connector, _ database.ChannelAccount) error {
		if !connectors.Has(conn, connectors.CapUserProfile) {
			return fmt.Errorf("%w: user profiles on %s", connectors.ErrUnsupported, platform)
		}
		var err error
		profile, err = conn.GetUser(ctx, userID)
		return err
	})
	return profile, err
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

func (s *Service) DeleteSource(ctx context.Context, userID, sourceID int64) error {
	_, err := s.db.DeleteUserGrowthSource(ctx, database.DeleteUserGrowthSourceParams{ID: sourceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrSourceNotFound, sourceID)
	}
	return err
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

func messageView(m connectors.Message) MessageView {
	v := MessageView{
		MessageID:      fmt.Sprint(m.ID),
		SenderID:       m.SenderID,
		SenderUsername: m.SenderUsername,
		SenderName:     m.SenderName,
		SenderIsBot:    m.SenderIsBot,
		Time:           m.Time.UTC(),
		Link:           m.Link,
		Text:           truncateRunes(m.Text, maxMessageRunes),
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
