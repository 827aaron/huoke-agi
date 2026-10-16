package connectors

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTelegramAPI struct {
	t *testing.T

	getUsers        func(id []tg.InputUserClass) ([]tg.UserClass, error)
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

func (f *fakeTelegramAPI) UsersGetUsers(ctx context.Context, id []tg.InputUserClass) ([]tg.UserClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.getUsers == nil {
		return nil, f.unexpected("UsersGetUsers")
	}
	return f.getUsers(id)
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

func newTestTelegram(t *testing.T, api *fakeTelegramAPI) (*Telegram, *sleepRecorder) {
	t.Helper()
	api.t = t
	tgc := newTelegramWithAPI(TelegramConfig{MaxInlineFloodWait: 60 * time.Second, CallTimeout: 5 * time.Second}, api)
	rec := &sleepRecorder{}
	tgc.pacer.sleep = rec.sleep
	return tgc, rec
}

func gamesChannel() *tg.Channel {
	return &tg.Channel{ID: 1001, AccessHash: 77, Title: "Mobile Gamers", Username: "mobilegamers", Megagroup: true, ParticipantsCount: 5400, Left: true}
}

func channelSourceRef(id, hash int64, username string) SourceRef {
	return SourceRef{Platform: PlatformTelegram, ID: "1001", Ref: tgRef{Kind: refChannel, ID: id, Hash: hash, Username: username}.encode()}
}

func TestTelegram_HealthCheck_ReportsAccount(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getUsers: func(id []tg.InputUserClass) ([]tg.UserClass, error) {
			require.IsType(t, &tg.InputUserSelf{}, id[0])
			return []tg.UserClass{&tg.User{ID: 42, AccessHash: 9, Username: "aaron_test", FirstName: "Aaron"}}, nil
		},
	})
	res := tgc.HealthCheck(context.Background())
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "42", res.Account.ID)
	assert.Equal(t, "aaron_test", res.Account.Username)
	assert.Equal(t, "https://t.me/aaron_test", res.Account.Link)
}

func TestTelegram_HealthCheck_MapsFailures(t *testing.T) {
	t.Run("revoked session needs login", func(t *testing.T) {
		tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
			getUsers: func([]tg.InputUserClass) ([]tg.UserClass, error) {
				return nil, tgerr.New(401, "AUTH_KEY_UNREGISTERED")
			},
		})
		res := tgc.HealthCheck(context.Background())
		assert.Equal(t, StatusNeedsLogin, res.Status)
		assert.Equal(t, "AUTH_KEY_UNREGISTERED", res.ErrorCode)
	})
	t.Run("client not started is unavailable", func(t *testing.T) {
		tgc := newTelegramWithAPI(TelegramConfig{}, nil)
		res := tgc.HealthCheck(context.Background())
		assert.Equal(t, StatusUnavailable, res.Status)
		assert.Equal(t, "NOT_STARTED", res.ErrorCode)
	})
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
	res := tgc.SearchSources(context.Background(), SearchQuery{Query: "genshin"})
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, 2, calls)
	assert.Equal(t, []time.Duration{7 * time.Second}, rec.waits)
	assert.Len(t, res.Sources, 1)
}

func TestTelegram_Call_ReturnsLongFloodWaitAsRateLimited(t *testing.T) {
	tgc, rec := newTestTelegram(t, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			return nil, tgerr.New(420, "FLOOD_WAIT_3600")
		},
	})
	res := tgc.SearchSources(context.Background(), SearchQuery{Query: "genshin"})
	assert.Equal(t, StatusRateLimited, res.Status)
	assert.Equal(t, "FLOOD_WAIT", res.ErrorCode)
	assert.Equal(t, 3600, res.RetryAfter)
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
	res := tgc.SearchSources(context.Background(), SearchQuery{Query: "genshin"})
	assert.Equal(t, StatusRateLimited, res.Status)
	assert.Equal(t, 2, res.RetryAfter)
	assert.Equal(t, 4, calls)
	assert.Len(t, rec.waits, 3)
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
	res := tgc.SearchSources(context.Background(), SearchQuery{Query: "mobile games", Limit: 5})
	require.Equal(t, StatusOK, res.Status)
	require.Len(t, res.Sources, 2)

	group := res.Sources[0]
	assert.Equal(t, "1001", group.ID)
	assert.Equal(t, SourceGroup, group.Kind)
	assert.Equal(t, "https://t.me/mobilegamers", group.Link)
	assert.Equal(t, 5400, group.Members)
	assert.Equal(t, MembershipNotJoined, group.Membership)
	assert.Equal(t, "h=77&id=1001&k=channel&u=mobilegamers", group.Ref)

	assert.Equal(t, SourceChannel, res.Sources[1].Kind)
	assert.True(t, res.Sources[1].Broadcast)
}

func TestTelegram_SearchSources_Similar(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		recommendations: func(req *tg.ChannelsGetChannelRecommendationsRequest) (tg.MessagesChatsClass, error) {
			assert.Equal(t, &tg.InputChannel{ChannelID: 1001, AccessHash: 77}, req.Channel)
			return &tg.MessagesChats{Chats: []tg.ChatClass{&tg.Channel{ID: 2001, AccessHash: 3, Title: "RPG Fans", Megagroup: true}}}, nil
		},
	})
	ref := channelSourceRef(1001, 77, "")
	res := tgc.SearchSources(context.Background(), SearchQuery{SimilarTo: &ref})
	require.Equal(t, StatusOK, res.Status)
	require.Len(t, res.Sources, 1)
	assert.Equal(t, "2001", res.Sources[0].ID)
	assert.Empty(t, res.Sources[0].Link)
}

func TestTelegram_SearchSources_RejectsEmptyQuery(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{})
	assert.Equal(t, StatusInvalid, tgc.SearchSources(context.Background(), SearchQuery{}).Status)

	chat := SourceRef{Ref: tgRef{Kind: refChat, ID: 5}.encode()}
	assert.Equal(t, StatusInvalid, tgc.SearchSources(context.Background(), SearchQuery{SimilarTo: &chat}).Status)
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

	res := tgc.ResolveSource(ctx, "https://t.me/mobilegamers/123")
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "1001", res.Source.ID)
	assert.Equal(t, "Mobile Gamers", res.Source.Title)

	res = tgc.ResolveSource(ctx, "@someperson")
	assert.Equal(t, StatusInvalid, res.Status)
	assert.Equal(t, "NOT_A_CHAT", res.ErrorCode)

	res = tgc.ResolveSource(ctx, "t.me/nosuchgroup")
	assert.Equal(t, StatusNotFound, res.Status)
	assert.Equal(t, "USERNAME_NOT_OCCUPIED", res.ErrorCode)

	res = tgc.ResolveSource(ctx, "https://example.com/x")
	assert.Equal(t, StatusInvalid, res.Status)
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

	res := tgc.ResolveSource(ctx, "https://t.me/+AbCdEfGh123")
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "invite:AbCdEfGh123", res.Source.ID)
	assert.Equal(t, SourceInvite, res.Source.Kind)
	assert.True(t, res.Source.RequestNeeded)
	assert.Equal(t, MembershipNotJoined, res.Source.Membership)
	assert.Equal(t, "https://t.me/+AbCdEfGh123", res.Source.Link)

	res = tgc.ResolveSource(ctx, "https://t.me/joinchat/MemberAlready1")
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "3001", res.Source.ID)
	assert.Equal(t, MembershipJoined, res.Source.Membership)
	assert.Equal(t, "h=4&i=MemberAlready1&id=3001&k=channel", res.Source.Ref)

	res = tgc.ResolveSource(ctx, "t.me/+Expired12345")
	assert.Equal(t, StatusNotFound, res.Status)
	assert.Equal(t, "INVITE_HASH_EXPIRED", res.ErrorCode)
}

func TestTelegram_JoinSource_PublicChannel(t *testing.T) {
	tests := []struct {
		name       string
		joinErr    error
		status     Status
		membership Membership
		code       string
	}{
		{"joined", nil, StatusOK, MembershipJoined, ""},
		{"approval required", tgerr.New(400, "INVITE_REQUEST_SENT"), StatusOK, MembershipPendingApproval, ""},
		{"already a member", tgerr.New(400, "USER_ALREADY_PARTICIPANT"), StatusOK, MembershipJoined, ""},
		{"private channel", tgerr.New(400, "CHANNEL_PRIVATE"), StatusOK, MembershipNotJoinable, "CHANNEL_PRIVATE"},
		{"account at its channel limit", tgerr.New(400, "CHANNELS_TOO_MUCH"), StatusInvalid, MembershipNotJoined, "CHANNELS_TOO_MUCH"},
		{"long flood wait", tgerr.New(420, "FLOOD_WAIT_900"), StatusRateLimited, MembershipNotJoined, "FLOOD_WAIT"},
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
					return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{Chats: []tg.ChatClass{joined}}}, nil
				},
			})
			res := tgc.JoinSource(context.Background(), channelSourceRef(1001, 77, "mobilegamers"))
			assert.Equal(t, tc.status, res.Status)
			assert.Equal(t, tc.membership, res.Membership)
			assert.Equal(t, tc.code, res.ErrorCode)
			if tc.joinErr == nil {
				require.NotNil(t, res.Source)
				assert.Equal(t, MembershipJoined, res.Source.Membership)
			}
		})
	}
}

func TestTelegram_JoinSource_InviteLink(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		importInvite: func(hash string) (tg.MessagesChatInviteJoinResultClass, error) {
			assert.Equal(t, "AbCdEfGh123", hash)
			return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{Chats: []tg.ChatClass{
				&tg.Channel{ID: 3001, AccessHash: 4, Title: "Private Gamers", Megagroup: true},
			}}}, nil
		},
	})
	src := SourceRef{Platform: PlatformTelegram, ID: "invite:AbCdEfGh123", Ref: tgRef{Kind: refInvite, Invite: "AbCdEfGh123"}.encode()}
	res := tgc.JoinSource(context.Background(), src)
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, MembershipJoined, res.Membership)
	require.NotNil(t, res.Source)
	assert.Equal(t, "3001", res.Source.ID)
	assert.Equal(t, "h=4&i=AbCdEfGh123&id=3001&k=channel", res.Source.Ref)
}

func TestTelegram_JoinSource_ChecksMembershipAfterTimeout(t *testing.T) {
	tests := []struct {
		name        string
		participant func() (*tg.ChannelsChannelParticipant, error)
		secondJoin  error
		joins       int
		status      Status
		membership  Membership
	}{
		{
			name: "join landed",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipant{UserID: 42}}, nil
			},
			joins: 1, status: StatusOK, membership: MembershipJoined,
		},
		{
			name: "join lost and retried",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return nil, tgerr.New(400, "USER_NOT_PARTICIPANT")
			},
			joins: 2, status: StatusOK, membership: MembershipJoined,
		},
		{
			name: "retry times out again",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return nil, tgerr.New(400, "USER_NOT_PARTICIPANT")
			},
			secondJoin: context.DeadlineExceeded,
			joins:      2, status: StatusUnavailable, membership: MembershipUnknown,
		},
		{
			name: "recheck fails",
			participant: func() (*tg.ChannelsChannelParticipant, error) {
				return nil, context.DeadlineExceeded
			},
			joins: 1, status: StatusUnavailable, membership: MembershipUnknown,
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
					return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{}}, nil
				},
				getParticipant: func(req *tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
					assert.IsType(t, &tg.InputPeerSelf{}, req.Participant)
					return tc.participant()
				},
			})
			res := tgc.JoinSource(context.Background(), channelSourceRef(1001, 77, ""))
			assert.Equal(t, tc.joins, joins)
			assert.Equal(t, tc.status, res.Status)
			assert.Equal(t, tc.membership, res.Membership)
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
	res := tgc.JoinSource(context.Background(), SourceRef{Platform: PlatformTelegram, ID: "55", Ref: tgRef{Kind: refChat, ID: 55}.encode()})
	assert.Equal(t, StatusOK, res.Status)
	assert.Equal(t, MembershipNotJoinable, res.Membership)
	assert.Equal(t, "INVITE_REQUIRED", res.ErrorCode)
}

func TestTelegram_MembershipStatus(t *testing.T) {
	tests := []struct {
		name       string
		ref        tgRef
		api        fakeTelegramAPI
		membership Membership
	}{
		{
			name: "channel member",
			ref:  tgRef{Kind: refChannel, ID: 1001, Hash: 77},
			api: fakeTelegramAPI{getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipantSelf{UserID: 42}}, nil
			}},
			membership: MembershipJoined,
		},
		{
			name: "channel non-member",
			ref:  tgRef{Kind: refChannel, ID: 1001, Hash: 77},
			api: fakeTelegramAPI{getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
				return nil, tgerr.New(400, "USER_NOT_PARTICIPANT")
			}},
			membership: MembershipNotJoined,
		},
		{
			name: "banned",
			ref:  tgRef{Kind: refChannel, ID: 1001, Hash: 77},
			api: fakeTelegramAPI{getParticipant: func(*tg.ChannelsGetParticipantRequest) (*tg.ChannelsChannelParticipant, error) {
				return &tg.ChannelsChannelParticipant{Participant: &tg.ChannelParticipantBanned{}}, nil
			}},
			membership: MembershipNotJoinable,
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
			membership: MembershipNotJoined,
		},
		{
			name: "expired invite",
			ref:  tgRef{Kind: refInvite, Invite: "AbCdEfGh123"},
			api: fakeTelegramAPI{checkInvite: func(string) (tg.ChatInviteClass, error) {
				return nil, tgerr.New(400, "INVITE_HASH_EXPIRED")
			}},
			membership: MembershipNotJoinable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tgc, _ := newTestTelegram(t, &tc.api)
			res := tgc.MembershipStatus(context.Background(), SourceRef{Platform: PlatformTelegram, Ref: tc.ref.encode()})
			assert.Equal(t, StatusOK, res.Status)
			assert.Equal(t, tc.membership, res.Membership)
		})
	}
}

func historyPage(msgs ...tg.MessageClass) *tg.MessagesChannelMessages {
	return &tg.MessagesChannelMessages{
		Messages: msgs,
		Users: []tg.UserClass{
			&tg.User{ID: 501, AccessHash: 11, Username: "player_one", FirstName: "Li"},
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

func TestTelegram_ReadSource_FirstPage(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, &tg.InputPeerChannel{ChannelID: 1001, AccessHash: 77}, req.Peer)
			assert.Equal(t, 0, req.OffsetID)
			assert.Equal(t, 3, req.Limit)
			reply := chatMsg(12, 502, "same, looking for a new gacha game")
			reply.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 11}
			return historyPage(
				reply,
				&tg.MessageService{ID: 11, Action: &tg.MessageActionChatJoinedByLink{}},
				chatMsg(10, 501, "bored of this game, any recommendations?"),
			), nil
		},
	})
	res := tgc.ReadSource(context.Background(), ReadRequest{Source: channelSourceRef(1001, 77, "mobilegamers"), Limit: 3})
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "12", res.Checkpoint)
	assert.Equal(t, "10", res.Before)
	require.Len(t, res.Messages, 2)

	first := res.Messages[0]
	assert.Equal(t, "12", first.ID)
	assert.Equal(t, "1001", first.SourceID)
	assert.Equal(t, "https://t.me/mobilegamers/12", first.Link)
	assert.Equal(t, "11", first.ReplyToID)
	assert.Equal(t, time.Date(2025, 10, 9, 8, 53, 32, 0, time.UTC), first.Date)
	require.NotNil(t, first.Sender)
	assert.Equal(t, "502", first.Sender.ID)
	assert.Equal(t, "fh=77&fid=1001&fk=channel&fm=12&id=502&k=user", first.Sender.Ref)

	second := res.Messages[1]
	assert.Equal(t, "player_one", res.Users[second.Sender.ID].Username)
	assert.Equal(t, "h=11&id=501&k=user", second.Sender.Ref)
}

func TestTelegram_ReadSource_OlderPageAndEnd(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, 10, req.OffsetID)
			return historyPage(chatMsg(9, 501, "hi")), nil
		},
	})
	res := tgc.ReadSource(context.Background(), ReadRequest{Source: channelSourceRef(1001, 77, ""), Before: "10", Limit: 3})
	require.Equal(t, StatusOK, res.Status)
	assert.Empty(t, res.Before)
	assert.Empty(t, res.Checkpoint)
	assert.Equal(t, "https://t.me/c/1001/9", res.Messages[0].Link)
}

func TestTelegram_ReadSource_RejectsUnreadableSources(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(*tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			return nil, tgerr.New(400, "CHANNEL_PRIVATE")
		},
	})
	ctx := context.Background()

	res := tgc.ReadSource(ctx, ReadRequest{Source: SourceRef{Ref: tgRef{Kind: refInvite, Invite: "AbCdEfGh123"}.encode()}})
	assert.Equal(t, StatusForbidden, res.Status)
	assert.Equal(t, "JOIN_REQUIRED", res.ErrorCode)

	res = tgc.ReadSource(ctx, ReadRequest{Source: channelSourceRef(1001, 77, "")})
	assert.Equal(t, StatusForbidden, res.Status)
	assert.Equal(t, "CHANNEL_PRIVATE", res.ErrorCode)

	res = tgc.ReadSource(ctx, ReadRequest{Source: channelSourceRef(1001, 77, ""), Before: "x"})
	assert.Equal(t, StatusInvalid, res.Status)
}

func TestTelegram_SyncSource_FromCheckpoint(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, 13, req.OffsetID)
			assert.Equal(t, -2, req.AddOffset)
			assert.Equal(t, 12, req.MinID)
			assert.Equal(t, 2, req.Limit)
			return historyPage(chatMsg(14, 501, "b"), chatMsg(13, 501, "a")), nil
		},
	})
	res := tgc.SyncSource(context.Background(), SyncRequest{Source: channelSourceRef(1001, 77, ""), Checkpoint: "12", Limit: 2})
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "14", res.Checkpoint)
	assert.True(t, res.HasMore)
	require.Len(t, res.Messages, 2)
	assert.Equal(t, "13", res.Messages[0].ID)
	assert.Equal(t, "14", res.Messages[1].ID)
}

func TestTelegram_SyncSource_NothingNewKeepsCheckpoint(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(*tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			return &tg.MessagesChannelMessages{}, nil
		},
	})
	res := tgc.SyncSource(context.Background(), SyncRequest{Source: channelSourceRef(1001, 77, ""), Checkpoint: "40"})
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "40", res.Checkpoint)
	assert.False(t, res.HasMore)
	assert.Empty(t, res.Messages)
}

func TestTelegram_SyncSource_WithoutCheckpointReadsNewest(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getHistory: func(req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
			assert.Equal(t, 0, req.OffsetID)
			assert.Equal(t, 0, req.AddOffset)
			assert.Equal(t, 100, req.Limit)
			return historyPage(chatMsg(8, 501, "b"), chatMsg(7, 0, "channel post")), nil
		},
	})
	res := tgc.SyncSource(context.Background(), SyncRequest{Source: channelSourceRef(1001, 77, "")})
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "8", res.Checkpoint)
	require.Len(t, res.Messages, 2)
	assert.Equal(t, "7", res.Messages[0].ID)
	assert.Nil(t, res.Messages[0].Sender)
}

func TestTelegram_GetUser(t *testing.T) {
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		getFullUser: func(id tg.InputUserClass) (*tg.UsersUserFull, error) {
			assert.Equal(t, &tg.InputUserFromMessage{
				Peer:   &tg.InputPeerChannel{ChannelID: 1001, AccessHash: 77},
				MsgID:  12,
				UserID: 502,
			}, id)
			return &tg.UsersUserFull{
				FullUser: tg.UserFull{ID: 502, About: "RPG addict, spends on skins"},
				Users:    []tg.UserClass{&tg.User{ID: 502, AccessHash: 99, Username: "hidden_one"}},
			}, nil
		},
	})
	ref := UserRef{Platform: PlatformTelegram, ID: "502", Ref: "fk=channel&fh=77&fid=1001&fm=12&id=502&k=user"}
	res := tgc.GetUser(context.Background(), ref)
	require.Equal(t, StatusOK, res.Status)
	assert.Equal(t, "RPG addict, spends on skins", res.User.About)
	assert.Equal(t, "hidden_one", res.User.Username)
	assert.Equal(t, "h=99&id=502&k=user", res.User.Ref)

	res = tgc.GetUser(context.Background(), UserRef{Ref: tgRef{Kind: refChannel, ID: 1}.encode()})
	assert.Equal(t, StatusInvalid, res.Status)
}

func TestTelegramConfig_Validate(t *testing.T) {
	valid := TelegramConfig{APIID: 12345, APIHash: "abc", SessionDir: "/data/tg"}
	require.NoError(t, valid.Validate())

	noID := valid
	noID.APIID = 0
	assert.ErrorContains(t, noID.Validate(), "TG_API_ID")

	noHash := valid
	noHash.APIHash = ""
	assert.ErrorContains(t, noHash.Validate(), "TG_API_HASH")

	noPath := valid
	noPath.SessionDir = ""
	assert.ErrorContains(t, noPath.Validate(), "TG_SESSION_DIR")
}

func TestTelegramConfig_FromEnv(t *testing.T) {
	t.Setenv("TG_API_ID", "12345")
	t.Setenv("TG_API_HASH", "abc")
	t.Setenv("TG_SESSION_DIR", "/srv/tg")
	t.Setenv("TG_MAX_INLINE_FLOOD_WAIT", "2m")
	cfg, err := TelegramConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 12345, cfg.APIID)
	assert.Equal(t, "/srv/tg", cfg.SessionDir)
	assert.Equal(t, 2*time.Minute, cfg.MaxInlineFloodWait)
	assert.Equal(t, 30*time.Second, cfg.CallTimeout)
	assert.False(t, cfg.TestServer)
}

func TestTelegram_NewTelegram_CreatesPrivateSessionDirectory(t *testing.T) {
	dir := t.TempDir() + "/nested/tg"
	tgc, err := NewTelegram(TelegramConfig{APIID: 1, APIHash: "x", SessionDir: dir})
	require.NoError(t, err)
	assert.Equal(t, dir+"/session.json", tgc.cfg.SessionPath)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	_, err = NewTelegram(TelegramConfig{APIHash: "x", SessionDir: dir})
	assert.ErrorContains(t, err, "TG_API_ID")
}
