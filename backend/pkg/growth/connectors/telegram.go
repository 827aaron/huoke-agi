package connectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"

	"github.com/caarlos0/env/v10"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func init() {
	Register(PlatformTelegram, newTelegramConnector)
	RegisterCredentialCheck(PlatformTelegram, checkTelegramCredentials)
	RegisterPhoneLogin(PlatformTelegram, startTelegramLogin)
}

type TelegramConfig struct {
	APIID   int
	APIHash string
	// SessionDir holds the join pacing state of every account and the session
	// file of an account that comes without a SessionStore. Session files hold
	// the account's auth key, so the directory lives outside every container.
	SessionDir string
	TestServer bool `env:"TG_TEST_SERVER" envDefault:"false"`
	// A FLOOD_WAIT up to this long is slept through inside the call; a longer
	// one is returned as a FloodWaitError so the caller is not blocked.
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

// TelegramConfigFor takes the app credentials and the session directory from
// cfg and the pacing defaults from the environment.
func TelegramConfigFor(cfg *config.Config) (TelegramConfig, error) {
	var c TelegramConfig
	if err := env.Parse(&c); err != nil {
		return TelegramConfig{}, fmt.Errorf("parse telegram config: %w", err)
	}
	c.APIID, c.APIHash, c.SessionDir = cfg.TGAPIID, cfg.TGAPIHash, cfg.TGSessionDir
	return c, nil
}

func (c TelegramConfig) Validate() error {
	switch {
	case c.APIID <= 0:
		return errors.New("TG_API_ID is not set")
	case c.APIHash == "":
		return errors.New("TG_API_HASH is not set")
	}
	return nil
}

func (c TelegramConfig) accountFile(acc Account, suffix string) string {
	if c.SessionDir == "" {
		return ""
	}
	return filepath.Join(c.SessionDir, fmt.Sprintf("account-%d%s", acc.ID, suffix))
}

// telegramAPI is the part of *tg.Client the connector calls; tests replace it.
type telegramAPI interface {
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
	maxKnownUsers      = 50000
)

var (
	errNotStarted = errors.New("telegram client is not running")
	// errJoinToResolve is not ErrNotFound: the invite is alive, and the service
	// keeps such a source unresolved until JoinSource returns it.
	errJoinToResolve = errors.New("the group behind this invite link shows itself only after joining; join it first")
)

// Telegram is the connector of one Telegram user account.
type Telegram struct {
	cfg     TelegramConfig
	account Account
	client  *telegram.Client
	pacer   *pacer

	mu   sync.RWMutex
	api  telegramAPI
	stop context.CancelFunc
	done chan struct{}

	// users holds how to address the senders this account has read, because
	// GetUser receives a bare id and Telegram wants an access hash or the
	// message the user was seen in.
	usersMu sync.Mutex
	users   map[int64]tgRef
}

var _ Connector = (*Telegram)(nil)

func newTelegramConnector(ctx context.Context, cfg *config.Config, acc Account) (Connector, error) {
	tc, err := TelegramConfigFor(cfg)
	if err != nil {
		return nil, err
	}
	t, err := NewTelegram(tc, acc)
	if err != nil {
		return nil, err
	}
	if err := t.Start(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

func NewTelegram(cfg TelegramConfig, acc Account) (*Telegram, error) {
	client, err := newTelegramClient(cfg, acc)
	if err != nil {
		return nil, err
	}
	t := newTelegramWithAPI(cfg, acc, nil)
	t.client = client
	return t, nil
}

func newTelegramWithAPI(cfg TelegramConfig, acc Account, api telegramAPI) *Telegram {
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 30 * time.Second
	}
	return &Telegram{
		cfg:     cfg,
		account: acc,
		api:     api,
		pacer:   newPacer(cfg, cfg.accountFile(acc, ".joins.json"), sleepCtx),
		users:   map[int64]tgRef{},
	}
}

func newTelegramClient(cfg TelegramConfig, acc Account) (*telegram.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dial, err := proxyDialer(acc.Proxy)
	if err != nil {
		return nil, err
	}
	if cfg.SessionDir != "" {
		if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
			return nil, fmt.Errorf("create telegram session directory: %w", err)
		}
	}
	var storage telegram.SessionStorage
	switch {
	case acc.Session != nil:
		storage = sessionStore{acc.Session}
	case cfg.SessionDir != "":
		storage = &session.FileStorage{Path: cfg.accountFile(acc, ".session.json")}
	default:
		return nil, errors.New("telegram account has no session store and TG_SESSION_DIR is not set")
	}
	opts := telegram.Options{SessionStorage: storage, Resolver: dcs.Plain(dcs.PlainOptions{Dial: dial})}
	if cfg.TestServer {
		opts.DCList = dcs.Test()
	}
	return telegram.NewClient(cfg.APIID, cfg.APIHash, opts), nil
}

// sessionStore maps the backend's empty session to the error gotd expects
// before the first login.
type sessionStore struct{ s SessionStore }

func (a sessionStore) LoadSession(ctx context.Context) ([]byte, error) {
	data, err := a.s.LoadSession(ctx)
	if err == nil && len(data) == 0 {
		return nil, session.ErrNotFound
	}
	return data, err
}

func (a sessionStore) StoreSession(ctx context.Context, data []byte) error {
	return a.s.StoreSession(ctx, data)
}

// Start connects and returns once the connection is up and the session is
// logged in; a missing login is ErrUnauthorized. The connection lives until
// ctx is done or Close is called.
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

	timeout := time.NewTimer(t.cfg.CallTimeout)
	defer timeout.Stop()
	select {
	case <-ready:
	case err := <-runErr:
		cancel()
		return fmt.Errorf("connect to telegram: %w", err)
	case <-timeout.C:
		cancel()
		<-done
		return fmt.Errorf("connect to telegram: no connection within %s", t.cfg.CallTimeout)
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	}
	t.pacer.resetLogin()
	t.mu.Lock()
	t.stop, t.done = cancel, done
	t.mu.Unlock()

	actx, acancel := context.WithTimeout(ctx, t.cfg.CallTimeout)
	defer acancel()
	status, err := t.client.Auth().Status(actx)
	switch {
	case err != nil:
		_ = t.Close()
		return fmt.Errorf("check telegram login: %w", errorOf(err))
	case !status.Authorized:
		_ = t.Close()
		return fmt.Errorf("%w: telegram account %d has no login", ErrUnauthorized, t.account.ID)
	}
	return nil
}

func (t *Telegram) Close() error {
	t.mu.Lock()
	stop, done := t.stop, t.done
	t.stop, t.done = nil, nil
	t.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
	return nil
}

// checkTelegramCredentials sends one unauthenticated request: Telegram checks
// api_id and api_hash on the first request of a connection.
func checkTelegramCredentials(ctx context.Context, cfg *config.Config) error {
	tc, err := TelegramConfigFor(cfg)
	if err != nil {
		return err
	}
	if err := tc.Validate(); err != nil {
		return err
	}
	dial, err := proxyDialer(cfg.GrowthProxyURL)
	if err != nil {
		return err
	}
	opts := telegram.Options{SessionStorage: &session.StorageMemory{}, Resolver: dcs.Plain(dcs.PlainOptions{Dial: dial})}
	if tc.TestServer {
		opts.DCList = dcs.Test()
	}
	client := telegram.NewClient(tc.APIID, tc.APIHash, opts)
	ctx, cancel := context.WithTimeout(ctx, tc.CallTimeout)
	defer cancel()
	return client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().HelpGetConfig(ctx)
		if tgerr.Is(err, "API_ID_INVALID", "API_ID_PUBLISHED_FLOOD") {
			return fmt.Errorf("telegram rejected the api_id and api_hash pair: %w", err)
		}
		return err
	})
}

func (t *Telegram) Platform() Platform { return PlatformTelegram }

func (t *Telegram) Capabilities() []Capability {
	return []Capability{
		CapSearchSources, CapResolveSource, CapRecommendSources,
		CapJoinSource, CapReadHistory, CapUserProfile,
	}
}

func (t *Telegram) SearchSources(ctx context.Context, query string, limit int) ([]Source, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search query is empty")
	}
	limit = clamp(limit, defaultSearchLimit, maxSearchLimit)
	var chats []tg.ChatClass
	err := t.call(ctx, "contacts.search", func(ctx context.Context, api telegramAPI) error {
		res, err := api.ContactsSearch(ctx, &tg.ContactsSearchRequest{Q: query, Limit: limit})
		if err == nil {
			chats = res.Chats
		}
		return err
	})
	if err != nil {
		return nil, errorOf(err)
	}
	return joinableSources(chats, limit), nil
}

func (t *Telegram) RecommendSources(ctx context.Context, ref SourceRef, limit int) ([]Source, error) {
	r, err := t.refOf(ctx, ref)
	if err != nil {
		return nil, errorOf(err)
	}
	channel, ok := r.inputChannel()
	if !ok {
		return nil, fmt.Errorf("%w: telegram recommends only next to channels and supergroups", ErrUnsupported)
	}
	limit = clamp(limit, defaultSearchLimit, maxSearchLimit)
	var chats []tg.ChatClass
	err = t.call(ctx, "channels.getChannelRecommendations", func(ctx context.Context, api telegramAPI) error {
		res, err := api.ChannelsGetChannelRecommendations(ctx, &tg.ChannelsGetChannelRecommendationsRequest{Channel: channel})
		if err == nil {
			chats = res.GetChats()
		}
		return err
	})
	if err != nil {
		return nil, errorOf(err)
	}
	return joinableSources(chats, limit), nil
}

func joinableSources(chats []tg.ChatClass, limit int) []Source {
	sources := make([]Source, 0, len(chats))
	for _, c := range chats {
		if info, ok := chatInfoOf(c); ok && info.status != JoinStatusNotJoinable {
			sources = append(sources, info.source)
		}
		if len(sources) == limit {
			break
		}
	}
	return sources
}

func (t *Telegram) ResolveSource(ctx context.Context, link string) (Source, error) {
	l, err := parseTelegramLink(link)
	if err != nil {
		return Source{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	var info chatInfo
	if l.Invite != "" {
		info, err = t.resolveInvite(ctx, l.Invite)
		if err == nil && info.source.ExternalID == "" {
			return Source{}, errJoinToResolve
		}
	} else {
		info, err = t.resolveChat(ctx, l.Username)
	}
	if err != nil {
		return Source{}, errorOf(err)
	}
	return info.source, nil
}

func (t *Telegram) resolveChat(ctx context.Context, username string) (chatInfo, error) {
	res, err := t.resolveUsername(ctx, username)
	if err != nil {
		return chatInfo{}, err
	}
	var id int64
	switch p := res.Peer.(type) {
	case *tg.PeerChannel:
		id = p.ChannelID
	case *tg.PeerChat:
		id = p.ChatID
	default:
		return chatInfo{}, fmt.Errorf("%w: @%s is a person, not a group or channel", ErrNotFound, username)
	}
	for _, c := range res.Chats {
		if c.GetID() == id {
			if info, ok := chatInfoOf(c); ok {
				return info, nil
			}
		}
	}
	return chatInfo{}, fmt.Errorf("%w: @%s is missing from the response", ErrNotFound, username)
}

func (t *Telegram) resolveUsername(ctx context.Context, username string) (res *tg.ContactsResolvedPeer, err error) {
	err = t.call(ctx, "contacts.resolveUsername", func(ctx context.Context, api telegramAPI) (err error) {
		res, err = api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
		return err
	})
	return res, err
}

// resolveInvite describes an invite link. A chat the account cannot see yet
// comes back with an empty ExternalID and an invite peer.
func (t *Telegram) resolveInvite(ctx context.Context, hash string) (chatInfo, error) {
	var inv tg.ChatInviteClass
	err := t.call(ctx, "messages.checkChatInvite", func(ctx context.Context, api telegramAPI) (err error) {
		inv, err = api.MessagesCheckChatInvite(ctx, hash)
		return err
	})
	if err != nil {
		return chatInfo{}, err
	}
	link := "https://t.me/+" + hash
	switch v := inv.(type) {
	case *tg.ChatInviteAlready:
		if info, ok := chatInfoOf(v.Chat); ok {
			return info.withInvite(hash, link), nil
		}
	case *tg.ChatInvitePeek:
		if info, ok := chatInfoOf(v.Chat); ok {
			info.status = JoinStatusNotJoined
			return info.withInvite(hash, link), nil
		}
	case *tg.ChatInvite:
		ref := tgRef{Kind: refInvite, Invite: hash}
		kind := SourceKindGroup
		if v.Broadcast {
			kind = SourceKindChannel
		}
		return chatInfo{
			source: Source{
				Title:       v.Title,
				Link:        link,
				Kind:        kind,
				MemberCount: int64(v.ParticipantsCount),
				Description: v.About,
				Peer:        ref.peer(),
			},
			ref:    ref,
			status: JoinStatusNotJoined,
		}, nil
	}
	return chatInfo{}, fmt.Errorf("unexpected telegram invite type %T", inv)
}

// refOf finds the account's address of a source: its stored peer, else its
// username, else its link. ExternalID alone carries no access hash.
func (t *Telegram) refOf(ctx context.Context, ref SourceRef) (tgRef, error) {
	if len(ref.Peer) > 0 && string(ref.Peer) != "null" {
		return peerRef(ref.Peer)
	}
	if ref.Username != "" {
		info, err := t.resolveChat(ctx, strings.TrimPrefix(ref.Username, "@"))
		return info.ref, err
	}
	if ref.Link != "" {
		l, err := parseTelegramLink(ref.Link)
		if err != nil {
			return tgRef{}, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		if l.Invite != "" {
			return tgRef{Kind: refInvite, Invite: l.Invite}, nil
		}
		info, err := t.resolveChat(ctx, l.Username)
		return info.ref, err
	}
	return tgRef{}, fmt.Errorf("%w: telegram source %q has no peer, username or link", ErrNotFound, ref.ExternalID)
}

// JoinSource joins by invite when the ref carries one, since a private chat
// cannot be joined by id. A join that times out is checked before it is
// retried once, so a join that did land is not sent twice.
func (t *Telegram) JoinSource(ctx context.Context, ref SourceRef) (JoinResult, error) {
	r, err := t.refOf(ctx, ref)
	if err != nil {
		return JoinResult{}, errorOf(err)
	}
	if r.Kind == refChat && r.Invite == "" {
		res, err := t.membership(ctx, r)
		if err != nil {
			return JoinResult{}, errorOf(err)
		}
		if res.Status != JoinStatusJoined {
			res = JoinResult{Status: JoinStatusNotJoinable, Reason: "a legacy group can be joined only through its invite link"}
		}
		return res, nil
	}

	res, err := t.joinOnce(ctx, r)
	if !transient(err) {
		return res, errorOf(err)
	}
	check, cerr := t.membership(ctx, r)
	switch {
	case cerr != nil:
		return JoinResult{}, fmt.Errorf("the join timed out and membership could not be checked; check it before joining again: %w", errorOf(err))
	case check.Status == JoinStatusNotJoined:
		res, err = t.joinOnce(ctx, r)
		return res, errorOf(err)
	}
	return check, nil
}

func (t *Telegram) joinOnce(ctx context.Context, r tgRef) (JoinResult, error) {
	if err := t.pacer.beforeJoin(ctx); err != nil {
		return JoinResult{}, err
	}
	method := "channels.joinChannel"
	if r.Invite != "" {
		method = "messages.importChatInvite"
	}
	var joined tg.MessagesChatInviteJoinResultClass
	err := t.call(ctx, method, func(ctx context.Context, api telegramAPI) (err error) {
		if r.Invite != "" {
			joined, err = api.MessagesImportChatInvite(ctx, r.Invite)
		} else {
			channel, _ := r.inputChannel()
			joined, err = api.ChannelsJoinChannel(ctx, channel)
		}
		return err
	})

	if err == nil || tgerr.Is(err, "INVITE_REQUEST_SENT") || transient(err) {
		t.pacer.joined()
	}

	switch {
	case err == nil:
	case tgerr.Is(err, "INVITE_REQUEST_SENT"):
		return JoinResult{Status: JoinStatusPending, Reason: "join request sent; a group admin has to approve it"}, nil
	case tgerr.Is(err, "USER_ALREADY_PARTICIPANT"):
		return JoinResult{Status: JoinStatusJoined}, nil
	case tgerr.Is(err, "INVITE_HASH_EXPIRED", "INVITE_HASH_INVALID", "INVITE_HASH_EMPTY",
		"CHANNEL_PRIVATE", "CHANNEL_INVALID", "CHAT_INVALID", "USER_BANNED_IN_CHANNEL", "CHANNEL_PUBLIC_GROUP_NA"):
		return JoinResult{Status: JoinStatusNotJoinable, Reason: rpcType(err)}, nil
	default:
		// CHANNELS_TOO_MUCH and flood waits concern the account, not the source.
		return JoinResult{}, err
	}

	switch v := joined.(type) {
	case *tg.MessagesChatInviteJoinResultOk:
		res := JoinResult{Status: JoinStatusJoined}
		if u, ok := v.Updates.(interface{ GetChats() []tg.ChatClass }); ok {
			for _, c := range u.GetChats() {
				if info, ok := chatInfoOf(c); ok && (r.ID == 0 || c.GetID() == r.ID) {
					if r.Invite != "" {
						info = info.withInvite(r.Invite, "https://t.me/+"+r.Invite)
					}
					res.Source = &info.source
					break
				}
			}
		}
		return res, nil
	case *tg.MessagesChatInviteJoinResultWebView:
		return JoinResult{Status: JoinStatusNotJoinable, Reason: "the group requires a bot web app before joining"}, nil
	}
	return JoinResult{Status: JoinStatusJoined}, nil
}

func (t *Telegram) membership(ctx context.Context, r tgRef) (JoinResult, error) {
	switch r.Kind {
	case refInvite:
		return t.inviteMembership(ctx, r.Invite)
	case refChannel:
		channel, _ := r.inputChannel()
		var res *tg.ChannelsChannelParticipant
		err := t.call(ctx, "channels.getParticipant", func(ctx context.Context, api telegramAPI) (err error) {
			res, err = api.ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{Channel: channel, Participant: &tg.InputPeerSelf{}})
			return err
		})
		switch {
		case err == nil:
			switch res.Participant.(type) {
			case *tg.ChannelParticipantBanned:
				return JoinResult{Status: JoinStatusNotJoinable, Reason: "USER_BANNED_IN_CHANNEL"}, nil
			case *tg.ChannelParticipantLeft:
				return JoinResult{Status: JoinStatusNotJoined}, nil
			}
			return JoinResult{Status: JoinStatusJoined}, nil
		case tgerr.Is(err, "USER_NOT_PARTICIPANT"):
			return JoinResult{Status: JoinStatusNotJoined}, nil
		case tgerr.Is(err, "CHANNEL_PRIVATE") && r.Invite != "":
			return t.inviteMembership(ctx, r.Invite)
		case tgerr.Is(err, "CHANNEL_PRIVATE", "CHANNEL_INVALID", "USER_BANNED_IN_CHANNEL"):
			return JoinResult{Status: JoinStatusNotJoinable, Reason: rpcType(err)}, nil
		}
		return JoinResult{}, err
	case refChat:
		var res tg.MessagesChatsClass
		err := t.call(ctx, "messages.getChats", func(ctx context.Context, api telegramAPI) (err error) {
			res, err = api.MessagesGetChats(ctx, []int64{r.ID})
			return err
		})
		if tgerr.Is(err, "CHAT_ID_INVALID", "PEER_ID_INVALID") {
			return JoinResult{Status: JoinStatusNotJoined}, nil
		}
		if err != nil {
			return JoinResult{}, err
		}
		for _, c := range res.GetChats() {
			if c.GetID() == r.ID {
				info, _ := chatInfoOf(c)
				return JoinResult{Status: info.status, Source: &info.source}, nil
			}
		}
		return JoinResult{Status: JoinStatusNotJoined}, nil
	}
	return JoinResult{}, fmt.Errorf("telegram peer of kind %q is not a source", r.Kind)
}

func (t *Telegram) inviteMembership(ctx context.Context, hash string) (JoinResult, error) {
	info, err := t.resolveInvite(ctx, hash)
	if tgerr.Is(err, "INVITE_HASH_EXPIRED", "INVITE_HASH_INVALID", "INVITE_HASH_EMPTY") {
		return JoinResult{Status: JoinStatusNotJoinable, Reason: rpcType(err)}, nil
	}
	if err != nil {
		return JoinResult{}, err
	}
	res := JoinResult{Status: info.status}
	if info.source.ExternalID != "" {
		res.Source = &info.source
	}
	return res, nil
}

// ReadHistory pages backwards from OffsetID, or forwards from MinID: with only
// MinID set it returns the Limit messages just above it, so a cursor that
// advances to the newest of them skips nothing.
func (t *Telegram) ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error) {
	r, err := t.refOf(ctx, ref)
	if err != nil {
		return HistoryPage{}, errorOf(err)
	}
	if r.Kind == refInvite {
		return HistoryPage{}, errors.New("an invite-only source can be read after it is joined")
	}
	peer, _ := r.inputPeer()
	limit := clamp(req.Limit, defaultPageSize, maxPageSize)
	hreq := &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: int(req.OffsetID), MinID: int(req.MinID), Limit: limit}
	if req.MinID > 0 && req.OffsetID == 0 {
		hreq.OffsetID, hreq.AddOffset = int(req.MinID)+1, -limit
	}

	var raw []tg.MessageClass
	var users []tg.UserClass
	err = t.call(ctx, "messages.getHistory", func(ctx context.Context, api telegramAPI) error {
		res, err := api.MessagesGetHistory(ctx, hreq)
		if err != nil {
			return err
		}
		if m, ok := res.AsModified(); ok {
			raw, users = m.GetMessages(), m.GetUsers()
		}
		return nil
	})
	if err != nil {
		return HistoryPage{}, errorOf(err)
	}
	return HistoryPage{
		Messages:  t.messages(r, raw, users),
		Exhausted: req.MinID == 0 && len(raw) < limit,
	}, nil
}

func (t *Telegram) messages(r tgRef, raw []tg.MessageClass, users []tg.UserClass) []Message {
	byID := make(map[int64]*tg.User, len(users))
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			byID[user.ID] = user
		}
	}
	out := make([]Message, 0, len(raw))
	for _, rm := range raw {
		m, ok := rm.(*tg.Message)
		if !ok {
			continue
		}
		msg := Message{
			ID:   int64(m.ID),
			Text: m.Message,
			Time: time.Unix(int64(m.Date), 0).UTC(),
			Link: messageLink(r, m.ID),
		}
		if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
			msg.ReplyToID = int64(h.ReplyToMsgID)
		}
		if from, ok := m.FromID.(*tg.PeerUser); ok {
			msg.SenderID = strconv.FormatInt(from.UserID, 10)
			via := tgRef{Kind: refUser, ID: from.UserID, FromPeer: &tgRef{Kind: r.Kind, ID: r.ID, Hash: r.Hash}, FromMsg: m.ID}
			if u := byID[from.UserID]; u != nil {
				p := profileOf(u)
				msg.SenderUsername, msg.SenderName, msg.SenderIsBot = p.Username, p.DisplayName, p.IsBot
				if !u.Min && u.AccessHash != 0 {
					via = tgRef{Kind: refUser, ID: u.ID, Hash: u.AccessHash}
				}
			}
			t.rememberUser(via)
		}
		out = append(out, msg)
	}
	return out
}

func (t *Telegram) rememberUser(r tgRef) {
	t.usersMu.Lock()
	defer t.usersMu.Unlock()
	if known, ok := t.users[r.ID]; ok && known.FromPeer == nil && r.FromPeer != nil {
		return
	}
	if len(t.users) >= maxKnownUsers {
		clear(t.users)
	}
	t.users[r.ID] = r
}

func (t *Telegram) knownUser(id int64) (tgRef, bool) {
	t.usersMu.Lock()
	defer t.usersMu.Unlock()
	r, ok := t.users[id]
	return r, ok
}

func messageLink(r tgRef, id int) string {
	switch {
	case r.Kind != refChannel:
		return ""
	case r.Username != "":
		return fmt.Sprintf("https://t.me/%s/%d", r.Username, id)
	}
	return fmt.Sprintf("https://t.me/c/%d/%d", r.ID, id)
}

// GetUser takes a numeric id of a sender this connector has read, or a
// username.
func (t *Telegram) GetUser(ctx context.Context, userID string) (UserProfile, error) {
	var input tg.InputUserClass
	if id, err := strconv.ParseInt(userID, 10, 64); err == nil {
		r, ok := t.knownUser(id)
		if !ok {
			return UserProfile{}, fmt.Errorf("%w: telegram user %s was not seen in a message this account read; read the source first", ErrNotFound, userID)
		}
		input, _ = r.inputUser()
	} else {
		name := strings.TrimPrefix(userID, "@")
		res, err := t.resolveUsername(ctx, name)
		if err != nil {
			return UserProfile{}, errorOf(err)
		}
		for _, u := range res.Users {
			if tu, ok := u.(*tg.User); ok {
				input = &tg.InputUser{UserID: tu.ID, AccessHash: tu.AccessHash}
			}
		}
		if input == nil {
			return UserProfile{}, fmt.Errorf("%w: @%s is not a person", ErrNotFound, name)
		}
	}

	var full *tg.UsersUserFull
	err := t.call(ctx, "users.getFullUser", func(ctx context.Context, api telegramAPI) (err error) {
		full, err = api.UsersGetFullUser(ctx, input)
		return err
	})
	if err != nil {
		return UserProfile{}, errorOf(err)
	}
	for _, u := range full.Users {
		if tu, ok := u.(*tg.User); ok && tu.ID == full.FullUser.ID {
			if !tu.Min && tu.AccessHash != 0 {
				t.rememberUser(tgRef{Kind: refUser, ID: tu.ID, Hash: tu.AccessHash})
			}
			p := profileOf(tu)
			p.Bio = full.FullUser.About
			return p, nil
		}
	}
	return UserProfile{}, fmt.Errorf("%w: telegram user %s is missing from the response", ErrNotFound, userID)
}

func profileOf(u *tg.User) UserProfile {
	p := UserProfile{
		ID:          strconv.FormatInt(u.ID, 10),
		Username:    u.Username,
		DisplayName: strings.TrimSpace(u.FirstName + " " + u.LastName),
		IsBot:       u.Bot,
	}
	if p.Username == "" {
		for _, n := range u.Usernames {
			if n.Active {
				p.Username = n.Username
				break
			}
		}
	}
	return p
}

// call runs fn against the live API, sleeping through flood waits no longer
// than MaxInlineFloodWait. Telegram counts a flood wait per method, so the
// retry repeats only the call that was refused.
func (t *Telegram) call(ctx context.Context, method string, fn func(ctx context.Context, api telegramAPI) error) error {
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
		// The caller never sees an absorbed wait, but the account's health does.
		if t.account.Health != nil {
			t.account.Health.ReportRateLimited(ctx, wait)
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
)

// errorOf maps a Telegram error to the errors the growth service acts on.
func errorOf(err error) error {
	if err == nil {
		return nil
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return &FloodWaitError{Wait: wait}
	}
	var paced *joinPacingError
	switch {
	case errors.As(err, &paced):
		return &FloodWaitError{Wait: paced.wait}
	case errors.Is(err, errNeedsLogin):
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	rpc, ok := tgerr.As(err)
	if !ok {
		return err
	}
	switch {
	case rpc.IsOneOf(needsLoginTypes...) || rpc.Code == 401:
		return fmt.Errorf("%w: telegram %s", ErrUnauthorized, rpc.Type)
	case rpc.IsOneOf(notFoundTypes...):
		return fmt.Errorf("%w: telegram %s", ErrNotFound, rpc.Type)
	case rpc.Code == 420 && rpc.Argument > 0:
		return &FloodWaitError{Wait: time.Duration(rpc.Argument) * time.Second}
	}
	return fmt.Errorf("telegram: %w", err)
}

// transient is a failure after which a join may or may not have landed.
func transient(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	rpc, ok := tgerr.As(err)
	return ok && rpc.Code >= 500
}

func rpcType(err error) string {
	if rpc, ok := tgerr.As(err); ok {
		return rpc.Type
	}
	return ""
}

func clamp(n, def, hi int) int {
	switch {
	case n <= 0:
		return def
	case n > hi:
		return hi
	}
	return n
}

// chatInfo is a source together with the account's address of it and the
// account's membership.
type chatInfo struct {
	source Source
	ref    tgRef
	status JoinStatus
}

func (c chatInfo) withInvite(hash, link string) chatInfo {
	c.ref.Invite = hash
	c.source.Peer = c.ref.peer()
	if c.source.Link == "" {
		c.source.Link = link
	}
	return c
}

func chatInfoOf(c tg.ChatClass) (chatInfo, bool) {
	var info chatInfo
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
		info.ref = tgRef{Kind: refChannel, ID: ch.ID, Hash: ch.AccessHash, Username: username}
		info.source = Source{Username: username, Title: ch.Title, Kind: SourceKindGroup, MemberCount: int64(ch.ParticipantsCount)}
		if ch.Broadcast {
			info.source.Kind = SourceKindChannel
		}
		if username != "" {
			info.source.Link = "https://t.me/" + username
		}
		info.status = JoinStatusJoined
		if ch.Left {
			info.status = JoinStatusNotJoined
		}
	case *tg.ChannelForbidden:
		info.ref = tgRef{Kind: refChannel, ID: ch.ID, Hash: ch.AccessHash}
		info.source = Source{Title: ch.Title, Kind: SourceKindGroup}
		if ch.Broadcast {
			info.source.Kind = SourceKindChannel
		}
		info.status = JoinStatusNotJoinable
	case *tg.Chat:
		info.ref = tgRef{Kind: refChat, ID: ch.ID}
		info.source = Source{Title: ch.Title, Kind: SourceKindGroup, MemberCount: int64(ch.ParticipantsCount)}
		switch {
		case ch.Deactivated:
			info.status = JoinStatusNotJoinable
		case ch.Left:
			info.status = JoinStatusNotJoined
		default:
			info.status = JoinStatusJoined
		}
	case *tg.ChatForbidden:
		info.ref = tgRef{Kind: refChat, ID: ch.ID}
		info.source = Source{Title: ch.Title, Kind: SourceKindGroup}
		info.status = JoinStatusNotJoinable
	default:
		return chatInfo{}, false
	}
	info.source.ExternalID = strconv.FormatInt(info.ref.ID, 10)
	info.source.Peer = info.ref.peer()
	return info, true
}
