package connectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type TelegramConfig struct {
	APIID   int    `env:"TG_API_ID"`
	APIHash string `env:"TG_API_HASH"`
	// SessionPath is the session file of the logged-in account. It holds the
	// account's auth key, so it lives outside every container and is never
	// shown to an agent.
	SessionPath string `env:"TG_SESSION_PATH" envDefault:"./data/telegram/session.json"`
	TestServer  bool   `env:"TG_TEST_SERVER" envDefault:"false"`
	// Proxy is the account's proxy URL; see Account.Proxy.
	Proxy string `env:"TG_PROXY"`
	// A FLOOD_WAIT up to this long is slept through inside the call; a longer
	// one is returned as StatusRateLimited so the caller is not blocked.
	MaxInlineFloodWait time.Duration `env:"TG_MAX_INLINE_FLOOD_WAIT" envDefault:"60s"`
	CallTimeout        time.Duration `env:"TG_CALL_TIMEOUT" envDefault:"30s"`

	// Anti-spam pacing defaults; zero turns each off. The gap between two
	// requests is random within [MinCallInterval, MaxCallInterval].
	MinCallInterval time.Duration `env:"TG_MIN_CALL_INTERVAL" envDefault:"1s"`
	MaxCallInterval time.Duration `env:"TG_MAX_CALL_INTERVAL" envDefault:"3s"`
	MaxConcurrency  int           `env:"TG_MAX_CONCURRENCY" envDefault:"1"`
	// Joins are spaced JoinWarmupInterval apart for the account's first
	// JoinWarmupCount joins, then JoinInterval, each plus up to 50% jitter.
	JoinInterval       time.Duration `env:"TG_JOIN_INTERVAL" envDefault:"90s"`
	JoinWarmupInterval time.Duration `env:"TG_JOIN_WARMUP_INTERVAL" envDefault:"5m"`
	JoinWarmupCount    int           `env:"TG_JOIN_WARMUP_COUNT" envDefault:"10"`
}

func TelegramConfigFromEnv() (TelegramConfig, error) {
	var cfg TelegramConfig
	if err := env.Parse(&cfg); err != nil {
		return TelegramConfig{}, fmt.Errorf("parse telegram config: %w", err)
	}
	return cfg, nil
}

func (c TelegramConfig) Validate() error {
	switch {
	case c.APIID <= 0:
		return errors.New("TG_API_ID is not set")
	case c.APIHash == "":
		return errors.New("TG_API_HASH is not set")
	case c.SessionPath == "":
		return errors.New("TG_SESSION_PATH is not set")
	}
	_, err := proxyDialer(c.Proxy)
	return err
}

// telegramAPI is the part of *tg.Client the connector calls; tests replace it.
type telegramAPI interface {
	UsersGetUsers(ctx context.Context, id []tg.InputUserClass) ([]tg.UserClass, error)
	UsersGetFullUser(ctx context.Context, id tg.InputUserClass) (*tg.UsersUserFull, error)
	ContactsSearch(ctx context.Context, request *tg.ContactsSearchRequest) (*tg.ContactsFound, error)
	ContactsResolveUsername(ctx context.Context, request *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	ChannelsGetChannelRecommendations(ctx context.Context, request *tg.ChannelsGetChannelRecommendationsRequest) (tg.MessagesChatsClass, error)
	ChannelsJoinChannel(ctx context.Context, channel tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error)
	ChannelsGetParticipant(ctx context.Context, request *tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error)
	MessagesCheckChatInvite(ctx context.Context, hash string) (tg.ChatInviteClass, error)
	MessagesImportChatInvite(ctx context.Context, hash string) (tg.MessagesChatInviteJoinResultClass, error)
	MessagesGetHistory(ctx context.Context, request *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error)
	MessagesGetChats(ctx context.Context, id []int64) (tg.MessagesChatsClass, error)
}

const (
	maxFloodRetries    = 3
	defaultSearchLimit = 20
	maxSearchLimit     = 100
	defaultPageSize    = 100
	maxPageSize        = 100
)

var errNotStarted = errors.New("telegram client is not running")

type Telegram struct {
	cfg    TelegramConfig
	client *telegram.Client
	pacer  *pacer

	mu       sync.RWMutex
	observer AccountObserver
	api      telegramAPI
	stop     context.CancelFunc
	done     chan struct{}
}

var _ Connector = (*Telegram)(nil)

func NewTelegram(cfg TelegramConfig) (*Telegram, error) {
	client, err := newTelegramClient(cfg)
	if err != nil {
		return nil, err
	}
	t := newTelegramWithAPI(cfg, nil)
	t.client = client
	return t, nil
}

func newTelegramWithAPI(cfg TelegramConfig, api telegramAPI) *Telegram {
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 30 * time.Second
	}
	return &Telegram{cfg: cfg, api: api, pacer: newPacer(cfg, sleepCtx)}
}

func newTelegramClient(cfg TelegramConfig) (*telegram.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SessionPath), 0o700); err != nil {
		return nil, fmt.Errorf("create telegram session directory: %w", err)
	}
	opts := telegram.Options{SessionStorage: &session.FileStorage{Path: cfg.SessionPath}}
	if cfg.TestServer {
		opts.DCList = dcs.Test()
	}
	if cfg.Proxy != "" {
		dial, _ := proxyDialer(cfg.Proxy)
		opts.Resolver = dcs.Plain(dcs.PlainOptions{Dial: dial})
	}
	return telegram.NewClient(cfg.APIID, cfg.APIHash, opts), nil
}

// Start connects and returns once the connection is up. It does not fail on a
// missing login: HealthCheck reports unauthorized until Login has been run.
// The connection lives until ctx is done or Close is called.
func (t *Telegram) Start(ctx context.Context) error {
	if t.client == nil {
		return errors.New("telegram connector has no client")
	}
	runCtx, cancel := context.WithCancel(ctx)
	ready := make(chan struct{})
	done := make(chan struct{})
	runErr := make(chan error, 1)
	go func() {
		defer close(done)
		runErr <- t.client.Run(runCtx, func(ctx context.Context) error {
			t.mu.Lock()
			t.api = t.client.API()
			t.mu.Unlock()
			close(ready)
			<-ctx.Done()
			return nil
		})
		t.mu.Lock()
		t.api = nil
		t.mu.Unlock()
	}()

	select {
	case <-ready:
		t.pacer.resetLogin()
		t.mu.Lock()
		t.stop, t.done = cancel, done
		t.mu.Unlock()
		return nil
	case err := <-runErr:
		cancel()
		return fmt.Errorf("connect to telegram: %w", err)
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	}
}

func (t *Telegram) SetObserver(o AccountObserver) {
	t.mu.Lock()
	t.observer = o
	t.mu.Unlock()
}

func (t *Telegram) report(method string, err error) {
	t.mu.RLock()
	o := t.observer
	t.mu.RUnlock()
	if o == nil || errors.Is(err, errNotStarted) {
		return
	}
	ev := AccountEvent{Platform: PlatformTelegram, Method: method, At: t.pacer.now(), Outcome: outcomeOf(err)}
	if ev.Status == StatusRateLimited && ev.RetryAfter > 0 {
		ev.RateLimitUntil = ev.At.Add(time.Duration(ev.RetryAfter) * time.Second)
	}
	o(ev)
}

func (t *Telegram) Close() {
	t.mu.Lock()
	stop, done := t.stop, t.done
	t.stop, t.done = nil, nil
	t.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}

// Login runs the interactive phone, code and 2FA flow once and leaves the
// session in cfg.SessionPath for every later Start.
func Login(ctx context.Context, cfg TelegramConfig, user auth.UserAuthenticator) error {
	client, err := newTelegramClient(cfg)
	if err != nil {
		return err
	}
	return client.Run(ctx, func(ctx context.Context) error {
		return auth.NewFlow(user, auth.SendCodeOptions{}).Run(ctx, client.Auth())
	})
}

func (t *Telegram) Platform() Platform { return PlatformTelegram }

func (t *Telegram) Capabilities() Capabilities {
	return Capabilities{
		Search:          true,
		Similar:         true,
		ResolveLink:     true,
		Join:            true,
		JoinByInvite:    true,
		JoinApproval:    true,
		ReadHistory:     true,
		IncrementalSync: true,
		MembershipCheck: true,
		UserProfile:     true,
		ReadWithoutJoin: true,
		MaxPageSize:     maxPageSize,
	}
}

func (t *Telegram) HealthCheck(ctx context.Context) HealthResult {
	var users []tg.UserClass
	err := t.call(ctx, "users.getUsers", func(ctx context.Context, api telegramAPI) (err error) {
		users, err = api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
		return err
	})
	if err != nil {
		return HealthResult{Outcome: outcomeOf(err)}
	}
	for _, u := range users {
		if self, ok := u.(*tg.User); ok {
			acc := userFromTG(self)
			return HealthResult{Outcome: okOutcome(), Account: &acc}
		}
	}
	return HealthResult{Outcome: Outcome{Status: StatusNeedsLogin, Message: "no account behind the session"}}
}

func (t *Telegram) SearchSources(ctx context.Context, q SearchQuery) SearchResult {
	limit := clamp(q.Limit, defaultSearchLimit, maxSearchLimit)
	var chats []tg.ChatClass
	switch {
	case q.SimilarTo != nil:
		ref, err := decodeRef(q.SimilarTo.Ref)
		if err != nil {
			return SearchResult{Outcome: invalid(err.Error())}
		}
		channel, ok := ref.inputChannel()
		if !ok {
			return SearchResult{Outcome: invalid("recommendations exist only for channels and supergroups")}
		}
		err = t.call(ctx, "channels.getChannelRecommendations", func(ctx context.Context, api telegramAPI) error {
			res, err := api.ChannelsGetChannelRecommendations(ctx, &tg.ChannelsGetChannelRecommendationsRequest{Channel: channel})
			if err == nil {
				chats = res.GetChats()
			}
			return err
		})
		if err != nil {
			return SearchResult{Outcome: outcomeOf(err)}
		}
	case q.Query != "":
		err := t.call(ctx, "contacts.search", func(ctx context.Context, api telegramAPI) error {
			res, err := api.ContactsSearch(ctx, &tg.ContactsSearchRequest{Q: q.Query, Limit: limit})
			if err == nil {
				chats = res.Chats
			}
			return err
		})
		if err != nil {
			return SearchResult{Outcome: outcomeOf(err)}
		}
	default:
		return SearchResult{Outcome: invalid("query or similar_to is required")}
	}

	sources := make([]Source, 0, len(chats))
	for _, c := range chats {
		if src, ok := sourceFromChat(c); ok && src.Membership != MembershipNotJoinable {
			sources = append(sources, src)
		}
		if len(sources) == limit {
			break
		}
	}
	return SearchResult{Outcome: okOutcome(), Sources: sources}
}

func (t *Telegram) ResolveSource(ctx context.Context, link string) SourceResult {
	l, err := parseTelegramLink(link)
	if err != nil {
		return SourceResult{Outcome: invalid(err.Error())}
	}
	if l.Invite != "" {
		return t.resolveInvite(ctx, l.Invite)
	}

	var res *tg.ContactsResolvedPeer
	err = t.call(ctx, "contacts.resolveUsername", func(ctx context.Context, api telegramAPI) (err error) {
		res, err = api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: l.Username})
		return err
	})
	if err != nil {
		return SourceResult{Outcome: outcomeOf(err)}
	}
	var id int64
	switch p := res.Peer.(type) {
	case *tg.PeerChannel:
		id = p.ChannelID
	case *tg.PeerChat:
		id = p.ChatID
	default:
		return SourceResult{Outcome: Outcome{Status: StatusInvalid, ErrorCode: "NOT_A_CHAT", Message: "@" + l.Username + " is a user, not a group or channel"}}
	}
	for _, c := range res.Chats {
		if c.GetID() == id {
			if src, ok := sourceFromChat(c); ok {
				return SourceResult{Outcome: okOutcome(), Source: &src}
			}
		}
	}
	return SourceResult{Outcome: Outcome{Status: StatusNotFound, Message: "resolved peer missing from response"}}
}

func (t *Telegram) resolveInvite(ctx context.Context, hash string) SourceResult {
	var inv tg.ChatInviteClass
	err := t.call(ctx, "messages.checkChatInvite", func(ctx context.Context, api telegramAPI) (err error) {
		inv, err = api.MessagesCheckChatInvite(ctx, hash)
		return err
	})
	if err != nil {
		return SourceResult{Outcome: outcomeOf(err)}
	}
	src, ok := sourceFromInvite(hash, inv)
	if !ok {
		return SourceResult{Outcome: Outcome{Status: StatusError, ErrorCode: "INTERNAL", Message: fmt.Sprintf("unexpected invite type %T", inv)}}
	}
	return SourceResult{Outcome: okOutcome(), Source: &src}
}

func (t *Telegram) MembershipStatus(ctx context.Context, src SourceRef) MembershipResult {
	ref, err := decodeRef(src.Ref)
	if err != nil {
		return MembershipResult{Outcome: invalid(err.Error())}
	}
	switch ref.Kind {
	case refInvite:
		return t.inviteMembership(ctx, ref.Invite)
	case refChannel:
		channel, _ := ref.inputChannel()
		var res *tg.ChannelsChannelParticipant
		err := t.call(ctx, "channels.getParticipant", func(ctx context.Context, api telegramAPI) (err error) {
			res, err = api.ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{Channel: channel, Participant: &tg.InputPeerSelf{}})
			return err
		})
		switch {
		case err == nil:
			switch res.Participant.(type) {
			case *tg.ChannelParticipantBanned:
				return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: "USER_BANNED_IN_CHANNEL"}, Membership: MembershipNotJoinable}
			case *tg.ChannelParticipantLeft:
				return MembershipResult{Outcome: okOutcome(), Membership: MembershipNotJoined}
			}
			return MembershipResult{Outcome: okOutcome(), Membership: MembershipJoined}
		case tgerr.Is(err, "USER_NOT_PARTICIPANT"):
			return MembershipResult{Outcome: okOutcome(), Membership: MembershipNotJoined}
		case tgerr.Is(err, "CHANNEL_PRIVATE") && ref.Invite != "":
			return t.inviteMembership(ctx, ref.Invite)
		case tgerr.Is(err, "CHANNEL_PRIVATE", "CHANNEL_INVALID", "USER_BANNED_IN_CHANNEL"):
			return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: rpcType(err)}, Membership: MembershipNotJoinable}
		}
		return MembershipResult{Outcome: outcomeOf(err), Membership: MembershipUnknown}
	case refChat:
		var res tg.MessagesChatsClass
		err := t.call(ctx, "messages.getChats", func(ctx context.Context, api telegramAPI) (err error) {
			res, err = api.MessagesGetChats(ctx, []int64{ref.ID})
			return err
		})
		if err != nil {
			if tgerr.Is(err, "CHAT_ID_INVALID", "PEER_ID_INVALID") {
				return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: rpcType(err)}, Membership: MembershipNotJoined}
			}
			return MembershipResult{Outcome: outcomeOf(err), Membership: MembershipUnknown}
		}
		for _, c := range res.GetChats() {
			if c.GetID() == ref.ID {
				s, _ := sourceFromChat(c)
				return MembershipResult{Outcome: okOutcome(), Membership: s.Membership, Source: &s}
			}
		}
		return MembershipResult{Outcome: okOutcome(), Membership: MembershipNotJoined}
	}
	return MembershipResult{Outcome: invalid("a user is not a source")}
}

func (t *Telegram) inviteMembership(ctx context.Context, hash string) MembershipResult {
	res := t.resolveInvite(ctx, hash)
	if !res.OK() {
		if isInviteDead(res.ErrorCode) {
			return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: res.ErrorCode}, Membership: MembershipNotJoinable}
		}
		return MembershipResult{Outcome: res.Outcome, Membership: MembershipUnknown}
	}
	return MembershipResult{Outcome: okOutcome(), Membership: res.Source.Membership, Source: res.Source}
}

func isInviteDead(code string) bool {
	return code == "INVITE_HASH_EXPIRED" || code == "INVITE_HASH_INVALID" || code == "INVITE_HASH_EMPTY"
}

// JoinSource joins by invite when the ref carries one, since a private chat
// cannot be joined by id. A join that times out is checked before it is
// retried once, so a join that did land is not sent twice.
func (t *Telegram) JoinSource(ctx context.Context, src SourceRef) MembershipResult {
	ref, err := decodeRef(src.Ref)
	if err != nil {
		return MembershipResult{Outcome: invalid(err.Error())}
	}
	if ref.Kind == refUser {
		return MembershipResult{Outcome: invalid("a user is not a source")}
	}
	if ref.Kind == refChat && ref.Invite == "" {
		st := t.MembershipStatus(ctx, src)
		if st.OK() && st.Membership != MembershipJoined {
			st.Membership, st.ErrorCode = MembershipNotJoinable, "INVITE_REQUIRED"
		}
		return st
	}

	res := t.joinOnce(ctx, ref)
	if res.Status != StatusUnavailable {
		return res
	}
	check := t.MembershipStatus(ctx, src)
	switch {
	case check.OK() && check.Membership == MembershipNotJoined:
		res = t.joinOnce(ctx, ref)
		if res.Status == StatusUnavailable {
			res.Membership = MembershipUnknown
		}
		return res
	case check.OK():
		return check
	}
	return MembershipResult{Outcome: res.Outcome, Membership: MembershipUnknown}
}

func (t *Telegram) joinOnce(ctx context.Context, ref tgRef) MembershipResult {
	if err := t.pacer.beforeJoin(ctx); err != nil {
		return MembershipResult{Outcome: outcomeOf(err), Membership: MembershipNotJoined}
	}
	method := "channels.joinChannel"
	if ref.Invite != "" {
		method = "messages.importChatInvite"
	}
	var joined tg.MessagesChatInviteJoinResultClass
	err := t.call(ctx, method, func(ctx context.Context, api telegramAPI) (err error) {
		if ref.Invite != "" {
			joined, err = api.MessagesImportChatInvite(ctx, ref.Invite)
		} else {
			channel, _ := ref.inputChannel()
			joined, err = api.ChannelsJoinChannel(ctx, channel)
		}
		return err
	})

	if err == nil || tgerr.Is(err, "INVITE_REQUEST_SENT") ||
		(outcomeOf(err).Status == StatusUnavailable && !errors.Is(err, errNotStarted)) {
		t.pacer.joined()
	}

	switch {
	case err == nil:
	case tgerr.Is(err, "INVITE_REQUEST_SENT"):
		return MembershipResult{Outcome: okOutcome(), Membership: MembershipPendingApproval}
	case tgerr.Is(err, "USER_ALREADY_PARTICIPANT"):
		return MembershipResult{Outcome: okOutcome(), Membership: MembershipJoined}
	case tgerr.Is(err, "INVITE_HASH_EXPIRED", "INVITE_HASH_INVALID", "INVITE_HASH_EMPTY",
		"CHANNEL_PRIVATE", "CHANNEL_INVALID", "CHAT_INVALID", "USER_BANNED_IN_CHANNEL", "CHANNEL_PUBLIC_GROUP_NA"):
		return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: rpcType(err)}, Membership: MembershipNotJoinable}
	default:
		// CHANNELS_TOO_MUCH and flood waits concern the account, not the source.
		o := outcomeOf(err)
		m := MembershipNotJoined
		if o.Status == StatusUnavailable {
			m = MembershipUnknown
		}
		return MembershipResult{Outcome: o, Membership: m}
	}

	switch r := joined.(type) {
	case *tg.MessagesChatInviteJoinResultOk:
		res := MembershipResult{Outcome: okOutcome(), Membership: MembershipJoined}
		if u, ok := r.Updates.(interface{ GetChats() []tg.ChatClass }); ok {
			for _, c := range u.GetChats() {
				if s, ok := sourceFromChat(c); ok && (ref.ID == 0 || c.GetID() == ref.ID) {
					if ref.Invite != "" {
						s.Ref = withInvite(s.Ref, ref.Invite)
					}
					s.Membership = MembershipJoined
					res.Source = &s
					break
				}
			}
		}
		return res
	case *tg.MessagesChatInviteJoinResultWebView:
		return MembershipResult{Outcome: Outcome{Status: StatusOK, ErrorCode: "JOIN_WEBVIEW_REQUIRED", Message: "the chat requires completing a bot web app before joining"}, Membership: MembershipUnknown}
	}
	return MembershipResult{Outcome: okOutcome(), Membership: MembershipJoined}
}

func withInvite(encoded, invite string) string {
	r, err := decodeRef(encoded)
	if err != nil {
		return encoded
	}
	r.Invite = invite
	return r.encode()
}

func (t *Telegram) ReadSource(ctx context.Context, req ReadRequest) ReadResult {
	ref, peer, o := readablePeer(req.Source)
	if !o.OK() {
		return ReadResult{Outcome: o}
	}
	before, err := parseOptionalInt(req.Before)
	if err != nil || before < 0 {
		return ReadResult{Outcome: invalid("before must be a message id")}
	}
	limit := clamp(req.Limit, defaultPageSize, maxPageSize)

	raw, msgs, users, err := t.history(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: int(before), Limit: limit})
	if err != nil {
		return ReadResult{Outcome: outcomeOf(err)}
	}
	res := t.buildRead(ref, req.Source, msgs, users)
	if len(raw) == limit {
		res.Before = strconv.Itoa(minID(raw))
	}
	if before == 0 {
		res.Checkpoint = strconv.Itoa(maxID(raw))
	}
	return res
}

func (t *Telegram) SyncSource(ctx context.Context, req SyncRequest) ReadResult {
	cp, err := parseOptionalInt(req.Checkpoint)
	if err != nil || cp < 0 {
		return ReadResult{Outcome: invalid("checkpoint is malformed")}
	}
	limit := clamp(req.Limit, defaultPageSize, maxPageSize)
	ref, peer, o := readablePeer(req.Source)
	if !o.OK() {
		return ReadResult{Outcome: o}
	}

	hreq := &tg.MessagesGetHistoryRequest{Peer: peer, Limit: limit}
	if cp > 0 {
		// The page of `limit` messages that starts just above the checkpoint.
		hreq.OffsetID, hreq.AddOffset, hreq.MinID = int(cp)+1, -limit, int(cp)
	}
	raw, msgs, users, err := t.history(ctx, hreq)
	if err != nil {
		return ReadResult{Outcome: outcomeOf(err)}
	}
	slices.Reverse(msgs)
	res := t.buildRead(ref, req.Source, msgs, users)
	res.HasMore = cp > 0 && len(raw) == limit
	res.Checkpoint = strconv.Itoa(max(int(cp), maxID(raw)))
	return res
}

func readablePeer(src SourceRef) (tgRef, tg.InputPeerClass, Outcome) {
	ref, err := decodeRef(src.Ref)
	if err != nil {
		return tgRef{}, nil, invalid(err.Error())
	}
	if ref.Kind == refInvite {
		return tgRef{}, nil, Outcome{Status: StatusForbidden, ErrorCode: "JOIN_REQUIRED", Message: "an invite-only source can be read after it is joined"}
	}
	if ref.Kind == refUser {
		return tgRef{}, nil, invalid("a user is not a source")
	}
	peer, _ := ref.inputPeer()
	return ref, peer, okOutcome()
}

func (t *Telegram) history(ctx context.Context, req *tg.MessagesGetHistoryRequest) (raw []tg.MessageClass, msgs []*tg.Message, users []tg.UserClass, err error) {
	err = t.call(ctx, "messages.getHistory", func(ctx context.Context, api telegramAPI) error {
		res, err := api.MessagesGetHistory(ctx, req)
		if err != nil {
			return err
		}
		if m, ok := res.AsModified(); ok {
			raw, users = m.GetMessages(), m.GetUsers()
		}
		return nil
	})
	for _, m := range raw {
		if msg, ok := m.(*tg.Message); ok {
			msgs = append(msgs, msg)
		}
	}
	return raw, msgs, users, err
}

func (t *Telegram) buildRead(ref tgRef, src SourceRef, msgs []*tg.Message, users []tg.UserClass) ReadResult {
	byID := make(map[int64]*tg.User, len(users))
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			byID[user.ID] = user
		}
	}
	res := ReadResult{Outcome: okOutcome(), Messages: make([]Message, 0, len(msgs)), Users: map[string]User{}}
	for _, m := range msgs {
		msg := Message{
			ID:       strconv.Itoa(m.ID),
			SourceID: strconv.FormatInt(ref.ID, 10),
			Date:     time.Unix(int64(m.Date), 0).UTC(),
			Text:     m.Message,
			Link:     messageLink(ref, m.ID),
		}
		if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok && h.ReplyToMsgID != 0 {
			msg.ReplyToID = strconv.Itoa(h.ReplyToMsgID)
		}
		if from, ok := m.FromID.(*tg.PeerUser); ok {
			key := strconv.FormatInt(from.UserID, 10)
			u, known := res.Users[key]
			if !known {
				if tu := byID[from.UserID]; tu != nil {
					u = userFromTG(tu)
					if tu.Min {
						u.Ref = minUserRef(tu.ID, ref, m.ID)
					}
				} else {
					u = User{UserRef: UserRef{Platform: PlatformTelegram, ID: key, Ref: minUserRef(from.UserID, ref, m.ID)}}
				}
				res.Users[key] = u
			}
			sender := u.UserRef
			msg.Sender = &sender
		}
		res.Messages = append(res.Messages, msg)
	}
	return res
}

func minUserRef(userID int64, source tgRef, msgID int) string {
	from := tgRef{Kind: source.Kind, ID: source.ID, Hash: source.Hash}
	return tgRef{Kind: refUser, ID: userID, FromPeer: &from, FromMsg: msgID}.encode()
}

func messageLink(ref tgRef, id int) string {
	switch {
	case ref.Kind != refChannel:
		return ""
	case ref.Username != "":
		return fmt.Sprintf("https://t.me/%s/%d", ref.Username, id)
	}
	return fmt.Sprintf("https://t.me/c/%d/%d", ref.ID, id)
}

func (t *Telegram) GetUser(ctx context.Context, user UserRef) UserResult {
	ref, err := decodeRef(user.Ref)
	if err != nil {
		return UserResult{Outcome: invalid(err.Error())}
	}
	input, ok := ref.inputUser()
	if !ok {
		return UserResult{Outcome: invalid("not a user ref")}
	}
	var full *tg.UsersUserFull
	err = t.call(ctx, "users.getFullUser", func(ctx context.Context, api telegramAPI) (err error) {
		full, err = api.UsersGetFullUser(ctx, input)
		return err
	})
	if err != nil {
		return UserResult{Outcome: outcomeOf(err)}
	}
	for _, u := range full.Users {
		if tu, ok := u.(*tg.User); ok && tu.ID == ref.ID {
			res := userFromTG(tu)
			if tu.Min || tu.AccessHash == 0 {
				res.Ref = user.Ref
			}
			res.About = full.FullUser.About
			return UserResult{Outcome: okOutcome(), User: &res}
		}
	}
	return UserResult{Outcome: Outcome{Status: StatusNotFound, Message: "user missing from response"}}
}

// call runs fn against the live API, sleeping through flood waits no longer
// than MaxInlineFloodWait. Telegram counts a flood wait per method, so the
// retry repeats only the call that was refused.
func (t *Telegram) call(ctx context.Context, method string, fn func(ctx context.Context, api telegramAPI) error) (err error) {
	defer func() { t.report(method, err) }()
	t.mu.RLock()
	api := t.api
	t.mu.RUnlock()
	if api == nil {
		return errNotStarted
	}
	release, err := t.pacer.acquire(ctx, method)
	if err != nil {
		return err
	}
	defer release()
	for attempt := 0; ; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, t.cfg.CallTimeout)
		err := fn(cctx, api)
		cancel()
		wait, flood := tgerr.AsFloodWait(err)
		if !flood || wait > t.cfg.MaxInlineFloodWait || attempt == maxFloodRetries {
			t.pacer.observe(method, err)
			return err
		}
		if err := t.pacer.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var (
	needsLoginTypes = []string{
		"AUTH_KEY_UNREGISTERED", "AUTH_KEY_INVALID", "SESSION_REVOKED", "SESSION_EXPIRED",
		"USER_DEACTIVATED", "USER_DEACTIVATED_BAN", "AUTH_KEY_DUPLICATED",
	}
	notFoundTypes = []string{
		"USERNAME_NOT_OCCUPIED", "USERNAME_INVALID", "CHANNEL_INVALID", "CHAT_ID_INVALID",
		"PEER_ID_INVALID", "USER_ID_INVALID", "INVITE_HASH_INVALID", "INVITE_HASH_EXPIRED",
		"INVITE_HASH_EMPTY", "MSG_ID_INVALID",
	}
	forbiddenTypes = []string{
		"CHANNEL_PRIVATE", "CHAT_FORBIDDEN", "USER_BANNED_IN_CHANNEL", "CHAT_ADMIN_REQUIRED",
	}
)

func outcomeOf(err error) Outcome {
	if err == nil {
		return okOutcome()
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return Outcome{Status: StatusRateLimited, ErrorCode: tgerr.ErrFloodWait, RetryAfter: int(wait / time.Second), Message: err.Error()}
	}
	var paced *joinPacingError
	switch {
	case errors.As(err, &paced):
		return Outcome{Status: StatusRateLimited, ErrorCode: "JOIN_PACING", RetryAfter: int((paced.wait + time.Second - 1) / time.Second), Message: err.Error()}
	case errors.Is(err, errNeedsLogin):
		return Outcome{Status: StatusNeedsLogin, ErrorCode: "NEEDS_LOGIN", Message: err.Error()}
	case errors.Is(err, errNotStarted):
		return Outcome{Status: StatusUnavailable, ErrorCode: "NOT_STARTED", Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return Outcome{Status: StatusUnavailable, ErrorCode: "TIMEOUT", Message: err.Error()}
	case errors.Is(err, context.Canceled):
		return Outcome{Status: StatusUnavailable, ErrorCode: "CANCELED", Message: err.Error()}
	}
	rpc, ok := tgerr.As(err)
	if !ok {
		return Outcome{Status: StatusError, ErrorCode: "INTERNAL", Message: err.Error()}
	}
	o := Outcome{ErrorCode: rpc.Type, Message: rpc.Error()}
	switch {
	case rpc.IsOneOf(needsLoginTypes...) || rpc.Code == 401:
		o.Status = StatusNeedsLogin
	case rpc.IsOneOf(notFoundTypes...):
		o.Status = StatusNotFound
	case rpc.IsOneOf(forbiddenTypes...) || rpc.Code == 403:
		o.Status = StatusForbidden
	case rpc.Code == 420:
		o.Status = StatusRateLimited
		o.RetryAfter = rpc.Argument
	case rpc.Code >= 500:
		o.Status = StatusUnavailable
	case rpc.Code == 400:
		o.Status = StatusInvalid
	default:
		o.Status = StatusError
	}
	return o
}

func rpcType(err error) string {
	if rpc, ok := tgerr.As(err); ok {
		return rpc.Type
	}
	return ""
}

func okOutcome() Outcome { return Outcome{Status: StatusOK} }

func invalid(msg string) Outcome { return Outcome{Status: StatusInvalid, Message: msg} }

func clamp(n, def, hi int) int {
	switch {
	case n <= 0:
		return def
	case n > hi:
		return hi
	}
	return n
}

func minID(msgs []tg.MessageClass) int {
	lo := 0
	for _, m := range msgs {
		if id := m.GetID(); lo == 0 || id < lo {
			lo = id
		}
	}
	return lo
}

func maxID(msgs []tg.MessageClass) int {
	hi := 0
	for _, m := range msgs {
		hi = max(hi, m.GetID())
	}
	return hi
}

func sourceFromChat(c tg.ChatClass) (Source, bool) {
	switch ch := c.(type) {
	case *tg.Channel:
		username := ch.Username
		if username == "" {
			for _, u := range ch.Usernames {
				if u.Active {
					username = u.Username
					break
				}
			}
		}
		s := Source{
			SourceRef: SourceRef{
				Platform: PlatformTelegram,
				ID:       strconv.FormatInt(ch.ID, 10),
				Ref:      tgRef{Kind: refChannel, ID: ch.ID, Hash: ch.AccessHash, Username: username}.encode(),
			},
			Kind:          SourceChannel,
			Title:         ch.Title,
			Username:      username,
			Members:       ch.ParticipantsCount,
			Membership:    MembershipJoined,
			RequestNeeded: ch.JoinRequest,
			Broadcast:     ch.Broadcast,
		}
		if !ch.Broadcast {
			s.Kind = SourceGroup
		}
		if username != "" {
			s.Link = "https://t.me/" + username
		}
		if ch.Left {
			s.Membership = MembershipNotJoined
		}
		return s, true
	case *tg.ChannelForbidden:
		return Source{
			SourceRef:  SourceRef{Platform: PlatformTelegram, ID: strconv.FormatInt(ch.ID, 10), Ref: tgRef{Kind: refChannel, ID: ch.ID, Hash: ch.AccessHash}.encode()},
			Kind:       SourceGroup,
			Title:      ch.Title,
			Membership: MembershipNotJoinable,
			Broadcast:  ch.Broadcast,
		}, true
	case *tg.Chat:
		s := Source{
			SourceRef:  SourceRef{Platform: PlatformTelegram, ID: strconv.FormatInt(ch.ID, 10), Ref: tgRef{Kind: refChat, ID: ch.ID}.encode()},
			Kind:       SourceGroup,
			Title:      ch.Title,
			Members:    ch.ParticipantsCount,
			Membership: MembershipJoined,
		}
		switch {
		case ch.Deactivated:
			s.Membership = MembershipNotJoinable
		case ch.Left:
			s.Membership = MembershipNotJoined
		}
		return s, true
	case *tg.ChatForbidden:
		return Source{
			SourceRef:  SourceRef{Platform: PlatformTelegram, ID: strconv.FormatInt(ch.ID, 10), Ref: tgRef{Kind: refChat, ID: ch.ID}.encode()},
			Kind:       SourceGroup,
			Title:      ch.Title,
			Membership: MembershipNotJoinable,
		}, true
	}
	return Source{}, false
}

// sourceFromInvite describes an invite link. A chat the account cannot see yet
// has no id, so its ID is "invite:<hash>" until JoinSource returns the real one.
func sourceFromInvite(hash string, inv tg.ChatInviteClass) (Source, bool) {
	link := "https://t.me/+" + hash
	switch v := inv.(type) {
	case *tg.ChatInviteAlready:
		s, ok := sourceFromChat(v.Chat)
		if ok {
			s.Ref = withInvite(s.Ref, hash)
			if s.Link == "" {
				s.Link = link
			}
		}
		return s, ok
	case *tg.ChatInvitePeek:
		s, ok := sourceFromChat(v.Chat)
		if ok {
			s.Ref = withInvite(s.Ref, hash)
			s.Membership = MembershipNotJoined
			if s.Link == "" {
				s.Link = link
			}
		}
		return s, ok
	case *tg.ChatInvite:
		s := Source{
			SourceRef:     SourceRef{Platform: PlatformTelegram, ID: inviteIDPrefix + hash, Ref: tgRef{Kind: refInvite, Invite: hash}.encode()},
			Kind:          SourceInvite,
			Title:         v.Title,
			Link:          link,
			About:         v.About,
			Members:       v.ParticipantsCount,
			Membership:    MembershipNotJoined,
			RequestNeeded: v.RequestNeeded,
			Broadcast:     v.Broadcast,
		}
		return s, true
	}
	return Source{}, false
}

func userFromTG(u *tg.User) User {
	res := User{
		UserRef:   UserRef{Platform: PlatformTelegram, ID: strconv.FormatInt(u.ID, 10), Ref: tgRef{Kind: refUser, ID: u.ID, Hash: u.AccessHash}.encode()},
		Username:  u.Username,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Bot:       u.Bot,
	}
	if res.Username == "" {
		for _, n := range u.Usernames {
			if n.Active {
				res.Username = n.Username
				break
			}
		}
	}
	if res.Username != "" {
		res.Link = "https://t.me/" + res.Username
	}
	return res
}
