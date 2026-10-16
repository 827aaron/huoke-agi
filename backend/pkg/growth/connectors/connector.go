// Package connectors reaches the platforms where leads are found. Every
// platform implements Connector; callers branch on Outcome.Status, never on a
// Go error, so a result can be handed to an agent as JSON unchanged.
package connectors

import (
	"context"
	"fmt"
	"time"
)

type Platform string

const (
	PlatformTelegram Platform = "telegram"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusRateLimited Status = "rate_limited"
	StatusNotFound    Status = "not_found"
	// The session is missing or revoked, or Telegram demands verification.
	// The connector stops calling Telegram until a human logs in again.
	StatusNeedsLogin Status = "needs_login"
	// No access: private, banned, or reading requires joining first.
	StatusForbidden   Status = "forbidden"
	StatusInvalid     Status = "invalid_argument"
	StatusUnsupported Status = "unsupported"
	// Transient failure (timeout, network); the same call may succeed later.
	StatusUnavailable Status = "unavailable"
	StatusError       Status = "error"
)

type Membership string

const (
	MembershipJoined          Membership = "joined"
	MembershipPendingApproval Membership = "pending_approval"
	MembershipNotJoined       Membership = "not_joined"
	MembershipNotJoinable     Membership = "not_joinable"
	// The join may or may not have happened (timeout after the request was
	// sent); recheck with MembershipStatus before joining again.
	MembershipUnknown Membership = "unknown"
)

type SourceKind string

const (
	SourceChannel SourceKind = "channel"
	SourceGroup   SourceKind = "group"
	// An invite link whose chat cannot be identified until it is joined or
	// previewed.
	SourceInvite SourceKind = "invite"
)

// Outcome is embedded in every result.
type Outcome struct {
	Status Status `json:"status"`
	// ErrorCode is the platform's own code (e.g. FLOOD_WAIT, CHANNELS_TOO_MUCH)
	// or one of TIMEOUT, CANCELED, NOT_STARTED, NEEDS_LOGIN, JOIN_PACING,
	// INTERNAL. With Status ok it
	// may still explain a not_joinable or unknown membership.
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	// RetryAfter is set with StatusRateLimited: the seconds the platform
	// demands before the same method is called again.
	RetryAfter int `json:"retry_after,omitempty"`
	// Checkpoint is an opaque cursor for SyncSource. The caller persists it per
	// source and passes it back unchanged; empty means "nothing read yet".
	Checkpoint string `json:"checkpoint,omitempty"`
}

func (o Outcome) OK() bool { return o.Status == StatusOK }

func (o Outcome) Err() error {
	if o.OK() {
		return nil
	}
	if o.ErrorCode != "" {
		return fmt.Errorf("%s: %s: %s", o.Status, o.ErrorCode, o.Message)
	}
	return fmt.Errorf("%s: %s", o.Status, o.Message)
}

type Capabilities struct {
	Search          bool `json:"search"`
	Similar         bool `json:"similar"`
	ResolveLink     bool `json:"resolve_link"`
	Join            bool `json:"join"`
	JoinByInvite    bool `json:"join_by_invite"`
	JoinApproval    bool `json:"join_approval"`
	ReadHistory     bool `json:"read_history"`
	IncrementalSync bool `json:"incremental_sync"`
	MembershipCheck bool `json:"membership_check"`
	UserProfile     bool `json:"user_profile"`
	// ReadWithoutJoin: public sources can be read before joining them.
	ReadWithoutJoin bool `json:"read_without_join"`
	// MaxPageSize is the most messages one ReadSource/SyncSource call returns.
	MaxPageSize int `json:"max_page_size"`
}

// SourceRef identifies a source for one account. Ref is opaque and
// account-bound (on Telegram it carries the access hash): store it alongside
// the source but never reuse it with another account.
type SourceRef struct {
	Platform Platform `json:"platform"`
	ID       string   `json:"id"`
	Ref      string   `json:"ref"`
}

type Source struct {
	SourceRef
	Kind       SourceKind `json:"kind"`
	Title      string     `json:"title"`
	Username   string     `json:"username,omitempty"`
	Link       string     `json:"link,omitempty"`
	About      string     `json:"about,omitempty"`
	Members    int        `json:"members,omitempty"`
	Membership Membership `json:"membership"`
	// RequestNeeded: joining sends a request that an admin must approve.
	RequestNeeded bool `json:"request_needed,omitempty"`
	// Broadcast: only admins post, so the authors of messages are not leads.
	Broadcast bool `json:"broadcast,omitempty"`
}

type UserRef struct {
	Platform Platform `json:"platform"`
	ID       string   `json:"id"`
	Ref      string   `json:"ref,omitempty"`
}

type User struct {
	UserRef
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	About     string `json:"about,omitempty"`
	Bot       bool   `json:"bot,omitempty"`
	Link      string `json:"link,omitempty"`
}

type Message struct {
	ID       string    `json:"id"`
	SourceID string    `json:"source_id"`
	Date     time.Time `json:"date"`
	Text     string    `json:"text"`
	// Sender is nil for anonymous admins and channel posts.
	Sender    *UserRef `json:"sender,omitempty"`
	ReplyToID string   `json:"reply_to_id,omitempty"`
	// Link opens the message in a client; empty for basic groups, which have
	// no public message URLs.
	Link string `json:"link,omitempty"`
}

type SearchQuery struct {
	Query string `json:"query,omitempty"`
	// SimilarTo, when set, asks for the platform's recommendations next to this
	// source instead of a text search.
	SimilarTo *SourceRef `json:"similar_to,omitempty"`
	Limit     int        `json:"limit,omitempty"`
}

type ReadRequest struct {
	Source SourceRef `json:"source"`
	// Before pages backwards: messages older than this cursor. Empty starts at
	// the newest message.
	Before string `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type SyncRequest struct {
	Source SourceRef `json:"source"`
	// Checkpoint from the previous ReadSource/SyncSource of this source. Empty
	// behaves like ReadSource from the newest message.
	Checkpoint string `json:"checkpoint,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type HealthResult struct {
	Outcome
	Account *User `json:"account,omitempty"`
}

type SearchResult struct {
	Outcome
	Sources []Source `json:"sources"`
}

type SourceResult struct {
	Outcome
	Source *Source `json:"source,omitempty"`
}

type MembershipResult struct {
	Outcome
	Membership Membership `json:"membership"`
	// Source is filled when the call learnt the chat's identity, e.g. after an
	// invite link was accepted.
	Source *Source `json:"source,omitempty"`
}

type ReadResult struct {
	Outcome
	Messages []Message `json:"messages"`
	// Users holds the senders of Messages, keyed by UserRef.ID.
	Users map[string]User `json:"users"`
	// Before continues paging backwards; empty when the start of history was
	// reached. Only ReadSource sets it.
	Before string `json:"before,omitempty"`
	// HasMore: SyncSource stopped at Limit and newer messages remain.
	HasMore bool `json:"has_more,omitempty"`
}

type UserResult struct {
	Outcome
	User *User `json:"user,omitempty"`
}

// AccountEvent reports the outcome of one platform request so the caller can
// keep the account's health record (last success, rate limit, login state).
type AccountEvent struct {
	Platform Platform  `json:"platform"`
	Method   string    `json:"method"`
	At       time.Time `json:"at"`
	Outcome
	// RateLimitUntil is At plus RetryAfter when Status is rate_limited.
	RateLimitUntil time.Time `json:"rate_limit_until,omitzero"`
}

// AccountObserver is called synchronously after every request, including one
// refused locally by a cooldown or a stopped session; it must not block.
type AccountObserver func(AccountEvent)

// Connector is safe for concurrent use. Calls block while the platform demands
// a short wait; a longer one comes back as StatusRateLimited with RetryAfter.
type Connector interface {
	Platform() Platform
	Capabilities() Capabilities
	HealthCheck(ctx context.Context) HealthResult
	SearchSources(ctx context.Context, q SearchQuery) SearchResult
	// ResolveSource accepts a link, @username or bare username. An invite link
	// is checked without joining.
	ResolveSource(ctx context.Context, link string) SourceResult
	MembershipStatus(ctx context.Context, src SourceRef) MembershipResult
	// JoinSource is idempotent: an already joined source returns joined.
	JoinSource(ctx context.Context, src SourceRef) MembershipResult
	// ReadSource returns messages newest first and a Checkpoint covering the
	// newest message seen.
	ReadSource(ctx context.Context, req ReadRequest) ReadResult
	// SyncSource returns messages newer than the checkpoint, oldest first.
	SyncSource(ctx context.Context, req SyncRequest) ReadResult
	GetUser(ctx context.Context, user UserRef) UserResult
}
