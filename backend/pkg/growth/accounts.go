package growth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"
	"pentagi/pkg/system"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
)

const (
	ProxySourceAccount = "account"
	ProxySourceGlobal  = "global"
	ProxySourceDirect  = "direct"

	accountFieldProxy     = "proxy"
	accountFieldBrowserWS = "external_browser_ws"
)

var ErrAccountNotFound = errors.New("account not found")

// AccountView is an account as the settings panel shows it; the session never leaves the backend.
type AccountView struct {
	ID                int64      `json:"id"`
	Platform          string     `json:"platform"`
	Kind              string     `json:"kind"`
	Label             string     `json:"label"`
	ExternalID        string     `json:"external_id,omitempty"`
	Status            string     `json:"status"`
	Available         bool       `json:"available"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastRateLimitedAt *time.Time `json:"last_rate_limited_at,omitempty"`
	RateLimitUntil    *time.Time `json:"rate_limit_until,omitempty"`
	ConsecutiveErrors int        `json:"consecutive_errors"`
	LastError         string     `json:"last_error,omitempty"`
	// ProxyURL is the account's own proxy, masked; ProxySource says which proxy its
	// traffic takes: its own, the customer's default, or none.
	ProxyURL          string          `json:"proxy,omitempty"`
	ProxySource       string          `json:"proxy_source"`
	Fingerprint       json.RawMessage `json:"fingerprint,omitempty" swaggertype:"object"`
	ExternalBrowserWS string          `json:"external_browser_ws,omitempty"`
	// The join pace counts days from FirstSignedInAt; NextJoinAt is the earliest next join
	// its interval allows. JoinedSources and JoinsToday (last 24 hours) are filled by ListAccounts.
	FirstSignedInAt *time.Time `json:"first_signed_in_at,omitempty"`
	NextJoinAt      *time.Time `json:"next_join_at,omitempty"`
	JoinedSources   int64      `json:"joined_sources"`
	JoinsToday      int64      `json:"joins_today"`
	// JoinAdvice holds the reminder codes a next join would raise, plus joined_groups_high
	// at the joined-groups reminder count; empty with reminders off.
	JoinAdvice []string `json:"join_advice,omitempty"`
}

// AccountUpdate changes an account; a nil field keeps its value and an empty string
// clears it. A value still carrying the mask "••••" is the page resubmitting what it
// was shown and keeps the stored one. Fingerprint JSON null clears it.
type AccountUpdate struct {
	Label             *string         `json:"label,omitempty"`
	Status            *string         `json:"status,omitempty"`
	ProxyURL          *string         `json:"proxy,omitempty"`
	Fingerprint       json.RawMessage `json:"fingerprint,omitempty" swaggertype:"object"`
	ExternalBrowserWS *string         `json:"external_browser_ws,omitempty"`
}

// keyedPlatforms are reached with the customer's API keys and no sign-in. They still need
// an account row, for the picker and for health; it is added on first use, while keys exist,
// and kept out of the accounts the customer manages.
var keyedPlatforms = map[connectors.Platform]func(*config.Config) bool{
	"youtube": func(c *config.Config) bool { return len(c.YouTubeAPIKeys()) > 0 },
}

const keyedAccountLabel = "api keys"

func (s *Service) keyedAccount(ctx context.Context, userID int64, platform connectors.Platform) (database.ChannelAccount, bool, error) {
	hasKeys, ok := keyedPlatforms[platform]
	if !ok {
		return database.ChannelAccount{}, false, nil
	}
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil || !hasKeys(cfg) {
		return database.ChannelAccount{}, false, err
	}
	acc, err := s.EnsureAccount(ctx, userID, AccountSpec{
		Platform: platform, Kind: AccountKindAPISession, Label: keyedAccountLabel, Status: AccountStatusActive,
	})
	return acc, err == nil, err
}

func (s *Service) ListAccounts(ctx context.Context, userID int64) ([]AccountView, error) {
	accounts, err := s.db.GetUserChannelAccounts(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	counts, err := s.db.GetUserAccountJoinCounts(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to count account joins: %w", err)
	}
	pace := s.telegramPace(ctx, userID)
	byAccount := make(map[int64]database.GetUserAccountJoinCountsRow, len(counts))
	for _, c := range counts {
		byAccount[c.AccountID] = c
	}
	views := make([]AccountView, 0, len(accounts))
	for _, a := range accounts {
		if _, keyed := keyedPlatforms[connectors.Platform(a.Platform)]; keyed {
			continue
		}
		v := s.accountView(a, cfg)
		c := byAccount[a.ID]
		v.JoinedSources, v.JoinsToday = c.Joined, c.Joins24h
		if a.Platform == string(connectors.PlatformTelegram) {
			l := joinLoad{joins: c.Joins24h, private: c.PrivateJoins24h}
			if a.FirstSignedInAt.Valid {
				l.signedInAt = a.FirstSignedInAt.Time
			} else if a.CreatedAt.Valid {
				l.signedInAt = a.CreatedAt.Time
			}
			if a.NextJoinAt.Valid {
				l.nextJoinAt = a.NextJoinAt.Time
			}
			v.JoinAdvice = pace.advice(time.Now(), l, false)
			if pace.Reminders && pace.WarnAt > 0 && c.Joined >= int64(pace.WarnAt) {
				v.JoinAdvice = append(v.JoinAdvice, AdviceJoinedGroups)
			}
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *Service) accountView(a database.ChannelAccount, cfg *config.Config) AccountView {
	now := time.Now()
	v := AccountView{
		ID:                a.ID,
		Platform:          a.Platform,
		Kind:              a.Kind,
		Label:             a.Label,
		ExternalID:        a.ExternalID,
		Status:            a.Status,
		LastUsedAt:        nullTime(a.LastUsedAt.Time, a.LastUsedAt.Valid),
		LastSuccessAt:     nullTime(a.LastSuccessAt.Time, a.LastSuccessAt.Valid),
		LastRateLimitedAt: nullTime(a.LastRateLimitedAt.Time, a.LastRateLimitedAt.Valid),
		ConsecutiveErrors: int(a.ConsecutiveErrors),
		LastError:         a.LastError,
		ProxySource:       ProxySourceDirect,
		FirstSignedInAt:   nullTime(a.FirstSignedInAt.Time, a.FirstSignedInAt.Valid),
	}
	if a.NextJoinAt.Valid && a.NextJoinAt.Time.After(now) {
		v.NextJoinAt = nullTime(a.NextJoinAt.Time, true)
	}
	resting := a.RateLimitUntil.Valid && a.RateLimitUntil.Time.After(now)
	if resting {
		v.RateLimitUntil = nullTime(a.RateLimitUntil.Time, true)
	}
	v.Available = (a.Status == AccountStatusActive || a.Status == AccountStatusRateLimited) && !resting

	if len(a.Proxy) > 0 {
		v.ProxySource, v.ProxyURL = ProxySourceAccount, "••••"
		if own, err := s.accountSecret(a, accountFieldProxy, a.Proxy); err == nil {
			v.ProxyURL = maskEndpoint(own)
		}
	} else if cfg.GrowthProxyURL != "" {
		v.ProxySource = ProxySourceGlobal
	}
	if a.Fingerprint.Valid {
		v.Fingerprint = a.Fingerprint.RawMessage
	}
	if len(a.ExternalBrowserWs) > 0 {
		v.ExternalBrowserWS = "••••"
		if ws, err := s.accountSecret(a, accountFieldBrowserWS, a.ExternalBrowserWs); err == nil {
			v.ExternalBrowserWS = maskEndpoint(ws)
		}
	}
	return v
}

func accountAAD(userID int64, field string) []byte {
	return []byte(fmt.Sprintf("channel_accounts:%d:%s", userID, field))
}

func (s *Service) accountSecret(a database.ChannelAccount, field string, blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", nil
	}
	plain, err := s.openBytes(blob, accountAAD(a.UserID, field))
	if err != nil {
		return "", fmt.Errorf("the %s of account %d does not open with the current GROWTH_SECRET_KEY", field, a.ID)
	}
	return string(plain), nil
}

// sealAccountField applies one change of an AccountUpdate to a sealed column.
func (s *Service) sealAccountField(
	a database.ChannelAccount,
	field string,
	current []byte,
	value *string,
	parse func(string) error,
) ([]byte, error) {
	if value == nil || strings.Contains(*value, "••••") {
		return current, nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil, nil
	}
	if err := parse(v); err != nil {
		return nil, err
	}
	return s.sealBytes([]byte(v), accountAAD(a.UserID, field))
}

// resolveProxy returns the proxy an account's traffic takes: its own, else the
// customer's default, else none. An own proxy that does not open is an error rather
// than a silent direct connection, which would expose the server's address.
func (s *Service) resolveProxy(a database.ChannelAccount, cfg *config.Config) (string, string, error) {
	own, err := s.accountSecret(a, accountFieldProxy, a.Proxy)
	switch {
	case err != nil:
		return "", "", err
	case own != "":
		return own, ProxySourceAccount, nil
	case cfg.GrowthProxyURL != "":
		return cfg.GrowthProxyURL, ProxySourceGlobal, nil
	default:
		return "", ProxySourceDirect, nil
	}
}

func (s *Service) userAccount(ctx context.Context, userID, accountID int64) (database.ChannelAccount, error) {
	a, err := s.db.GetUserChannelAccount(ctx, database.GetUserChannelAccountParams{ID: accountID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrAccountNotFound
	}
	if err != nil {
		return a, fmt.Errorf("failed to load account %d: %w", accountID, err)
	}
	return a, nil
}

// UpdateAccount changes the label, the operator status (active or disabled) and the
// network identity of an account. The connector is rebuilt on its next call.
func (s *Service) UpdateAccount(ctx context.Context, userID, accountID int64, u AccountUpdate) (AccountView, error) {
	a, err := s.userAccount(ctx, userID, accountID)
	if err != nil {
		return AccountView{}, err
	}
	params := database.UpdateUserChannelAccountParams{
		ID:                a.ID,
		UserID:            userID,
		Label:             a.Label,
		Status:            a.Status,
		Proxy:             a.Proxy,
		Fingerprint:       a.Fingerprint,
		ExternalBrowserWs: a.ExternalBrowserWs,
	}
	if u.Label != nil {
		if params.Label = strings.TrimSpace(*u.Label); params.Label == "" {
			return AccountView{}, fmt.Errorf("%w: label cannot be empty", ErrInvalidSetting)
		}
	}
	if u.Status != nil {
		if *u.Status != AccountStatusActive && *u.Status != AccountStatusDisabled {
			return AccountView{}, fmt.Errorf("%w: status can be set to %s or %s",
				ErrInvalidSetting, AccountStatusActive, AccountStatusDisabled)
		}
		params.Status = *u.Status
	}
	if u.ProxyURL != nil || u.ExternalBrowserWS != nil {
		if _, err := s.sealer(); err != nil {
			return AccountView{}, err
		}
	}
	params.Proxy, err = s.sealAccountField(a, accountFieldProxy, a.Proxy, u.ProxyURL, func(v string) error {
		_, err := parseProxyURL(v)
		return err
	})
	if err != nil {
		return AccountView{}, err
	}
	params.ExternalBrowserWs, err = s.sealAccountField(a, accountFieldBrowserWS, a.ExternalBrowserWs, u.ExternalBrowserWS,
		func(v string) error {
			_, err := parseEndpointURL(v)
			return err
		})
	if err != nil {
		return AccountView{}, err
	}
	if u.Fingerprint != nil {
		params.Fingerprint = pqtype.NullRawMessage{}
		if fp := strings.TrimSpace(string(u.Fingerprint)); fp != "null" {
			var obj map[string]any
			if err := json.Unmarshal(u.Fingerprint, &obj); err != nil {
				return AccountView{}, fmt.Errorf("%w: fingerprint must be a JSON object", ErrInvalidSetting)
			}
			params.Fingerprint = pqtype.NullRawMessage{RawMessage: u.Fingerprint, Valid: true}
		}
	}

	updated, err := s.db.UpdateUserChannelAccount(ctx, params)
	if err != nil {
		return AccountView{}, fmt.Errorf("failed to update account %d: %w", accountID, err)
	}
	s.DropConnector(accountID)
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return AccountView{}, err
	}
	return s.accountView(updated, cfg), nil
}

// CreateAccount adds an account a login or a browser profile attaches to later.
func (s *Service) CreateAccount(ctx context.Context, userID int64, spec AccountSpec, u AccountUpdate) (AccountView, error) {
	if strings.TrimSpace(string(spec.Platform)) == "" {
		return AccountView{}, fmt.Errorf("%w: platform is required", ErrInvalidSetting)
	}
	if spec.Kind != "" && spec.Kind != AccountKindAPISession && spec.Kind != AccountKindBrowserSession {
		return AccountView{}, fmt.Errorf("%w: kind must be %s or %s",
			ErrInvalidSetting, AccountKindAPISession, AccountKindBrowserSession)
	}
	acc, err := s.EnsureAccount(ctx, userID, spec)
	if err != nil {
		return AccountView{}, err
	}
	u.Label = nil
	return s.UpdateAccount(ctx, userID, acc.ID, u)
}

// TestAccountProxy fetches proxyCheckURL the way the account's traffic would go, or
// through candidate when it is set: a proxy typed into the page and not saved yet.
func (s *Service) TestAccountProxy(ctx context.Context, userID, accountID int64, candidate string) (ProxyCheck, error) {
	a, err := s.userAccount(ctx, userID, accountID)
	if err != nil {
		return ProxyCheck{}, err
	}
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return ProxyCheck{}, err
	}
	proxyURL, source, err := s.resolveProxy(a, cfg)
	if candidate = strings.TrimSpace(candidate); candidate != "" && !strings.Contains(candidate, "••••") {
		if _, perr := parseProxyURL(candidate); perr != nil {
			return ProxyCheck{}, perr
		}
		proxyURL, source, err = candidate, ProxySourceAccount, nil
	}
	if err != nil {
		return ProxyCheck{Source: ProxySourceAccount, Error: err.Error()}, nil
	}
	check := ProxyCheck{Source: source, Proxy: maskEndpoint(proxyURL)}

	var latency time.Duration
	if proxyURL != "" {
		latency, err = checkProxy(ctx, cfg, proxyURL)
	} else {
		latency, err = checkDirect(ctx, cfg)
	}
	if err != nil {
		check.Error = err.Error()
		return check, nil
	}
	check.OK, check.LatencyMs = true, latency.Milliseconds()
	return check, nil
}

func checkDirect(ctx context.Context, cfg *config.Config) (time.Duration, error) {
	client, err := system.GetHTTPClient(cfg)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyCheckURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return time.Since(start), nil
}

func nullTime(t time.Time, valid bool) *time.Time {
	if !valid {
		return nil
	}
	t = t.UTC()
	return &t
}

// pickAccount chooses the account the next platform call goes to: one not resting after
// a rate limit, preferring an account that already joined sourceID (its cursor and
// membership are there), then the fewest errors in a row, then the longest unused.
// With every usable account resting it returns a FloodWaitError for the shortest rest.
// For a join, an account that already joined sourceID comes first, then one whose next
// join fits the suggested pace; the advice of the account picked is left in join.
func (s *Service) pickAccount(
	ctx context.Context,
	userID int64,
	platform connectors.Platform,
	sourceID int64,
	tried map[int64]bool,
	join *joinIntent,
) (database.ChannelAccount, error) {
	rows, err := s.db.GetUsableChannelAccounts(ctx, database.GetUsableChannelAccountsParams{
		UserID:   userID,
		Platform: string(platform),
		SourceID: sourceID,
	})
	if err != nil {
		return database.ChannelAccount{}, fmt.Errorf("failed to load %s accounts: %w", platform, err)
	}
	if len(rows) == 0 && len(tried) == 0 {
		if acc, ok, err := s.keyedAccount(ctx, userID, platform); ok || err != nil {
			return acc, err
		}
	}

	now := time.Now()
	var (
		ready   []database.GetUsableChannelAccountsRow
		soonest time.Time
		advice  = map[int64][]string{}
	)
	for _, r := range rows {
		if tried[r.ID] {
			continue
		}
		if r.RateLimitUntil.Valid && r.RateLimitUntil.Time.After(now) {
			if soonest.IsZero() || r.RateLimitUntil.Time.Before(soonest) {
				soonest = r.RateLimitUntil.Time
			}
			continue
		}
		if join != nil && !r.JoinedSource {
			advice[r.ID] = join.pace.advice(now, loadOf(r), join.private)
		}
		ready = append(ready, r)
	}
	if len(ready) == 0 {
		if !soonest.IsZero() {
			return database.ChannelAccount{}, &connectors.FloodWaitError{Wait: soonest.Sub(now)}
		}
		return database.ChannelAccount{}, fmt.Errorf("%w for %s: log an account in first", ErrNoAccount, platform)
	}

	sort.SliceStable(ready, func(i, j int) bool {
		a, b := ready[i], ready[j]
		if a.JoinedSource != b.JoinedSource {
			return a.JoinedSource
		}
		if len(advice[a.ID]) != len(advice[b.ID]) {
			return len(advice[a.ID]) < len(advice[b.ID])
		}
		if a.ConsecutiveErrors != b.ConsecutiveErrors {
			return a.ConsecutiveErrors < b.ConsecutiveErrors
		}
		if a.LastUsedAt.Valid != b.LastUsedAt.Valid {
			return !a.LastUsedAt.Valid
		}
		return a.LastUsedAt.Time.Before(b.LastUsedAt.Time)
	})
	r := ready[0]
	if join != nil {
		join.advice[r.ID] = advice[r.ID]
	}
	return database.ChannelAccount{
		ID:                r.ID,
		UserID:            r.UserID,
		Platform:          r.Platform,
		Kind:              r.Kind,
		Label:             r.Label,
		ExternalID:        r.ExternalID,
		ProfileDir:        r.ProfileDir,
		Status:            r.Status,
		Proxy:             r.Proxy,
		Fingerprint:       r.Fingerprint,
		ExternalBrowserWs: r.ExternalBrowserWs,
	}, nil
}

func loadOf(r database.GetUsableChannelAccountsRow) joinLoad {
	l := joinLoad{joins: r.Joins24h, private: r.PrivateJoins24h}
	switch {
	case r.FirstSignedInAt.Valid:
		l.signedInAt = r.FirstSignedInAt.Time
	case r.CreatedAt.Valid:
		l.signedInAt = r.CreatedAt.Time
	}
	if r.NextJoinAt.Valid {
		l.nextJoinAt = r.NextJoinAt.Time
	}
	return l
}

// withAccount runs op on the account pickAccount chooses and moves on to the next one
// when the platform rate-limits or rejects it. Once no account is left it returns the
// shortest rate limit seen, or else the last error. sourceID 0 means no preference;
// join is nil unless op joins sourceID.
func (s *Service) withAccount(
	ctx context.Context,
	userID int64,
	platform connectors.Platform,
	sourceID int64,
	join *joinIntent,
	op func(conn connectors.Connector, acc database.ChannelAccount) error,
) error {
	tried := make(map[int64]bool)
	var (
		lastErr error
		flood   *connectors.FloodWaitError
	)
	keepShortest := func(err error) {
		var f *connectors.FloodWaitError
		if errors.As(err, &f) && (flood == nil || f.Wait < flood.Wait) {
			flood = f
		}
	}

	for {
		acc, err := s.pickAccount(ctx, userID, platform, sourceID, tried, join)
		if err != nil {
			keepShortest(err)
			switch {
			case flood != nil:
				return flood
			case lastErr != nil && errors.Is(err, ErrNoAccount):
				return lastErr
			default:
				return err
			}
		}
		tried[acc.ID] = true

		conn, err := s.connectorFor(ctx, acc)
		if err != nil {
			return err
		}
		_ = s.db.MarkChannelAccountUsed(context.WithoutCancel(ctx), acc.ID)

		err = op(conn, acc)
		s.recordOutcome(ctx, acc.ID, err)
		if err == nil || ctx.Err() != nil {
			return err
		}
		var f *connectors.FloodWaitError
		if !errors.As(err, &f) && !errors.Is(err, connectors.ErrUnauthorized) {
			return err
		}
		keepShortest(err)
		lastErr = err
	}
}

// recordOutcome keeps the account's health in step with what the platform answered.
// Not-found and unsupported answers say nothing about the account.
func (s *Service) recordOutcome(ctx context.Context, accountID int64, err error) {
	ctx = context.WithoutCancel(ctx)
	var flood *connectors.FloodWaitError
	switch {
	case err == nil:
		_ = s.db.RecordChannelAccountSuccess(ctx, accountID)
	case errors.As(err, &flood):
		_ = s.db.RecordChannelAccountRateLimit(ctx, database.RecordChannelAccountRateLimitParams{
			ID: accountID, Until: time.Now().Add(flood.Wait),
		})
	case errors.Is(err, connectors.ErrUnauthorized):
		_, _ = s.db.UpdateChannelAccountStatus(ctx, database.UpdateChannelAccountStatusParams{
			ID: accountID, Status: AccountStatusNeedsLogin,
		})
		// Asynchronous, because a connector may report from inside its own construction,
		// while connectorFor holds the lock DropConnector takes.
		go s.DropConnector(accountID)
	case errors.Is(err, connectors.ErrNotFound), errors.Is(err, connectors.ErrUnsupported),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
	default:
		_ = s.db.RecordChannelAccountError(ctx, database.RecordChannelAccountErrorParams{
			ID: accountID, LastError: truncateRunes(err.Error(), 500),
		})
	}
}

// dialogCountTimeout bounds the one Telegram call an account's detail makes; the
// detail is shown without the count when it runs out.
var dialogCountTimeout = 10 * time.Second

// AccountDetail is what an account has joined, from our own join records. DialogsCount is
// every chat the platform account is in, counted live; nil when the platform cannot
// count or the account could not be asked.
type AccountDetail struct {
	JoinedGroupsCount int                  `json:"joined_groups_count"`
	DialogsCount      *int                 `json:"dialogs_count,omitempty"`
	Groups            []AccountJoinedGroup `json:"groups"`
}

type AccountJoinedGroup struct {
	SourceID     int64      `json:"source_id"`
	Title        string     `json:"title"`
	Link         string     `json:"link"`
	MemberCount  int64      `json:"member_count"`
	JoinedAt     *time.Time `json:"joined_at,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
}

func (s *Service) AccountDetail(ctx context.Context, userID, accountID int64) (AccountDetail, error) {
	acc, err := s.userAccount(ctx, userID, accountID)
	if err != nil {
		return AccountDetail{}, err
	}
	rows, err := s.db.GetAccountJoinedSources(ctx, database.GetAccountJoinedSourcesParams{UserID: userID, AccountID: acc.ID})
	if err != nil {
		return AccountDetail{}, fmt.Errorf("failed to list the groups of account %d: %w", acc.ID, err)
	}
	d := AccountDetail{JoinedGroupsCount: len(rows), Groups: make([]AccountJoinedGroup, 0, len(rows))}
	for _, r := range rows {
		d.Groups = append(d.Groups, AccountJoinedGroup{
			SourceID: r.SourceID, Title: r.Title, Link: r.Link, MemberCount: r.MemberCount,
			JoinedAt: nullTime(r.JoinedAt.Time, r.JoinedAt.Valid), LastSyncedAt: nullTime(r.SyncedAt.Time, r.SyncedAt.Valid),
		})
	}
	if acc.Status != AccountStatusActive {
		return d, nil
	}
	conn, err := s.connectorFor(ctx, acc)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("account_id", acc.ID).Warn("account detail without its dialog count")
		return d, nil
	}
	if counter, ok := conn.(connectors.DialogCounter); ok {
		cctx, cancel := context.WithTimeout(ctx, dialogCountTimeout)
		defer cancel()
		if n, err := counter.CountDialogs(cctx); err == nil {
			d.DialogsCount = &n
		} else {
			logrus.WithContext(ctx).WithError(err).WithField("account_id", acc.ID).Warn("account detail without its dialog count")
		}
	}
	return d, nil
}

func (s *Service) connectorFor(ctx context.Context, acc database.ChannelAccount) (connectors.Connector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conn, ok := s.conns[acc.ID]; ok {
		return conn, nil
	}

	platform := connectors.Platform(acc.Platform)
	factory, ok := connectors.Lookup(platform)
	if !ok {
		return nil, fmt.Errorf("%w: %s", connectors.ErrNoConnector, platform)
	}

	account, cfg, err := s.connectorAccount(ctx, acc)
	if err != nil {
		return nil, err
	}
	// The connector outlives the request that happened to build it.
	conn, err := factory(context.WithoutCancel(ctx), cfg, account)
	if err != nil {
		return nil, fmt.Errorf("failed to start %s connector: %w", platform, err)
	}
	s.conns[acc.ID] = conn
	return conn, nil
}

type accountHealth struct {
	s         *Service
	accountID int64
}

func (h *accountHealth) ReportSuccess(ctx context.Context) { h.s.recordOutcome(ctx, h.accountID, nil) }

func (h *accountHealth) ReportRateLimited(ctx context.Context, wait time.Duration) {
	h.s.recordOutcome(ctx, h.accountID, &connectors.FloodWaitError{Wait: wait})
}

func (h *accountHealth) ReportUnauthorized(ctx context.Context) {
	h.s.recordOutcome(ctx, h.accountID, connectors.ErrUnauthorized)
}

func (h *accountHealth) ReportError(ctx context.Context, err error) {
	if err != nil {
		h.s.recordOutcome(ctx, h.accountID, err)
	}
}
