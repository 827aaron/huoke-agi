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
	tgc := newTelegramWithAPI(cfg, Account{ID: 1}, api)
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

func requireSearchOK(t *testing.T, tgc *Telegram, query string) {
	t.Helper()
	_, err := tgc.SearchSources(context.Background(), query, 0)
	require.NoError(t, err)
}

func floodWait(t *testing.T, err error) time.Duration {
	t.Helper()
	var flood *FloodWaitError
	require.ErrorAs(t, err, &flood)
	return flood.Wait
}

func TestTelegramPacing_Acquire_SpacesCallsByRandomGap(t *testing.T) {
	tgc, clock, waits := newPacedTelegram(t, TelegramConfig{MinCallInterval: time.Second, MaxCallInterval: 3 * time.Second}, &fakeTelegramAPI{search: searchOK})

	requireSearchOK(t, tgc, "a")
	assert.Empty(t, *waits)

	clock.advance(time.Second)
	requireSearchOK(t, tgc, "b")
	assert.Equal(t, []time.Duration{2 * time.Second}, *waits)

	clock.advance(10 * time.Second)
	requireSearchOK(t, tgc, "c")
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
	tgc := newTelegramWithAPI(TelegramConfig{MaxConcurrency: 1}, Account{}, api)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tgc.SearchSources(context.Background(), "x", 0)
			assert.NoError(t, err)
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

	_, err := tgc.SearchSources(ctx, "a", 0)
	assert.Equal(t, 600*time.Second, floodWait(t, err))

	clock.advance(100 * time.Second)
	_, err = tgc.SearchSources(ctx, "a", 0)
	assert.Equal(t, 500*time.Second, floodWait(t, err))
	assert.Equal(t, 1, calls)

	_, err = tgc.ResolveSource(ctx, "@mobilegamers")
	assert.NoError(t, err, "another method stays open")

	clock.advance(500 * time.Second)
	requireSearchOK(t, tgc, "a")
	assert.Equal(t, 2, calls)
}

func TestTelegramPacing_Observe_StopsAfterRevokedSession(t *testing.T) {
	calls := 0
	tgc, _, _ := newPacedTelegram(t, TelegramConfig{}, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			return nil, tgerr.New(401, "SESSION_REVOKED")
		},
		resolveUsername: func(*tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
			calls++
			return &tg.ContactsResolvedPeer{Peer: &tg.PeerChannel{ChannelID: 1001}, Chats: []tg.ChatClass{gamesChannel()}}, nil
		},
	})
	ctx := context.Background()

	_, err := tgc.SearchSources(ctx, "a", 0)
	assert.ErrorIs(t, err, ErrUnauthorized)
	_, err = tgc.ResolveSource(ctx, "@mobilegamers")
	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.Equal(t, 1, calls)

	tgc.pacer.resetLogin()
	_, err = tgc.ResolveSource(ctx, "@mobilegamers")
	assert.NoError(t, err)
}

func joinedOK(tg.InputChannelClass) (tg.MessagesChatInviteJoinResultClass, error) {
	return &tg.MessagesChatInviteJoinResultOk{Updates: &tg.Updates{}}, nil
}

func TestTelegramPacing_BeforeJoin_WarmsUpThenSpeedsUp(t *testing.T) {
	cfg := TelegramConfig{JoinInterval: 20 * time.Second, JoinWarmupInterval: 40 * time.Second, JoinWarmupCount: 2}
	tgc, _, waits := newPacedTelegram(t, cfg, &fakeTelegramAPI{joinChannel: joinedOK})
	ctx := context.Background()
	src := channelRef(1001, 77, "")

	for range 4 {
		res, err := tgc.JoinSource(ctx, src)
		require.NoError(t, err)
		require.Equal(t, JoinStatusJoined, res.Status)
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
	src := channelRef(1001, 77, "")

	_, err := tgc.JoinSource(ctx, src)
	require.NoError(t, err)
	_, err = tgc.JoinSource(ctx, src)
	assert.Equal(t, 135*time.Second, floodWait(t, err))
	assert.Equal(t, 1, joins)
	assert.Empty(t, *waits)
}

func TestTelegramPacing_Joined_PersistsWarmupInSessionDir(t *testing.T) {
	dir := t.TempDir()
	cfg := TelegramConfig{SessionDir: dir, JoinWarmupCount: 5}
	tgc, _, _ := newPacedTelegram(t, cfg, &fakeTelegramAPI{joinChannel: joinedOK})

	_, err := tgc.JoinSource(context.Background(), channelRef(1001, 77, ""))
	require.NoError(t, err)

	path := filepath.Join(dir, "account-1.joins.json")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	reloaded := newPacer(cfg, path, sleepCtx)
	assert.Equal(t, 1, reloaded.joins.Count)
	assert.False(t, reloaded.joins.Last.IsZero())
}
