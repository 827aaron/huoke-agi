package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/config"

	"github.com/gotd/td/session"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTelegramAPI struct {
	t *testing.T

	getFullUser     func(id tg.InputUserClass) (*tg.UsersUserFull, error)
	search          func(req *tg.ContactsSearchRequest) (*tg.ContactsFound, error)
	resolveUsername func(req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	recommendations func(req *tg.ChannelsGetChannelRecommendationsRequest) (tg.MessagesChatsClass, error)
	joinChannel     func(channel tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error)
	getParticipant  func(req *tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error)
	checkInvite     func(hash string) (tg.ChatInviteClass, error)
	importInvite    func(hash string) (tg.MessagesChatInviteJoinResultClass, error)
	getHistory      func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error)
	getChats        func(id []int64) (tg.MessagesChatsClass, error)
}

func (f *fakeTelegramAPI) unexpected(method string) error {
	f.t.Helper()
	f.t.Errorf("unexpected call to %s", method)
	return errors.New("unexpected call")
}

func (f *fakeTelegramAPI) UsersGetFullUser(ctx context.Context, id tg.InputUserClass) (*tg.UsersUserFull, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.getFullUser == nil {
		return nil, f.unexpected("UsersGetFullUser")
	}
	return f.getFullUser(id)
}

func (f *fakeTelegramAPI) ContactsSearch(ctx context.Context, req *tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.search == nil {
		return nil, f.unexpected("ContactsSearch")
	}
	return f.search(req)
}

func (f *fakeTelegramAPI) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.resolveUsername == nil {
		return nil, f.unexpected("ContactsResolveUsername")
	}
	return f.resolveUsername(req)
}

func (f *fakeTelegramAPI) ChannelsGetChannelRecommendations(ctx context.Context, req *tg.ChannelsGetChannelRecommendationsRequest) (tg.MessagesChatsClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.recommendations == nil {
		return nil, f.unexpected("ChannelsGetChannelRecommendations")
	}
	return f.recommendations(req)
}

func (f *fakeTelegramAPI) ChannelsJoinChannel(ctx context.Context, channel tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.joinChannel == nil {
		return nil, f.unexpected("ChannelsJoinChannel")
	}
	return f.joinChannel(channel)
}

func (f *fakeTelegramAPI) ChannelsGetParticipant(ctx context.Context, req *tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.getParticipant == nil {
		return nil, f.unexpected("ChannelsGetParticipant")
	}
	return f.getParticipant(req)
}

func (f *fakeTelegramAPI) MessagesCheckChatInvite(ctx context.Context, hash string) (tg.ChatInviteClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.checkInvite == nil {
		return nil, f.unexpected("MessagesCheckChatInvite")
	}
	return f.checkInvite(hash)
}

func (f *fakeTelegramAPI) MessagesImportChatInvite(ctx context.Context, hash string) (tg.MessagesChatInviteJoinResultClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.importInvite == nil {
		return nil, f.unexpected("MessagesImportChatInvite")
	}
	return f.importInvite(hash)
}

func (f *fakeTelegramAPI) MessagesGetHistory(ctx context.Context, req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.getHistory == nil {
		return nil, f.unexpected("MessagesGetHistory")
	}
	return f.getHistory(req)
}

func (f *fakeTelegramAPI) MessagesGetChats(ctx context.Context, id []int64) (tg.MessagesChatsClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.getChats == nil {
		return nil, f.unexpected("MessagesGetChats")
	}
	return f.getChats(id)
}

type sleepRecorder struct{ waits []time.Duration }

func (s *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	return ctx.Err()
}

type healthRecorder struct {
	mu          sync.Mutex
	rateLimited []time.Duration
}

func (h *healthRecorder) ReportSuccess(context.Context)      {}
func (h *healthRecorder) ReportUnauthorized(context.Context) {}
func (h *healthRecorder) ReportError(context.Context, error) {}
func (h *healthRecorder) ReportRateLimited(_ context.Context, wait time.Duration) {
	h.mu.Lock()
	h.rateLimited = append(h.rateLimited, wait)
	h.mu.Unlock()
}

func newTestTelegram(t *testing.T, api *fakeTelegramAPI) (*Telegram, *sleepRecorder) {
	t.Helper()
	api.t = t
	tgc := newTelegramWithAPI(TelegramConfig{MaxInlineFloodWait: 60 * time.Second, CallTimeout: 5 * time.Second}, Account{ID: 1}, api)
	rec := &sleepRecorder{}
	tgc.pacer.sleep = rec.sleep
	return tgc, rec
}

func gamesChannel() *tg.Channel {
	return &tg.Channel{ID: 1001, AccessHash: 77, Title: "Mobile Gamers", Username: "mobilegamers", Megagroup: true, ParticipantsCount: 5400, Left: true}
}

func channelRef(id, hash int64, username string) SourceRef {
	r := tgRef{Kind: refChannel, ID: id, Hash: hash, Username: username}
	return SourceRef{ExternalID: strconv.FormatInt(id, 10), Peer: r.peer()}
}

func TestTelegram_Call_SleepsThroughShortFloodWait(t *testing.T) {
	calls := 0
	tgc, rec := newTestTelegram(t, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			if calls == 1 {
				return nil, tgerr.New(420, "FLOOD_WAIT_7")
			}
			return &tg.ContactsFound{Chats: []tg.ChatClass{gamesChannel()}}, nil
		},
	})
	health := &healthRecorder{}
	tgc.account.Health = health

	sources, err := tgc.SearchSources(context.Background(), "genshin", 0)
	require.NoError(t, err)
	assert.Len(t, sources, 1)
	assert.Equal(t, 2, calls)
	assert.Equal(t, []time.Duration{7 * time.Second}, rec.waits)
	assert.Equal(t, []time.Duration{7 * time.Second}, health.rateLimited)
}

func TestTelegram_Call_ReturnsLongFloodWait(t *testing.T) {
	tgc, rec := newTestTelegram(t, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			return nil, tgerr.New(420, "FLOOD_WAIT_3600")
		},
	})
	_, err := tgc.SearchSources(context.Background(), "genshin", 0)
	var flood *FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Equal(t, time.Hour, flood.Wait)
	assert.Empty(t, rec.waits)
}

func TestTelegram_Call_GivesUpAfterRepeatedFloodWaits(t *testing.T) {
	calls := 0
	tgc, rec := newTestTelegram(t, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			return nil, tgerr.New(420, "FLOOD_WAIT_2")
		},
	})
	_, err := tgc.SearchSources(context.Background(), "genshin", 0)
	var flood *FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Equal(t, 2*time.Second, flood.Wait)
	assert.Equal(t, 4, calls)
	assert.Len(t, rec.waits, 3)
}

func TestTelegram_Call_NotStarted(t *testing.T) {
	tgc := newTelegramWithAPI(TelegramConfig{}, Account{}, nil)
	_, err := tgc.SearchSources(context.Background(), "genshin", 0)
	assert.ErrorIs(t, err, errNotStarted)
}

func TestTelegram_SearchSources_ByQuery(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		search: func(req *tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			assert.Equal(t, "mobile games", req.Q)
			assert.Equal(t, 5, req.Limit)
			return &tg.ContactsFound{
				Chats: []tg.ChatClass{
					gamesChannel(),
					&tg.Channel{ID: 1002, AccessHash: 88, Title: "Game News", Username: "gamenews", Broadcast: true},
					&tg.ChannelForbidden{ID: 1003, Title: "Banned"},
				},
				Users: []tg.UserClass{&tg.User{ID: 5, Username: "someone"}},
			}, nil
		},
	})
	sources, err := tgc.SearchSources(context.Background(), "mobile games", 5)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	assert.Equal(t, Source{
		ExternalID:  "1001",
		Username:    "mobilegamers",
		Title:       "Mobile Gamers",
		Link:        "https://t.me/mobilegamers",
		Kind:        SourceKindGroup,
		MemberCount: 5400,
		Peer:        json.RawMessage(`{"k":"channel","id":1001,"h":77,"u":"mobilegamers"}`),
	}, sources[0])
	assert.Equal(t, SourceKindChannel, sources[1].Kind)
}

func TestTelegram_SearchSources_RejectsEmptyQuery(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{})
	_, err := tgc.SearchSources(context.Background(), "  ", 0)
	assert.ErrorContains(t, err, "empty")
}

func TestTelegram_RecommendSources(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		recommendations: func(req *tg.ChannelsGetChannelRecommendationsRequest) (tg.MessagesChatsClass, error) {
			assert.Equal(t, &tg.InputChannel{ChannelID: 1001, AccessHash: 77}, req.Channel)
			return &tg.MessagesChats{Chats: []tg.ChatClass{&tg.Channel{ID: 2001, AccessHash: 3, Title: "RPG Fans", Megagroup: true}}}, nil
		},
	})
	sources, err := tgc.RecommendSources(context.Background(), channelRef(1001, 77, ""), 0)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "2001", sources[0].ExternalID)
	assert.Empty(t, sources[0].Link)

	_, err = tgc.RecommendSources(context.Background(), SourceRef{Peer: tgRef{Kind: refChat, ID: 5}.peer()}, 0)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTelegram_ResolveSource_Username(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		resolveUsername: func(req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
			switch req.Username {
			case "mobilegamers":
				return &tg.ContactsResolvedPeer{Peer: &tg.PeerChannel{ChannelID: 1001}, Chats: []tg.ChatClass{gamesChannel()}}, nil
			case "someperson":
				return &tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: 5}, Users: []tg.UserClass{&tg.User{ID: 5}}}, nil
			}
			return nil, tgerr.New(400, "USERNAME_NOT_OCCUPIED")
		},
	})
	ctx := context.Background()

	src, err := tgc.ResolveSource(ctx, "https://t.me/mobilegamers/123")
	require.NoError(t, err)
	assert.Equal(t, "1001", src.ExternalID)
	assert.Equal(t, "Mobile Gamers", src.Title)

	_, err = tgc.ResolveSource(ctx, "@someperson")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "is a person")

	_, err = tgc.ResolveSource(ctx, "t.me/nosuchgroup")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "USERNAME_NOT_OCCUPIED")

	_, err = tgc.ResolveSource(ctx, "https://example.com/x")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTelegram_ResolveSource_Invite(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		checkInvite: func(hash string) (tg.ChatInviteClass, error) {
			switch hash {
			case "AbCdEfGh123":
				return &tg.ChatInvite{Title: "Private Gamers", ParticipantsCount: 300, RequestNeeded: true, Megagroup: true}, nil
			case "MemberAlready1":
				return &tg.ChatInviteAlready{Chat: &tg.Channel{ID: 3001, AccessHash: 4, Title: "Inner", Megagroup: true}}, nil
			}
			return nil, tgerr.New(400, "INVITE_HASH_EXPIRED")
		},
	})
	ctx := context.Background()

	_, err := tgc.ResolveSource(ctx, "https://t.me/+AbCdEfGh123")
	assert.ErrorIs(t, err, errJoinToResolve)
	assert.NotErrorIs(t, err, ErrNotFound)

	src, err := tgc.ResolveSource(ctx, "https://t.me/joinchat/MemberAlready1")
	require.NoError(t, err)
	assert.Equal(t, "3001", src.ExternalID)
	assert.Equal(t, "https://t.me/+MemberAlready1", src.Link)
	assert.JSONEq(t, `{"k":"channel","id":3001,"h":4,"i":"MemberAlready1"}`, string(src.Peer))

	_, err = tgc.ResolveSource(ctx, "t.me/+Expired12345")
	assert.ErrorIs(t, err, ErrNotFound)
}

func joinedResult(chats ...tg.ChatClass) *tg.MessagesChatInviteJoinResultOk {
	return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{Chats: chats}}
}

func TestTelegram_JoinSource_PublicChannel(t *testing.T) {
	tests := []struct {
		name    string
		joinErr error
		status  JoinStatus
		reason  string
		wantErr func(t *testing.T, err error)
	}{
		{name: "joined", status: JoinStatusJoined},
		{name: "approval required", joinErr: tgerr.New(400, "INVITE_REQUEST_SENT"), status: JoinStatusPending, reason: "join request sent; a group admin has to approve it"},
		{name: "already a member", joinErr: tgerr.New(400, "USER_ALREADY_PARTICIPANT"), status: JoinStatusJoined},
		{name: "private channel", joinErr: tgerr.New(400, "CHANNEL_PRIVATE"), status: JoinStatusNotJoinable, reason: "CHANNEL_PRIVATE"},
		{name: "account at its channel limit", joinErr: tgerr.New(400, "CHANNELS_TOO_MUCH"), wantErr: func(t *testing.T, err error) {
			assert.ErrorContains(t, err, "CHANNELS_TOO_MUCH")
			assert.NotErrorIs(t, err, ErrNotFound)
		}},
		{name: "long flood wait", joinErr: tgerr.New(420, "FLOOD_WAIT_900"), wantErr: func(t *testing.T, err error) {
			var flood *FloodWaitError
			require.ErrorAs(t, err, &flood)
			assert.Equal(t, 900*time.Second, flood.Wait)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
				joinChannel: func(channel tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
					assert.Equal(t, &tg.InputChannel{ChannelID: 1001, AccessHash: 77}, channel)
					if tc.joinErr != nil {
						return nil, tc.joinErr
					}
					joined := gamesChannel()
					joined.Left = false
					return joinedResult(joined), nil
				},
			})
			res, err := tgc.JoinSource(context.Background(), channelRef(1001, 77, "mobilegamers"))
			if tc.wantErr != nil {
				tc.wantErr(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.status, res.Status)
			assert.Equal(t, tc.reason, res.Reason)
			if tc.joinErr == nil {
				require.NotNil(t, res.Source)
				assert.Equal(t, "1001", res.Source.ExternalID)
			}
		})
	}
}

func TestTelegram_JoinSource_ResolvesUsernameWithoutPeer(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		resolveUsername: func(req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
			assert.Equal(t, "mobilegamers", req.Username)
			return &tg.ContactsResolvedPeer{Peer: &tg.PeerChannel{ChannelID: 1001}, Chats: []tg.ChatClass{gamesChannel()}}, nil
		},
		joinChannel: func(channel tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
			assert.Equal(t, &tg.InputChannel{ChannelID: 1001, AccessHash: 77}, channel)
			return joinedResult(), nil
		},
	})
	res, err := tgc.JoinSource(context.Background(), SourceRef{ExternalID: "1001", Username: "@mobilegamers"})
	require.NoError(t, err)
	assert.Equal(t, JoinStatusJoined, res.Status)
}

func TestTelegram_JoinSource_InviteLink(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		importInvite: func(hash string) (tg.MessagesChatInviteJoinResultClass, error) {
			assert.Equal(t, "AbCdEfGh123", hash)
			return joinedResult(&tg.Channel{ID: 3001, AccessHash: 4, Title: "Private Gamers", Megagroup: true}), nil
		},
	})
	res, err := tgc.JoinSource(context.Background(), SourceRef{Link: "https://t.me/+AbCdEfGh123"})
	require.NoError(t, err)
	assert.Equal(t, JoinStatusJoined, res.Status)
	require.NotNil(t, res.Source)
	assert.Equal(t, "3001", res.Source.ExternalID)
	assert.Equal(t, "https://t.me/+AbCdEfGh123", res.Source.Link)
	assert.JSONEq(t, `{"k":"channel","id":3001,"h":4,"i":"AbCdEfGh123"}`, string(res.Source.Peer))
}

func TestTelegram_JoinSource_ChecksMembershipAfterTimeout(t *testing.T) {
	notParticipant := func() (*tg.ChannelsChannelParticipant, error) {
		return nil, tgerr.New(400, "USER_NOT_PARTICIPANT")
	}
	tests := []struct {
		name        string
		participant func() (*tg.ChannelsChannelParticipant, error)
		secondJoin  error
		joins       int
		status      JoinStatus
		wantErr     bool
	}{
		{
			name: "join landed",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipant{UserID: 42}}, nil
			},
			joins: 1, status: JoinStatusJoined,
		},
		{name: "join lost and retried", participant: notParticipant, joins: 2, status: JoinStatusJoined},
		{name: "retry times out again", participant: notParticipant, secondJoin: context.DeadlineExceeded, joins: 2, wantErr: true},
		{
			name: "recheck fails",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return nil, context.DeadlineExceeded
			},
			joins: 1, wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			joins := 0
			tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
				joinChannel: func(tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
					joins++
					if joins == 1 {
						return nil, context.DeadlineExceeded
					}
					if tc.secondJoin != nil {
						return nil, tc.secondJoin
					}
					return joinedResult(), nil
				},
				getParticipant: func(req *tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
					assert.IsType(t, &tg.InputPeerSelf{}, req.Participant)
					return tc.participant()
				},
			})
			res, err := tgc.JoinSource(context.Background(), channelRef(1001, 77, ""))
			assert.Equal(t, tc.joins, joins)
			if tc.wantErr {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.status, res.Status)
		})
	}
}

func TestTelegram_JoinSource_BasicGroupNeedsInvite(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getChats: func(id []int64) (tg.MessagesChatsClass, error) {
			assert.Equal(t, []int64{55}, id)
			return &tg.MessagesChats{Chats: []tg.ChatClass{&tg.Chat{ID: 55, Title: "Old group", Left: true}}}, nil
		},
	})
	res, err := tgc.JoinSource(context.Background(), SourceRef{ExternalID: "55", Peer: tgRef{Kind: refChat, ID: 55}.peer()})
	require.NoError(t, err)
	assert.Equal(t, JoinStatusNotJoinable, res.Status)
	assert.Contains(t, res.Reason, "invite link")
}

func TestTelegram_Membership(t *testing.T) {
	tests := []struct {
		name   string
		ref    tgRef
		api    fakeTelegramAPI
		status JoinStatus
	}{
		{
			name: "channel member",
			ref:  tgRef{Kind: refChannel, ID: 1001, Hash: 77},
			api: fakeTelegramAPI{getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipantSelf{UserID: 42}}, nil
			}},
			status: JoinStatusJoined,
		},
		{
			name: "banned",
			ref:  tgRef{Kind: refChannel, ID: 1001, Hash: 77},
			api: fakeTelegramAPI{getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipantBanned{}}, nil
			}},
			status: JoinStatusNotJoinable,
		},
		{
			name: "private channel with an invite falls back to the invite",
			ref:  tgRef{Kind: refChannel, ID: 3001, Hash: 4, Invite: "AbCdEfGh123"},
			api: fakeTelegramAPI{
				getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
					return nil, tgerr.New(400, "CHANNEL_PRIVATE")
				},
				checkInvite: func(string) (tg.ChatInviteClass, error) {
					return &tg.ChatInvite{Title: "Private Gamers", RequestNeeded: true}, nil
				},
			},
			status: JoinStatusNotJoined,
		},
		{
			name: "expired invite",
			ref:  tgRef{Kind: refInvite, Invite: "AbCdEfGh123"},
			api: fakeTelegramAPI{checkInvite: func(string) (tg.ChatInviteClass, error) {
				return nil, tgerr.New(400, "INVITE_HASH_EXPIRED")
			}},
			status: JoinStatusNotJoinable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tgc, _ := newTestTelegram(t, &tc.api)
			res, err := tgc.membership(context.Background(), tc.ref)
			require.NoError(t, err)
			assert.Equal(t, tc.status, res.Status)
		})
	}
}

func tgHistoryPage(msgs ...tg.MessageClass) *tg.MessagesChannelMessages {
	return &tg.MessagesChannelMessages{
		Messages: msgs,
		Users: []tg.UserClass{
			&tg.User{ID: 501, AccessHash: 11, Username: "player_one", FirstName: "Li", LastName: "Wei"},
			&tg.User{ID: 502, Min: true, FirstName: "Hidden"},
		},
	}
}

func chatMsg(id int, from int64, text string) *tg.Message {
	m := &tg.Message{ID: id, Date: 1760000000 + id, Message: text, PeerID: &tg.PeerChannel{ChannelID: 1001}}
	if from != 0 {
		m.FromID = &tg.PeerUser{UserID: from}
	}
	return m
}

func TestTelegram_ReadHistory_NewestPage(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, &tg.InputPeerChannel{ChannelID: 1001, AccessHash: 77}, req.Peer)
			assert.Equal(t, 0, req.OffsetID)
			assert.Equal(t, 0, req.MinID)
			assert.Equal(t, 3, req.Limit)
			reply := chatMsg(12, 502, "same, looking for a new gacha game")
			reply.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 11}
			return tgHistoryPage(
				reply,
				&tg.MessageService{ID: 11, Action: &tg.MessageActionChatJoinedByLink{}},
				chatMsg(10, 501, "bored of this game, any recommendations?"),
			), nil
		},
	})
	page, err := tgc.ReadHistory(context.Background(), channelRef(1001, 77, "mobilegamers"), HistoryRequest{Limit: 3})
	require.NoError(t, err)
	assert.False(t, page.Exhausted)
	require.Len(t, page.Messages, 2)

	assert.Equal(t, Message{
		ID:         12,
		SenderID:   "502",
		SenderName: "Hidden",
		Text:       "same, looking for a new gacha game",
		Time:       time.Date(2025, 10, 9, 8, 53, 32, 0, time.UTC),
		Link:       "https://t.me/mobilegamers/12",
		ReplyToID:  11,
	}, page.Messages[0])
	assert.Equal(t, int64(10), page.Messages[1].ID)
	assert.Equal(t, "501", page.Messages[1].SenderID)
	assert.Equal(t, "player_one", page.Messages[1].SenderUsername)
	assert.Equal(t, "Li Wei", page.Messages[1].SenderName)
}

func TestTelegram_ReadHistory_OlderPageReachesStart(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, 10, req.OffsetID)
			assert.Equal(t, 0, req.AddOffset)
			return tgHistoryPage(chatMsg(9, 501, "hi")), nil
		},
	})
	page, err := tgc.ReadHistory(context.Background(), channelRef(1001, 77, ""), HistoryRequest{OffsetID: 10, Limit: 3})
	require.NoError(t, err)
	assert.True(t, page.Exhausted)
	assert.Equal(t, "https://t.me/c/1001/9", page.Messages[0].Link)
}

func TestTelegram_ReadHistory_NewerThanMinID(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, 13, req.OffsetID)
			assert.Equal(t, -3, req.AddOffset)
			assert.Equal(t, 12, req.MinID)
			assert.Equal(t, 3, req.Limit)
			return tgHistoryPage(chatMsg(14, 501, "b"), chatMsg(13, 501, "a")), nil
		},
	})
	page, err := tgc.ReadHistory(context.Background(), channelRef(1001, 77, ""), HistoryRequest{MinID: 12, Limit: 3})
	require.NoError(t, err)
	assert.False(t, page.Exhausted, "a short page of new messages is not the start of history")
	require.Len(t, page.Messages, 2)
	assert.Equal(t, int64(14), page.Messages[0].ID)
	assert.Equal(t, int64(13), page.Messages[1].ID)
}

func TestTelegram_ReadHistory_RejectsUnreadableSources(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(*tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			return nil, tgerr.New(400, "CHANNEL_PRIVATE")
		},
	})
	ctx := context.Background()

	_, err := tgc.ReadHistory(ctx, SourceRef{Link: "https://t.me/+AbCdEfGh123"}, HistoryRequest{})
	assert.ErrorContains(t, err, "after it is joined")

	_, err = tgc.ReadHistory(ctx, channelRef(1001, 77, ""), HistoryRequest{})
	assert.ErrorContains(t, err, "CHANNEL_PRIVATE")

	_, err = tgc.ReadHistory(ctx, SourceRef{Peer: json.RawMessage(`{"k":"user","id":5}`)}, HistoryRequest{})
	assert.ErrorContains(t, err, "not a source")
}

func TestTelegram_GetUser(t *testing.T) {
	var asked []tg.InputUserClass
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(*tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			return tgHistoryPage(chatMsg(12, 502, "x"), chatMsg(10, 501, "y")), nil
		},
		getFullUser: func(id tg.InputUserClass) (*tg.UsersUserFull, error) {
			asked = append(asked, id)
			return &tg.UsersUserFull{
				FullUser: tg.UserFull{ID: 502, About: "RPG addict, spends on skins"},
				Users:    []tg.UserClass{&tg.User{ID: 502, AccessHash: 99, Username: "hidden_one", FirstName: "Hidden"}},
			}, nil
		},
	})
	ctx := context.Background()

	_, err := tgc.GetUser(ctx, "502")
	require.ErrorIs(t, err, ErrNotFound, "a user never read cannot be addressed")
	assert.Empty(t, asked)

	_, err = tgc.ReadHistory(ctx, channelRef(1001, 77, ""), HistoryRequest{})
	require.NoError(t, err)

	profile, err := tgc.GetUser(ctx, "502")
	require.NoError(t, err)
	assert.Equal(t, UserProfile{ID: "502", Username: "hidden_one", DisplayName: "Hidden", Bio: "RPG addict, spends on skins"}, profile)
	assert.Equal(t, &tg.InputUserFromMessage{Peer: &tg.InputPeerChannel{ChannelID: 1001, AccessHash: 77}, MsgID: 12, UserID: 502}, asked[0])

	_, err = tgc.GetUser(ctx, "502")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputUser{UserID: 502, AccessHash: 99}, asked[1], "the full user's hash replaces the message route")

	_, err = tgc.GetUser(ctx, "501")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputUser{UserID: 501, AccessHash: 11}, asked[2])
}

func TestTelegram_GetUser_ByUsername(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		resolveUsername: func(req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
			assert.Equal(t, "player_one", req.Username)
			return &tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: 501}, Users: []tg.UserClass{&tg.User{ID: 501, AccessHash: 11}}}, nil
		},
		getFullUser: func(id tg.InputUserClass) (*tg.UsersUserFull, error) {
			assert.Equal(t, &tg.InputUser{UserID: 501, AccessHash: 11}, id)
			return &tg.UsersUserFull{FullUser: tg.UserFull{ID: 501}, Users: []tg.UserClass{&tg.User{ID: 501, AccessHash: 11, Username: "player_one"}}}, nil
		},
	})
	profile, err := tgc.GetUser(context.Background(), "@player_one")
	require.NoError(t, err)
	assert.Equal(t, "501", profile.ID)
}

func TestTelegram_ErrorOf(t *testing.T) {
	assert.ErrorIs(t, errorOf(tgerr.New(401, "AUTH_KEY_UNREGISTERED")), ErrUnauthorized)
	assert.ErrorIs(t, errorOf(tgerr.New(400, "USER_DEACTIVATED_BAN")), ErrUnauthorized)
	assert.ErrorIs(t, errorOf(errNeedsLogin), ErrUnauthorized)
	assert.ErrorIs(t, errorOf(tgerr.New(400, "USERNAME_INVALID")), ErrNotFound)

	var flood *FloodWaitError
	require.ErrorAs(t, errorOf(tgerr.New(420, "SLOWMODE_WAIT_30")), &flood)
	assert.Equal(t, 30*time.Second, flood.Wait)
	require.ErrorAs(t, errorOf(&joinPacingError{wait: 95 * time.Second}), &flood)
	assert.Equal(t, 95*time.Second, flood.Wait)
	require.ErrorAs(t, errorOf(tgerr.New(400, "PEER_FLOOD")), &flood)
	assert.Equal(t, 24*time.Hour, flood.Wait)

	other := errorOf(tgerr.New(400, "CHAT_WRITE_FORBIDDEN"))
	assert.NotErrorIs(t, other, ErrNotFound)
	assert.ErrorContains(t, other, "CHAT_WRITE_FORBIDDEN")
	assert.NoError(t, errorOf(nil))
}

func TestTelegram_Init_Registers(t *testing.T) {
	_, ok := Lookup(PlatformTelegram)
	assert.True(t, ok)
	_, ok = LookupCredentialCheck(PlatformTelegram)
	assert.True(t, ok)
	_, ok = LookupPhoneLogin(PlatformTelegram)
	assert.True(t, ok)
}

func TestTelegramConfig_TelegramConfigFor(t *testing.T) {
	t.Setenv("TG_MAX_INLINE_FLOOD_WAIT", "2m")
	t.Setenv("TG_JOIN_WARMUP_COUNT", "3")
	cfg, err := TelegramConfigFor(&config.Config{TGAPIID: 12345, TGAPIHash: "abc", TGSessionDir: "/srv/tg"})
	require.NoError(t, err)
	assert.Equal(t, 12345, cfg.APIID)
	assert.Equal(t, "abc", cfg.APIHash)
	assert.Equal(t, "/srv/tg", cfg.SessionDir)
	assert.Equal(t, 2*time.Minute, cfg.MaxInlineFloodWait)
	assert.Equal(t, 3, cfg.JoinWarmupCount)
	assert.Equal(t, 30*time.Second, cfg.CallTimeout)
	assert.Zero(t, cfg.JoinInterval, "the connector refuses no join unless the deployer asks it to")
	assert.Zero(t, cfg.JoinWarmupInterval)
	assert.False(t, cfg.TestServer)
	require.NoError(t, cfg.Validate())

	cfg.APIID = 0
	assert.ErrorContains(t, cfg.Validate(), "TG_API_ID")
	cfg.APIID, cfg.APIHash = 1, ""
	assert.ErrorContains(t, cfg.Validate(), "TG_API_HASH")
}

type memorySessions struct{ data []byte }

func (m *memorySessions) LoadSession(ctx context.Context) ([]byte, error) { return m.data, ctx.Err() }
func (m *memorySessions) StoreSession(ctx context.Context, data []byte) error {
	m.data = data
	return ctx.Err()
}

func TestTelegram_NewTelegram_SessionStorage(t *testing.T) {
	t.Run("file session in a private directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "tg")
		_, err := NewTelegram(TelegramConfig{APIID: 1, APIHash: "x", SessionDir: dir}, Account{ID: 7})
		require.NoError(t, err)
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})
	t.Run("no store and no directory", func(t *testing.T) {
		_, err := NewTelegram(TelegramConfig{APIID: 1, APIHash: "x"}, Account{ID: 7})
		assert.ErrorContains(t, err, "TG_SESSION_DIR")
	})
	t.Run("account session store", func(t *testing.T) {
		_, err := NewTelegram(TelegramConfig{APIID: 1, APIHash: "x"}, Account{ID: 7, Session: &memorySessions{}})
		require.NoError(t, err)
	})
	t.Run("bad proxy", func(t *testing.T) {
		_, err := NewTelegram(TelegramConfig{APIID: 1, APIHash: "x"}, Account{ID: 7, Session: &memorySessions{}, Proxy: "gopher://x:70"})
		assert.ErrorContains(t, err, "unsupported proxy scheme")
	})
	t.Run("missing credentials", func(t *testing.T) {
		_, err := NewTelegram(TelegramConfig{APIHash: "x"}, Account{ID: 7, Session: &memorySessions{}})
		assert.ErrorContains(t, err, "TG_API_ID")
	})
}

func TestTelegram_SessionStore_EmptyIsNotFound(t *testing.T) {
	ctx := context.Background()
	store := &memorySessions{}
	s := sessionStore{store}

	_, err := s.LoadSession(ctx)
	assert.ErrorIs(t, err, session.ErrNotFound)

	require.NoError(t, s.StoreSession(ctx, []byte(`{"v":1}`)))
	data, err := s.LoadSession(ctx)
	require.NoError(t, err)
	assert.Equal(t, `{"v":1}`, string(data))
}

func TestTelegram_Start_GivesUpAtTheConnectTimeoutWhenTelegramIsUnreachable(t *testing.T) {
	tc := TelegramConfig{APIID: 12345, APIHash: "abc", SessionDir: t.TempDir(), ConnectTimeout: 300 * time.Millisecond, CallTimeout: 30 * time.Second}
	conn, err := NewTelegram(tc, Account{ID: 1, Proxy: silentTelegramRoute(t)})
	require.NoError(t, err)

	start := time.Now()
	err = conn.Start(t.Context())
	assert.ErrorIs(t, err, ErrUnreachable)
	assert.Less(t, time.Since(start), 5*time.Second)
}
