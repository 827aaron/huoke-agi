package connectors

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newPacedTelegram wires a fake clock whose sleeps advance time, and a jitter
// that always picks the upper bound.
func newPacedTelegram(t *testing.T, cfg TelegramConfig, api *fakeTelegramAPI) (*Telegram, *fakeClock, *[]time.Duration) {
	t.Helper()
	api.t = t
	if cfg.MaxInlineFloodWait == 0 {
		cfg.MaxInlineFloodWait = 60 * time.Second
	}
	tgc := newTelegramWithAPI(cfg, api)
	clock := &fakeClock{t: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	var waits []time.Duration
	tgc.pacer.now = clock.now
	tgc.pacer.jitter = func(_, hi time.Duration) time.Duration { return hi }
	tgc.pacer.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		clock.advance(d)
		return ctx.Err()
	}
	return tgc, clock, &waits
}

func searchOK(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
	return &tg.ContactsFound{}, nil
}

func TestTelegramPacing_Acquire_SpacesCallsByRandomGap(t *testing.T) {
	tgc, clock, waits := newPacedTelegram(t, TelegramConfig{MinCallInterval: time.Second, MaxCallInterval: 3 * time.Second}, &fakeTelegramAPI{search: searchOK})
	ctx := context.Background()

	require.True(t, tgc.SearchSources(ctx, SearchQuery{Query: "a"}).OK())
	assert.Empty(t, *waits)

	clock.advance(time.Second)
	require.True(t, tgc.SearchSources(ctx, SearchQuery{Query: "b"}).OK())
	assert.Equal(t, []time.Duration{2 * time.Second}, *waits)

	clock.advance(10 * time.Second)
	require.True(t, tgc.SearchSources(ctx, SearchQuery{Query: "c"}).OK())
	assert.Len(t, *waits, 1)
}

func TestTelegramPacing_Acquire_SerializesCalls(t *testing.T) {
	var inFlight, peak atomic.Int32
	api := &fakeTelegramAPI{search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return &tg.ContactsFound{}, nil
	}}
	api.t = t
	tgc := newTelegramWithAPI(TelegramConfig{MaxConcurrency: 1}, api)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.True(t, tgc.SearchSources(context.Background(), SearchQuery{Query: "x"}).OK())
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), peak.Load())
}

func TestTelegramPacing_Observe_LongFloodWaitClosesTheMethod(t *testing.T) {
	calls := 0
	tgc, clock, _ := newPacedTelegram(t, TelegramConfig{}, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			if calls == 1 {
				return nil, tgerr.New(420, "FLOOD_WAIT_600")
			}
			return &tg.ContactsFound{}, nil
		},
		resolveUsername: func(*tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
			return &tg.ContactsResolvedPeer{Peer: &tg.PeerChannel{ChannelID: 1001}, Chats: []tg.ChatClass{gamesChannel()}}, nil
		},
	})
	ctx := context.Background()

	assert.Equal(t, 600, tgc.SearchSources(ctx, SearchQuery{Query: "a"}).RetryAfter)

	clock.advance(100 * time.Second)
	res := tgc.SearchSources(ctx, SearchQuery{Query: "a"})
	assert.Equal(t, StatusRateLimited, res.Status)
	assert.Equal(t, 500, res.RetryAfter)
	assert.Equal(t, 1, calls)

	assert.True(t, tgc.ResolveSource(ctx, "@mobilegamers").OK(), "another method stays open")

	clock.advance(500 * time.Second)
	assert.True(t, tgc.SearchSources(ctx, SearchQuery{Query: "a"}).OK())
	assert.Equal(t, 2, calls)
}

func TestTelegramPacing_Observe_StopsAfterRevokedSession(t *testing.T) {
	calls := 0
	tgc, _, _ := newPacedTelegram(t, TelegramConfig{}, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			return nil, tgerr.New(401, "SESSION_REVOKED")
		},
		getUsers: func([]tg.InputUserClass) ([]tg.UserClass, error) {
			calls++
			return []tg.UserClass{&tg.User{ID: 42}}, nil
		},
	})
	ctx := context.Background()

	assert.Equal(t, StatusNeedsLogin, tgc.SearchSources(ctx, SearchQuery{Query: "a"}).Status)
	res := tgc.HealthCheck(ctx)
	assert.Equal(t, StatusNeedsLogin, res.Status)
	assert.Equal(t, "NEEDS_LOGIN", res.ErrorCode)
	assert.Equal(t, 1, calls)

	tgc.pacer.resetLogin()
	assert.True(t, tgc.HealthCheck(ctx).OK())
}

func joinedOK(tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
	return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{}}, nil
}

func TestTelegramPacing_BeforeJoin_WarmsUpThenSpeedsUp(t *testing.T) {
	cfg := TelegramConfig{JoinInterval: 20 * time.Second, JoinWarmupInterval: 40 * time.Second, JoinWarmupCount: 2}
	tgc, _, waits := newPacedTelegram(t, cfg, &fakeTelegramAPI{joinChannel: joinedOK})
	ctx := context.Background()
	src := channelSourceRef(1001, 77, "")

	for range 4 {
		require.Equal(t, MembershipJoined, tgc.JoinSource(ctx, src).Membership)
	}
	// Gaps are the interval plus the maximal 50% jitter: two warm-up gaps, then
	// the settled one.
	assert.Equal(t, []time.Duration{60 * time.Second, 30 * time.Second, 30 * time.Second}, *waits)
	assert.Equal(t, 4, tgc.pacer.joins.Count)
}

func TestTelegramPacing_BeforeJoin_LongGapIsReturnedNotSlept(t *testing.T) {
	cfg := TelegramConfig{JoinInterval: 90 * time.Second, MaxInlineFloodWait: 30 * time.Second}
	joins := 0
	tgc, _, waits := newPacedTelegram(t, cfg, &fakeTelegramAPI{joinChannel: func(c tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
		joins++
		return joinedOK(c)
	}})
	ctx := context.Background()
	src := channelSourceRef(1001, 77, "")

	require.Equal(t, MembershipJoined, tgc.JoinSource(ctx, src).Membership)
	res := tgc.JoinSource(ctx, src)
	assert.Equal(t, StatusRateLimited, res.Status)
	assert.Equal(t, "JOIN_PACING", res.ErrorCode)
	assert.Equal(t, 135, res.RetryAfter)
	assert.Equal(t, MembershipNotJoined, res.Membership)
	assert.Equal(t, 1, joins)
	assert.Empty(t, *waits)
}

func TestTelegramPacing_Joined_PersistsWarmupNextToSession(t *testing.T) {
	session := filepath.Join(t.TempDir(), "session.json")
	cfg := TelegramConfig{SessionPath: session, JoinWarmupCount: 5}
	tgc, _, _ := newPacedTelegram(t, cfg, &fakeTelegramAPI{joinChannel: joinedOK})

	require.Equal(t, MembershipJoined, tgc.JoinSource(context.Background(), channelSourceRef(1001, 77, "")).Membership)

	info, err := os.Stat(session + ".joins.json")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	reloaded := newPacer(cfg, sleepCtx)
	assert.Equal(t, 1, reloaded.joins.Count)
	assert.False(t, reloaded.joins.Last.IsZero())
}

func TestTelegram_SetObserver_ReportsAccountHealth(t *testing.T) {
	calls := 0
	tgc, _, _ := newPacedTelegram(t, TelegramConfig{}, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			if calls == 1 {
				return &tg.ContactsFound{}, nil
			}
			return nil, tgerr.New(420, "FLOOD_WAIT_300")
		},
	})
	var events []AccountEvent
	tgc.SetObserver(func(ev AccountEvent) { events = append(events, ev) })
	ctx := context.Background()

	tgc.SearchSources(ctx, SearchQuery{Query: "a"})
	tgc.SearchSources(ctx, SearchQuery{Query: "a"})
	tgc.SearchSources(ctx, SearchQuery{Query: "a"})

	require.Len(t, events, 3)
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, AccountEvent{Platform: PlatformTelegram, Method: "contacts.search", At: start, Outcome: Outcome{Status: StatusOK}}, events[0])
	assert.Equal(t, StatusRateLimited, events[1].Status)
	assert.Equal(t, start.Add(300*time.Second), events[1].RateLimitUntil)
	assert.Equal(t, StatusRateLimited, events[2].Status, "refused locally while the method cools down")
	assert.Equal(t, 300, events[2].RetryAfter)
	assert.Equal(t, 2, calls)
}
