package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/gotd/td/tgerr"
)

// pacer spaces the account's requests the way a person's client would. It is
// a default meant to keep the account out of Telegram's anti-spam, never a
// quota: every knob can be set to zero.
type pacer struct {
	cfg   TelegramConfig
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	// jitter returns a duration in [lo, hi].
	jitter func(lo, hi time.Duration) time.Duration

	slots chan struct{}

	mu         sync.Mutex
	lastCall   time.Time
	cooldown   map[string]time.Time
	needsLogin bool
	joins      joinState
}

type joinState struct {
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

var errNeedsLogin = errors.New("telegram demands a new login; calls are stopped until it is done")

func newPacer(cfg TelegramConfig, sleep func(ctx context.Context, d time.Duration) error) *pacer {
	p := &pacer{
		cfg:      cfg,
		now:      time.Now,
		sleep:    sleep,
		jitter:   randomBetween,
		slots:    make(chan struct{}, max(cfg.MaxConcurrency, 1)),
		cooldown: map[string]time.Time{},
	}
	p.joins = p.loadJoins()
	return p
}

func randomBetween(lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + rand.N(hi-lo+1)
}

// acquire takes a call slot and waits out the random gap since the previous
// call. The slot is held through the call and any flood wait inside it, so a
// waiting method is not overtaken by another request from the same account.
func (p *pacer) acquire(ctx context.Context, method string) (release func(), err error) {
	p.mu.Lock()
	stopped := p.needsLogin
	until := p.cooldown[method]
	p.mu.Unlock()
	if stopped {
		return nil, errNeedsLogin
	}
	if wait := until.Sub(p.now()); wait > 0 {
		return nil, floodWaitError(wait)
	}

	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release = func() { <-p.slots }

	p.mu.Lock()
	gap := p.jitter(p.cfg.MinCallInterval, p.cfg.MaxCallInterval)
	wait := p.lastCall.Add(gap).Sub(p.now())
	p.mu.Unlock()
	if wait > 0 {
		if err := p.sleep(ctx, wait); err != nil {
			release()
			return nil, err
		}
	}
	p.mu.Lock()
	p.lastCall = p.now()
	p.mu.Unlock()
	return release, nil
}

// observe records what a finished call taught about the account: a flood wait
// too long to sleep through closes the method until it ends, and a revoked
// session stops every call.
func (p *pacer) observe(method string, err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if wait, ok := tgerr.AsFloodWait(err); ok {
		p.cooldown[method] = p.now().Add(wait)
		return
	}
	if rpc, ok := tgerr.As(err); ok && (rpc.IsOneOf(needsLoginTypes...) || rpc.Code == 401) {
		p.needsLogin = true
	}
}

func (p *pacer) resetLogin() {
	p.mu.Lock()
	p.needsLogin = false
	p.mu.Unlock()
}

// joinInterval starts slow on an account that has joined few chats through
// this connector and settles at JoinInterval after JoinWarmupCount joins.
func (p *pacer) joinInterval(count int) time.Duration {
	if count < p.cfg.JoinWarmupCount && p.cfg.JoinWarmupInterval > p.cfg.JoinInterval {
		return p.cfg.JoinWarmupInterval
	}
	return p.cfg.JoinInterval
}

// beforeJoin waits for the join gap when it is short enough to sleep through;
// a longer gap comes back as a JOIN_PACING rate limit so the caller is not
// blocked.
func (p *pacer) beforeJoin(ctx context.Context) error {
	p.mu.Lock()
	j := p.joins
	interval := p.joinInterval(j.Count)
	p.mu.Unlock()
	if j.Last.IsZero() || interval <= 0 {
		return nil
	}
	gap := p.jitter(interval, interval+interval/2)
	wait := j.Last.Add(gap).Sub(p.now())
	if wait <= 0 {
		return nil
	}
	if wait > p.cfg.MaxInlineFloodWait {
		return &joinPacingError{wait: wait}
	}
	return p.sleep(ctx, wait)
}

func (p *pacer) joined() {
	p.mu.Lock()
	p.joins.Count++
	p.joins.Last = p.now()
	state := p.joins
	p.mu.Unlock()
	p.saveJoins(state)
}

func (p *pacer) joinStatePath() string {
	if p.cfg.SessionPath == "" {
		return ""
	}
	return p.cfg.SessionPath + ".joins.json"
}

func (p *pacer) loadJoins() joinState {
	var s joinState
	path := p.joinStatePath()
	if path == "" {
		return s
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// saveJoins is best effort: losing the count only restarts the warm-up.
func (p *pacer) saveJoins(s joinState) {
	path := p.joinStatePath()
	if path == "" {
		return
	}
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

type joinPacingError struct{ wait time.Duration }

func (e *joinPacingError) Error() string {
	return fmt.Sprintf("next join is paced in %s", e.wait.Round(time.Second))
}

func floodWaitError(wait time.Duration) error {
	secs := int((wait + time.Second - 1) / time.Second)
	return tgerr.New(420, fmt.Sprintf("%s_%d", tgerr.ErrFloodWait, secs))
}
