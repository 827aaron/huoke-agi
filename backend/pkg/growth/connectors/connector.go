// Package connectors is the platform boundary of lead generation: every platform
// (Telegram first, forums and browser-driven sites later) implements Connector,
// declares what it can do through Capabilities, and registers a Factory.
// Nothing above this package speaks a platform protocol.
package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"pentagi/pkg/config"
)

type Platform string

const (
	PlatformTelegram Platform = "telegram"
	// PlatformWeb is public forum threads and pages. It has no connector: agents read
	// them with the browser tool, so the pool only stores and dedups their links.
	PlatformWeb Platform = "web"
)

type Capability string

const (
	CapSearchSources    Capability = "search_sources"
	CapResolveSource    Capability = "resolve_source"
	CapRecommendSources Capability = "recommend_sources"
	CapJoinSource       Capability = "join_source"
	CapReadHistory      Capability = "read_history"
	CapUserProfile      Capability = "user_profile"
)

type SourceKind string

const (
	SourceKindGroup   SourceKind = "group"
	SourceKindChannel SourceKind = "channel"
	SourceKindForum   SourceKind = "forum"
	SourceKindUnknown SourceKind = "unknown"
)

type JoinStatus string

const (
	JoinStatusNotJoined   JoinStatus = "not_joined"
	JoinStatusPending     JoinStatus = "pending"
	JoinStatusJoined      JoinStatus = "joined"
	JoinStatusNotJoinable JoinStatus = "not_joinable"
)

var (
	// ErrUnsupported is returned by a method whose capability the connector does not declare.
	ErrUnsupported = errors.New("operation is not supported by this platform connector")
	ErrNotFound    = errors.New("source or user not found")
	// ErrUnauthorized means the account session is missing or revoked; a new login is needed.
	// A connector returning it should expect the account to be moved to needs_login.
	ErrUnauthorized = errors.New("platform account is not authorized")
	ErrNoConnector  = errors.New("no connector is registered for this platform")
	// ErrUnreachable means the platform's servers could not be reached, usually a
	// missing or broken proxy.
	ErrUnreachable = errors.New("the platform could not be reached; check the proxy")
)

// FloodWaitError is returned when the platform asked for a pause longer than the
// connector is willing to block the calling agent for. Shorter waits are slept
// inside the connector and never surface.
type FloodWaitError struct {
	Wait time.Duration
}

func (e *FloodWaitError) Error() string {
	return fmt.Sprintf("platform rate limit: retry after %s", e.Wait)
}

// Source is a group, channel or forum as the platform reports it.
// ExternalID is the stable platform id; Peer is opaque connector state valid only
// for the account that produced it (a Telegram access hash) and is stored per account.
type Source struct {
	ExternalID  string
	Username    string
	Title       string
	Link        string
	Kind        SourceKind
	MemberCount int64
	Description string
	Peer        json.RawMessage
}

// SourceRef addresses a source on a call. A connector should prefer Peer, then
// ExternalID, then Username, then Link; an unresolved source carries only Link.
type SourceRef struct {
	ExternalID string
	Username   string
	Link       string
	Peer       json.RawMessage
}

type JoinResult struct {
	Status JoinStatus
	// Reason explains pending and not_joinable outcomes in a few words.
	Reason string
	// Source is the resolved source when joining also resolved it (an invite link), else nil.
	Source *Source
}

// HistoryRequest pages through a source. With both ids zero it returns the newest page.
// OffsetID returns messages strictly older than it; MinID returns messages strictly newer
// than it. With only MinID set the page is the Limit messages just above MinID, not the
// newest ones, so a cursor that advances to the page's newest message skips nothing.
type HistoryRequest struct {
	OffsetID int64
	MinID    int64
	Limit    int
}

type Message struct {
	ID             int64
	SenderID       string
	SenderUsername string
	SenderName     string
	SenderIsBot    bool
	Text           string
	Time           time.Time
	Link           string
	ReplyToID      int64
}

// HistoryPage holds messages newest first. Exhausted is true when an older-direction
// read reached the beginning of the history.
type HistoryPage struct {
	Messages  []Message
	Exhausted bool
}

type UserProfile struct {
	ID          string
	Username    string
	DisplayName string
	Bio         string
	IsBot       bool
}

// Connector is one authorised platform account. Implementations must be safe for
// concurrent use: several agents of one flow may call it at the same time.
type Connector interface {
	Platform() Platform
	Capabilities() []Capability

	SearchSources(ctx context.Context, query string, limit int) ([]Source, error)
	// ResolveSource turns a link, @username or invite link into a source without joining it.
	ResolveSource(ctx context.Context, link string) (Source, error)
	RecommendSources(ctx context.Context, ref SourceRef, limit int) ([]Source, error)
	// JoinSource must be idempotent: joining a source the account is already in reports joined.
	JoinSource(ctx context.Context, ref SourceRef) (JoinResult, error)
	ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error)
	GetUser(ctx context.Context, userID string) (UserProfile, error)

	Close() error
}

func Has(c Connector, capability Capability) bool {
	return slices.Contains(c.Capabilities(), capability)
}

// Joinable reports whether an account must join a source on p before it can read it;
// it agrees with which connectors offer CapJoinSource.
func Joinable(p Platform) bool {
	return p == PlatformTelegram || p == PlatformFacebook
}

// SessionStore persists the account session. Its method set matches gotd's
// session.Storage, so a Telegram connector can use it directly.
type SessionStore interface {
	LoadSession(ctx context.Context) ([]byte, error)
	StoreSession(ctx context.Context, data []byte) error
}

// Account is what a factory gets to build a connector. Kind is "api_session" (Session
// holds the protocol session) or "browser_session" (ProfileDir is a persistent browser
// profile an operator logs in by hand).
type Account struct {
	ID         int64
	UserID     int64
	Platform   Platform
	Kind       string
	ExternalID string
	ProfileDir string
	Session    SessionStore
	Health     HealthReporter

	// Proxy is the route every connection of this account must take, already resolved
	// (the account's own proxy, else the customer's default): http://, https://,
	// socks5:// or socks5h://, optionally with user:pass@. Empty means a direct connection.
	Proxy string
	// Fingerprint is the browser profile the browser service generated for a
	// browser_session account, stored and returned as given; nil when none was set.
	Fingerprint json.RawMessage
	// ExternalBrowserWS is the DevTools endpoint of an external fingerprint browser
	// (AdsPower, BitBrowser) to attach to instead of launching one; empty when unused.
	ExternalBrowserWS string
}

// HealthReporter takes what a connector learns about its account outside a call's
// return value, such as a flood wait it absorbed by sleeping in a retry middleware.
// The service already records the outcome of every call that returns, so a connector
// that only returns errors needs none of it. Methods never block on the platform.
type HealthReporter interface {
	ReportSuccess(ctx context.Context)
	ReportRateLimited(ctx context.Context, wait time.Duration)
	ReportUnauthorized(ctx context.Context)
	ReportError(ctx context.Context, err error)
}

// Factory builds a running connector for one account. It is called once per
// account and the result is cached until Close.
type Factory func(ctx context.Context, cfg *config.Config, account Account) (Connector, error)

var (
	registryMu sync.RWMutex
	registry   = map[Platform]Factory{}
)

// Register is meant to be called from a connector's init; a second registration replaces the first.
func Register(platform Platform, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[platform] = factory
}

func Lookup(platform Platform) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[platform]
	return f, ok
}

func Registered() []Platform {
	registryMu.RLock()
	defer registryMu.RUnlock()
	platforms := make([]Platform, 0, len(registry))
	for p := range registry {
		platforms = append(platforms, p)
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i] < platforms[j] })
	return platforms
}

// CredentialCheck confirms that the app credentials in cfg (TG_API_ID and TG_API_HASH
// for Telegram) are accepted by the platform, without an account session.
type CredentialCheck func(ctx context.Context, cfg *config.Config) error

var credentialChecks = map[Platform]CredentialCheck{}

// RegisterCredentialCheck is called from a connector package's init, beside Register.
func RegisterCredentialCheck(platform Platform, check CredentialCheck) {
	registryMu.Lock()
	defer registryMu.Unlock()
	credentialChecks[platform] = check
}

func LookupCredentialCheck(platform Platform) (CredentialCheck, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	check, ok := credentialChecks[platform]
	return check, ok
}

// ErrPasswordNeeded is returned by PhoneLoginSession.Code when the account has
// two-step verification: the session then waits for Password.
var ErrPasswordNeeded = errors.New("the account has two-step verification: its password is needed")

// ErrBadCode is returned by PhoneLoginSession.Code and Password when the platform
// rejected what the operator typed; the session stays open for another try.
var ErrBadCode = errors.New("the code or password was rejected")

// PhoneLogin starts signing an api_session account in by phone number: it asks the
// platform to send a code and returns the session that takes it. ctx bounds sending
// the code only; the session outlives it and holds whatever connection it needs until
// it succeeds or is cancelled. account.Session is where the signed-in session is stored.
type PhoneLogin func(ctx context.Context, cfg *config.Config, account Account, phone string) (PhoneLoginSession, error)

// PhoneLoginSession is one sign-in in progress. Methods are called one at a time.
// A nil error from Code or Password means the session is stored and the account is
// signed in. The caller calls Cancel when it gives up, and also after success.
type PhoneLoginSession interface {
	Code(ctx context.Context, code string) error
	Password(ctx context.Context, password string) error
	Cancel()
}

// RemoteLoginView is what an operator opens to sign a browser_session account in by
// hand. Fingerprint is the profile the browser runs with, stored on the account; nil
// keeps the account's own.
type RemoteLoginView struct {
	ViewURL     string
	ExpiresAt   time.Time
	Fingerprint json.RawMessage
}

// RemoteLogin opens an account's server-side browser on the platform's login page for
// an operator, who types the credentials and passes any verification; nothing here
// fills a form. Finish reports whether the page is signed in now, with what it still
// shows when it is not; it may be called again. Cancel closes the operator's view.
type RemoteLogin interface {
	Start(ctx context.Context, cfg *config.Config, account Account) (RemoteLoginView, error)
	Finish(ctx context.Context, cfg *config.Config, account Account) (signedIn bool, reason string, err error)
	Cancel(ctx context.Context, cfg *config.Config, account Account) error
}

var (
	phoneLogins  = map[Platform]PhoneLogin{}
	remoteLogins = map[Platform]RemoteLogin{}
)

// RegisterPhoneLogin and RegisterRemoteLogin are called from a connector package's
// init, beside Register.
func RegisterPhoneLogin(platform Platform, login PhoneLogin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	phoneLogins[platform] = login
}

func LookupPhoneLogin(platform Platform) (PhoneLogin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	login, ok := phoneLogins[platform]
	return login, ok
}

func RegisterRemoteLogin(platform Platform, login RemoteLogin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	remoteLogins[platform] = login
}

func LookupRemoteLogin(platform Platform) (RemoteLogin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	login, ok := remoteLogins[platform]
	return login, ok
}
