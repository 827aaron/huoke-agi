package connectors

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cross-platform behaviour of the Connector contract. It builds every platform's
// connector and fake, so it belongs to no single source file.

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
		{PlatformTikTok, browserConformance(func(t *testing.T, fb *fakeBrowser) Connector { return newTestTikTok(t, fb) })},
		{PlatformYouTube, youtubeConformance},
	}
}

func TestConnector_Platform_MatchesTheConstructedPlatform(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failNone)
			assert.Equal(t, tc.platform, c.Platform())
		})
	}
}

func TestConnector_SearchSources_RateLimitCarriesTheWait(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failRateLimited)

			_, err := c.SearchSources(context.Background(), "genshin impact", 5)

			var flood *FloodWaitError
			require.ErrorAs(t, err, &flood)
			assert.Positive(t, flood.Wait, "a rate limit must tell the caller how long to wait")
			assert.NotErrorIs(t, err, ErrUnauthorized)
		})
	}
}

func TestConnector_SearchSources_RevokedLoginIsUnauthorized(t *testing.T) {
	for _, tc := range conformanceCases() {
		if tc.platform == PlatformYouTube {
			continue // an API key has no login to lose
		}
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failNeedsLogin)

			_, err := c.SearchSources(context.Background(), "genshin impact", 5)

			require.ErrorIs(t, err, ErrUnauthorized)
			var flood *FloodWaitError
			assert.False(t, errors.As(err, &flood))
		})
	}
}

func TestConnector_SearchSources_EmptyQueryNeverReachesThePlatform(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, requests := tc.build(t, failNone)

			_, err := c.SearchSources(context.Background(), "", 5)

			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrUnauthorized)
			assert.Zero(t, requests())
		})
	}
}

func TestConnector_Capabilities_NeverClaimMoreThanTheConnectorDoes(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := tc.build(t, failNone)

			if !Has(c, CapJoinSource) {
				_, err := c.JoinSource(context.Background(), SourceRef{Link: "https://example.invalid/g"})
				assert.ErrorIs(t, err, ErrUnsupported)
			}
			if !Has(c, CapUserProfile) {
				_, err := c.GetUser(context.Background(), "42")
				assert.ErrorIs(t, err, ErrUnsupported)
			}
		})
	}
}
