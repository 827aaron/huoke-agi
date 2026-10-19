package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/tools/searchers"
)

// keyedEngines are the engines whose API key may be a comma-separated list.
var keyedEngines = map[database.SearchengineType]func(c *config.Config) *string{
	EngineGoogle:     func(c *config.Config) *string { return &c.GoogleAPIKey },
	EngineTavily:     func(c *config.Config) *string { return &c.TavilyAPIKey },
	EngineFirecrawl:  func(c *config.Config) *string { return &c.FirecrawlAPIKey },
	EngineTraversaal: func(c *config.Config) *string { return &c.TraversaalAPIKey },
	EnginePerplexity: func(c *config.Config) *string { return &c.PerplexityAPIKey },
	EngineBrave:      func(c *config.Config) *string { return &c.BraveAPIKey },
}

func newGoogle(c *config.Config) searchers.Searcher     { return searchers.NewGoogle(c) }
func newTraversaal(c *config.Config) searchers.Searcher { return searchers.NewTraversaal(c) }
func newBrave(c *config.Config) searchers.Searcher      { return searchers.NewBrave(c) }

func newFirecrawl(sum searchers.SummarizeHandler) func(*config.Config) searchers.Searcher {
	return func(c *config.Config) searchers.Searcher { return searchers.NewFirecrawl(c, sum) }
}

// defaultKeyRest is how long a key rests after a quota error that names no retry time;
// most search APIs reset their quotas daily.
const defaultKeyRest = 24 * time.Hour

// spentKeys is process-wide, so every flow skips a key another flow has used up.
var spentKeys = struct {
	sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

func spentKeyID(engine database.SearchengineType, key string) string {
	sum := sha256.Sum256([]byte(key))
	return string(engine) + ":" + hex.EncodeToString(sum[:8])
}

// withKeyRotation builds engine once per key when its key is a list, and otherwise
// exactly as build would.
func withKeyRotation(
	cfg *config.Config,
	engine database.SearchengineType,
	build func(*config.Config) searchers.Searcher,
) searchers.Searcher {
	target, ok := keyedEngines[engine]
	if !ok {
		return build(cfg)
	}
	keys := config.SplitList(*target(cfg))
	if len(keys) < 2 {
		one := *cfg
		*target(&one) = strings.Join(keys, "")
		return build(&one)
	}
	r := &keyRotation{engine: engine}
	for _, key := range keys {
		one := *cfg
		*target(&one) = key
		r.keys = append(r.keys, spentKeyID(engine, key))
		r.engines = append(r.engines, build(&one))
	}
	return r
}

// keyRotation sends a query with the first key that has not run out of quota and moves
// to the next key when the provider answers with a quota or rate-limit error. Only when
// every key is spent does the error reach the orchestrator, which then falls back to
// another engine as it would for a single key.
type keyRotation struct {
	engine  database.SearchengineType
	keys    []string
	engines []searchers.Searcher
}

func (r *keyRotation) Engine() database.SearchengineType { return r.engine }

func (r *keyRotation) IsAvailable() bool { return r.engines[0].IsAvailable() }

func (r *keyRotation) Handle(ctx context.Context, req searchers.Request) (string, error) {
	var lastErr error
	for i, e := range r.engines {
		if keySpent(r.keys[i]) {
			continue
		}
		out, err := e.Handle(ctx, req)
		if err == nil || !isQuotaError(err) {
			return out, err
		}
		markKeySpent(r.keys[i], err)
		lastErr = err
	}
	if lastErr == nil {
		return "", searchers.Fatal(fmt.Errorf("every %s API key has run out of quota", r.engine))
	}
	return "", lastErr
}

func keySpent(id string) bool {
	spentKeys.Lock()
	defer spentKeys.Unlock()
	until, ok := spentKeys.until[id]
	if ok && time.Now().After(until) {
		delete(spentKeys.until, id)
		return false
	}
	return ok
}

func markKeySpent(id string, err error) {
	rest := defaultKeyRest
	var retry *searchers.RetryableError
	if errors.As(err, &retry) && retry.RetryAfter > 0 {
		rest = retry.RetryAfter
	}
	spentKeys.Lock()
	defer spentKeys.Unlock()
	spentKeys.until[id] = time.Now().Add(rest)
}

// quotaMarkers are the texts the engines in searchers put in a quota or rate-limit
// error; the engines classify by status code, not by key, so the text is all there is.
var quotaMarkers = []string{
	"HTTP 429",                              // google, brave
	"there are requesting too many results", // tavily, firecrawl, perplexity on 429
	"unexpected status code: 429",           // traversaal
	"insufficient credits",                  // firecrawl on 402
	"unexpected status code: 432",           // tavily, plan limit
	"unexpected status code: 433",           // tavily, pay-as-you-go limit
}

func isQuotaError(err error) bool {
	msg := err.Error()
	for _, marker := range quotaMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
