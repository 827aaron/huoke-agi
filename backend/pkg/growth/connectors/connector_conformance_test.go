package connectors

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cross-platform behaviour of the Connector contract. It lives apart from
// connector_test.go because it builds every platform's connector and fake.

type conformanceCase struct {
	platform Platform
	// build returns a connector whose platform fails every request with the
	// given failure and a counter of requests that reached the platform.
	build func(t *testing.T, f failure) (Connector, func() int)
}

type failure string

const (
	failRateLimited failure = "rate_limited"
	failNeedsLogin  failure = "needs_login"
	failNone        failure = "none"
)

func telegramConformance(t *testing.T, f failure) (Connector, func() int) {
	calls := 0
	tgc, _ := newTestTelegram(t, &fakeTelegramAPI{
		search: func(*tg.ContactsSearchRequest) (*tg.ContactsFound, error) {
			calls++
			switch f {
			case failRateLimited:
				return nil, tgerr.New(420, "FLOOD_WAIT_3600")
			case failNeedsLogin:
				return nil, tgerr.New(401, "AUTH_KEY_UNREGISTERED")
			}
			return &tg.ContactsFound{}, nil
		},
	})
	return tgc, func() int { return calls }
}

func browserConformance(newConnector func(*testing.T, *fakeBrowser) Connector) func(*testing.T, failure) (Connector, func() int) {
	return func(t *testing.T, f failure) (Connector, func() int) {
		fb := newFakeBrowser(t)
		switch f {
		case failRateLimited:
			fb.fail["navigate"] = fakeFailure{status: 429, body: `{"error":{"code":"rate_limited","message":"slow down","retry_after_sec":3600}}`}
		case failNeedsLogin:
			fb.fail["navigate"] = fakeFailure{status: 423, body: `{"error":{"code":"needs_login","message":"redirected to /checkpoint/","account_state":"needs_login"}}`}
		}
		return newConnector(t, fb), func() int { return len(fb.callLog()) }
	}
}

func youtubeConformance(t *testing.T, f failure) (Connector, func() int) {
	calls := 0
	api := newFakeYouTubeAPI(t, func(string, url.Values) (int, string, string) {
		calls++
		if f == failRateLimited {
			return 403, "", `{"error":{"code":403,"message":"quota","errors":[{"reason":"quotaExceeded","domain":"youtube.quota"}]}}`
		}
		return 200, "", `{"items":[]}`
	})
	return api.client(t, YouTubeConfig{}), func() int { return calls }
}

func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{PlatformTelegram, telegramConformance},
		{PlatformFacebook, browserConformance(func(t *testing.T, fb *fakeBrowser) Connector { return newTestFacebook(t, fb) })},
		{PlatformInstagram, browserConformance(func(t *testing.T, fb *fakeBrowser) Connector { return newTestInstagram(t, fb) })},
		{PlatformYouTube, youtubeConformance},
	}
}

type observable interface{ SetObserver(AccountObserver) }

func observe(t *testing.T, c Connector) *[]AccountEvent {
	t.Helper()
	o, ok := c.(observable)
	require.True(t, ok, "%T has no SetObserver", c)
	events := &[]AccountEvent{}
	o.SetObserver(func(ev AccountEvent) { *events = append(*events, ev) })
	return events
}

func TestConnector_Platform_MatchesTheConstructedPlatform(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failNone)
			assert.Equal(t, tc.platform, c.Platform())
		})
	}
}

func TestConnector_SearchSources_RateLimitedCarriesTheWaitAndAnEvent(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failRateLimited)
			events := observe(t, c)

			res := c.SearchSources(context.Background(), SearchQuery{Query: "genshin impact"})

			assert.Equal(t, StatusRateLimited, res.Status)
			assert.Positive(t, res.RetryAfter, "a rate limit must tell the caller how long to wait")
			require.Len(t, *events, 1)
			assert.Equal(t, StatusRateLimited, (*events)[0].Status)
			assert.Equal(t, tc.platform, (*events)[0].Platform)
			assert.False(t, (*events)[0].RateLimitUntil.IsZero(), "the account row needs a rate_limit_until")
		})
	}
}

func TestConnector_SearchSources_NeedsLoginStaysNeedsLogin(t *testing.T) {
	for _, tc := range conformanceCases() {
		if tc.platform == PlatformYouTube {
			continue // an API key has no login to lose
		}
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failNeedsLogin)
			events := observe(t, c)
			ctx := context.Background()

			first := c.SearchSources(ctx, SearchQuery{Query: "genshin impact"})
			second := c.SearchSources(ctx, SearchQuery{Query: "genshin impact"})

			assert.Equal(t, StatusNeedsLogin, first.Status)
			assert.Equal(t, StatusNeedsLogin, second.Status)
			require.Len(t, *events, 2, "a call refused locally is still reported")
			assert.Equal(t, StatusNeedsLogin, (*events)[1].Status)
		})
	}
}

func TestConnector_SearchSources_EmptyQueryNeverReachesThePlatform(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, requests := tc.build(t, failNone)

			res := c.SearchSources(context.Background(), SearchQuery{})

			assert.Equal(t, StatusInvalid, res.Status)
			assert.Zero(t, requests())
		})
	}
}

func TestConnector_SearchSources_ResultJSONIsFlat(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failRateLimited)

			data, err := json.Marshal(c.SearchSources(context.Background(), SearchQuery{Query: "genshin impact"}))
			require.NoError(t, err)

			var top map[string]any
			require.NoError(t, json.Unmarshal(data, &top))
			assert.Equal(t, "rate_limited", top["status"])
			assert.Contains(t, top, "retry_after")
		})
	}
}
