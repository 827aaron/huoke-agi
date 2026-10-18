// Package growth is the lead-generation domain: the source pool (groups and
// channels to read), the lead store with its evidence, and the per-task round
// that decides when enough qualified leads exist to deliver. Agents reach it
// only through the growth_* tools in pkg/tools; platforms only through
// pkg/growth/connectors.
package growth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"
)

const (
	TierQualified = "qualified"
	TierWatchlist = "watchlist"

	QualifiedMinScore = 7
	WatchlistMinScore = 4

	DefaultTarget = 50

	AccountKindAPISession     = "api_session"
	AccountKindBrowserSession = "browser_session"

	AccountStatusActive      = "active"
	AccountStatusRateLimited = "rate_limited"
	AccountStatusNeedsLogin  = "needs_login"
	AccountStatusDisabled    = "disabled"
)

var ErrNoAccount = errors.New("no authorised platform account")

// Scope is where a call comes from. TaskID is nil when an agent runs outside a task
// (the assistant's ad-hoc delegations); such calls read and write the pool and the
// lead store but have no round to count against.
type Scope struct {
	UserID int64
	FlowID int64
	TaskID *int64
}

func (s Scope) flowID() sql.NullInt64 {
	return sql.NullInt64{Int64: s.FlowID, Valid: s.FlowID != 0}
}

func (s Scope) taskID() sql.NullInt64 {
	if s.TaskID == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *s.TaskID, Valid: true}
}

type Service struct {
	db  database.Querier
	cfg *config.Config

	mu    sync.Mutex
	conns map[int64]connectors.Connector
	// knowledge takes the long lists of the default business profile; nil until the router sets it.
	knowledge ProfileKnowledge

	logins loginRegistry

	readMu sync.Mutex
	// lastRead is each account's latest source read: which source, and when it started or is scheduled to.
	lastRead map[int64]sourceRead
}

func NewService(db database.Querier, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg, conns: make(map[int64]connectors.Connector)}
}

var (
	defaultMu      sync.RWMutex
	defaultService *Service
)

// SetDefault installs the process-wide service the agent tools use; nil disables the tools.
func SetDefault(s *Service) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultService = s
}

func Default() *Service {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultService
}

// SetConnector binds a ready connector to an account, bypassing the factory registry.
func (s *Service) SetConnector(accountID int64, conn connectors.Connector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.conns[accountID]; ok && old != conn {
		_ = old.Close()
	}
	s.conns[accountID] = conn
}

// DropConnector closes and forgets the cached connector of an account, so the next
// call builds a fresh one (after a new login, for example).
func (s *Service) DropConnector(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conn, ok := s.conns[accountID]; ok {
		_ = conn.Close()
		delete(s.conns, accountID)
	}
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for id, conn := range s.conns {
		errs = append(errs, conn.Close())
		delete(s.conns, id)
	}
	return errors.Join(errs...)
}

// EnsureAccount creates or refreshes the account row a login attaches its session to.
// An api_session account starts active; a browser_session account starts in needs_login
// until an operator signs in to its profile.
func (s *Service) EnsureAccount(ctx context.Context, userID int64, spec AccountSpec) (database.ChannelAccount, error) {
	if spec.Label == "" {
		spec.Label = "default"
	}
	if spec.Kind == "" {
		spec.Kind = AccountKindAPISession
	}
	if spec.Status == "" {
		spec.Status = AccountStatusActive
		if spec.Kind == AccountKindBrowserSession {
			spec.Status = AccountStatusNeedsLogin
		}
	}
	acc, err := s.db.UpsertChannelAccount(ctx, database.UpsertChannelAccountParams{
		UserID:     userID,
		Platform:   string(spec.Platform),
		Kind:       spec.Kind,
		Label:      spec.Label,
		ExternalID: spec.ExternalID,
		ProfileDir: spec.ProfileDir,
		Status:     spec.Status,
	})
	if err != nil {
		return acc, fmt.Errorf("failed to store %s account: %w", spec.Platform, err)
	}
	s.DropConnector(acc.ID)
	return acc, nil
}

type AccountSpec struct {
	Platform   connectors.Platform
	Kind       string
	Label      string
	ExternalID string
	ProfileDir string
	Status     string
}

// SetAccountStatus records what a connector learnt about its session, needs_login above all.
func (s *Service) SetAccountStatus(ctx context.Context, accountID int64, status string) error {
	if _, err := s.db.UpdateChannelAccountStatus(ctx, database.UpdateChannelAccountStatusParams{
		ID: accountID, Status: status,
	}); err != nil {
		return fmt.Errorf("failed to update account %d: %w", accountID, err)
	}
	if status != AccountStatusActive {
		s.DropConnector(accountID)
	}
	return nil
}

// SessionStore returns the session storage of an account for a login command.
func (s *Service) SessionStore(accountID int64) connectors.SessionStore {
	return &accountSession{db: s.db, accountID: accountID}
}

type accountSession struct {
	db        database.Querier
	accountID int64
}

func (a *accountSession) LoadSession(ctx context.Context) ([]byte, error) {
	data, err := a.db.GetChannelAccountSession(ctx, a.accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session of account %d: %w", a.accountID, err)
	}
	return data, nil
}

func (a *accountSession) StoreSession(ctx context.Context, data []byte) error {
	return a.db.UpdateChannelAccountSession(ctx, database.UpdateChannelAccountSessionParams{
		ID:      a.accountID,
		Session: data,
	})
}

func TierForScore(score int) string {
	switch {
	case score >= QualifiedMinScore:
		return TierQualified
	case score >= WatchlistMinScore:
		return TierWatchlist
	default:
		return ""
	}
}
