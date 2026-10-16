package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeYouTubeAPI answers Data API resources from testdata/youtube/ and records
// the query of every request.
type fakeYouTubeAPI struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []url.Values
	paths  []string
	route  func(resource string, q url.Values) (status int, file string, body string)
}

func newFakeYouTubeAPI(t *testing.T, route func(resource string, q url.Values) (int, string, string)) *fakeYouTubeAPI {
	t.Helper()
	f := &fakeYouTubeAPI{route: route}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := filepath.Base(r.URL.Path)
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Query())
		f.paths = append(f.paths, resource)
		f.mu.Unlock()
		status, file, body := f.route(resource, r.URL.Query())
		if file != "" {
			raw, err := os.ReadFile(filepath.Join("testdata", "youtube", file))
			if err != nil {
				t.Error(err)
			}
			body = string(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		if status == http.StatusTooManyRequests || status == http.StatusForbidden {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeYouTubeAPI) client(t *testing.T, cfg YouTubeConfig) *YouTube {
	t.Helper()
	cfg.APIKey = "yt-key"
	cfg.BaseURL = f.server.URL + "/youtube/v3"
	y, err := NewYouTube(cfg)
	require.NoError(t, err)
	return y
}

func threadsRoute(resource string, q url.Values) (int, string, string) {
	switch {
	case resource == "commentThreads" && q.Get("pageToken") == "":
		return 200, "threads_page1.json", ""
	case resource == "commentThreads" && q.Get("pageToken") == "QURTSl9p":
		return 200, "threads_page2.json", ""
	case resource == "comments" && q.Get("parentId") == "UgxAAA":
		return 200, "replies_UgxAAA.json", ""
	}
	return 404, "", `{"error":{"code":404,"message":"unexpected request","errors":[{"reason":"notFound"}]}}`
}

func ytTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

var (
	ytNightOwl = WebPost{Platform: "youtube", Kind: "comment", SourceID: "dQw4w9WgXcQ", PostID: "UgxAAA", ParentID: "dQw4w9WgXcQ",
		URL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ&lc=UgxAAA",
		Author: WebAuthor{UserID: "UC1111111111111111111111", Username: "@NightOwlGamer", DisplayName: "@NightOwlGamer", ProfileURL: "http://www.youtube.com/@NightOwlGamer"},
		Text:   "I quit after 5.0, looking for a new open world game. Spent $2k on this :(", CreatedAt: ytTime("2026-10-12T08:00:00Z"), Likes: 42, Replies: 3}
	ytKiko = WebPost{Platform: "youtube", Kind: "reply", SourceID: "dQw4w9WgXcQ", PostID: "UgxAAA.9Zr1", ParentID: "UgxAAA",
		URL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ&lc=UgxAAA.9Zr1",
		Author: WebAuthor{UserID: "UC2222222222222222222222", Username: "@kiko", DisplayName: "@kiko", ProfileURL: "http://www.youtube.com/@kiko"},
		Text:   "try wuwa", CreatedAt: ytTime("2026-10-12T09:00:00Z"), Likes: 3}
)

func TestYouTube_ReadComments_ReadsAllPagesNewestFirst(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)

	page, err := api.client(t, YouTubeConfig{}).ReadComments(context.Background(), "https://youtu.be/dQw4w9WgXcQ?t=42", "")
	require.NoError(t, err)
	require.Len(t, page.Posts, 4)
	assert.Equal(t, ytKiko, page.Posts[0])
	assert.Equal(t, ytNightOwl, page.Posts[1])
	assert.Equal(t, []string{"UgxBBB", "UgxCCC"}, []string{page.Posts[2].PostID, page.Posts[3].PostID})
	assert.Equal(t, "1791795600", page.Cursor)
	assert.True(t, page.Done)

	require.Len(t, api.calls, 2)
	first := api.calls[0]
	assert.Equal(t, "yt-key", first.Get("key"))
	assert.Equal(t, "dQw4w9WgXcQ", first.Get("videoId"))
	assert.Equal(t, "time", first.Get("order"))
	assert.Equal(t, "snippet,replies", first.Get("part"))
	assert.Equal(t, "QURTSl9p", api.calls[1].Get("pageToken"))
}

func TestYouTube_ReadComments_StopsAtTheCursor(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)

	page, err := api.client(t, YouTubeConfig{}).ReadComments(context.Background(), "dQw4w9WgXcQ", "1791705600")
	require.NoError(t, err)
	assert.Equal(t, []WebPost{ytKiko, ytNightOwl}, page.Posts)
	assert.True(t, page.Done)
	assert.Len(t, api.calls, 1)
}

func TestYouTube_ReadComments_StopsAtThePageBudget(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)

	page, err := api.client(t, YouTubeConfig{CommentPages: 1}).ReadComments(context.Background(), "dQw4w9WgXcQ", "")
	require.NoError(t, err)
	assert.Len(t, page.Posts, 3)
	assert.False(t, page.Done)
	assert.Len(t, api.calls, 1)
}

func TestYouTube_ReadComments_AllRepliesFetchesTheRestOfAThread(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)

	page, err := api.client(t, YouTubeConfig{AllReplies: true}).ReadComments(context.Background(), "dQw4w9WgXcQ", "1791705600")
	require.NoError(t, err)
	ids := []string{}
	for _, p := range page.Posts {
		ids = append(ids, p.PostID)
	}
	assert.Equal(t, []string{"UgxAAA.9Zr2", "UgxAAA.9Zr1", "UgxAAA"}, ids)
	assert.Equal(t, []string{"commentThreads", "comments"}, api.paths)
	assert.Equal(t, "UgxAAA", api.calls[1].Get("parentId"))
}

func TestYouTube_ReadComments_MapsAPIErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantCode  WebErrorCode
		wantRetry string
		check     func(t *testing.T, err error)
	}{
		{name: "daily quota waits until midnight Pacific", status: 403,
			body:     `{"error":{"code":403,"message":"The request cannot be completed because you have exceeded your quota.","errors":[{"reason":"quotaExceeded","domain":"youtube.quota"}]}}`,
			wantCode: WebErrRateLimited, wantRetry: "10h0m0s",
			check: func(t *testing.T, err error) {
				var flood *FloodWaitError
				require.ErrorAs(t, err, &flood)
				assert.Equal(t, 10*time.Hour, flood.Wait)
			}},
		{name: "comments disabled", status: 403,
			body:     `{"error":{"code":403,"message":"The video identified by the videoId parameter has disabled comments.","errors":[{"reason":"commentsDisabled"}]}}`,
			wantCode: WebErrUnavailable, wantRetry: "0s",
			check: func(t *testing.T, err error) { assert.ErrorContains(t, err, "disabled comments") }},
		{name: "video gone", status: 404,
			body:     `{"error":{"code":404,"message":"The video identified by the videoId parameter could not be found.","errors":[{"reason":"videoNotFound"}]}}`,
			wantCode: WebErrNotFound, wantRetry: "0s",
			check: func(t *testing.T, err error) { assert.ErrorIs(t, err, ErrNotFound) }},
		{name: "bad key", status: 400,
			body:     `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"badRequest"}]}}`,
			wantCode: WebErrNotConfigured, wantRetry: "0s",
			check: func(t *testing.T, err error) { assert.Equal(t, WebErrNotConfigured, WebErrorCodeOf(err)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeYouTubeAPI(t, func(string, url.Values) (int, string, string) { return tt.status, "", tt.body })
			y := api.client(t, YouTubeConfig{})
			// 21:00 UTC on 16 Oct 2026 is 14:00 PDT; the quota resets at 07:00 UTC.
			y.now = func() time.Time { return time.Date(2026, 10, 16, 21, 0, 0, 0, time.UTC) }

			_, err := y.ReadComments(context.Background(), "dQw4w9WgXcQ", "")
			var we *WebError
			require.ErrorAs(t, err, &we)
			assert.Equal(t, tt.wantCode, we.Code)
			assert.Equal(t, tt.wantRetry, we.RetryAfter.String())

			_, err = y.ReadHistory(context.Background(), SourceRef{ExternalID: "dQw4w9WgXcQ"}, HistoryRequest{})
			tt.check(t, err)
		})
	}
}

func TestYouTube_ReadComments_SitsOutAShortRateLimitOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return 429, "", `{"error":{"code":429,"errors":[{"reason":"rateLimitExceeded"}]}}`
		}
		return 200, "threads_page2.json", ""
	})

	page, err := api.client(t, YouTubeConfig{MaxInlineBackoff: 2 * time.Second}).ReadComments(context.Background(), "dQw4w9WgXcQ", "")
	require.NoError(t, err)
	assert.Len(t, page.Posts, 1)
	assert.Equal(t, 2, calls)
}

func TestYouTube_SearchVideos_SendsFiltersAndKeepsOnlyVideos(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		return 200, "search_videos.json", ""
	})

	videos, next, err := api.client(t, YouTubeConfig{}).SearchVideos(context.Background(), YouTubeSearch{
		Query: "genshin quit", Order: "date", PublishedAfter: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), RegionCode: "US", Language: "en",
	})
	require.NoError(t, err)
	published := ytTime("2026-10-10T15:00:00Z")
	assert.Equal(t, []WebSource{{Platform: "youtube", Kind: "video", SourceID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Title: "Is Genshin dying? Honest review 2026", Description: "GachaTalk", PublishedAt: &published}}, videos)
	assert.Equal(t, "CAUQAA", next)
	q := api.calls[0]
	assert.Equal(t, "video", q.Get("type"))
	assert.Equal(t, "genshin quit", q.Get("q"))
	assert.Equal(t, "2026-10-01T00:00:00Z", q.Get("publishedAfter"))
	assert.Equal(t, "date", q.Get("order"))
	assert.Equal(t, "US", q.Get("regionCode"))
	assert.Equal(t, "en", q.Get("relevanceLanguage"))
	assert.Equal(t, "25", q.Get("maxResults"))
}

func TestYouTube_ChannelVideos_ResolvesAHandleAndStopsAtPublishedAfter(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		switch resource {
		case "channels":
			return 200, "channel.json", ""
		case "playlistItems":
			return 200, "uploads.json", ""
		}
		return 404, "", `{}`
	})

	videos, err := api.client(t, YouTubeConfig{}).ChannelVideos(context.Background(), "https://www.youtube.com/@GachaTalk/videos", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), 0)
	require.NoError(t, err)
	ids := []string{}
	for _, v := range videos {
		ids = append(ids, v.SourceID)
	}
	assert.Equal(t, []string{"aaaaaaaaaaa", "dQw4w9WgXcQ"}, ids)
	assert.Equal(t, "@GachaTalk", api.calls[0].Get("forHandle"))
	assert.Equal(t, "UUabcdefghijklmnopqrstuv", api.calls[1].Get("playlistId"))
}

func TestYouTube_YouTubeVideoID_AcceptsEveryURLForm(t *testing.T) {
	tests := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":               "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?feature=share&v=dQw4w9WgXcQ": "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?si=x":                         "dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                "dQw4w9WgXcQ",
		"https://m.youtube.com/live/dQw4w9WgXcQ":                    "dQw4w9WgXcQ",
		"dQw4w9WgXcQ":                                               "dQw4w9WgXcQ",
		"https://www.youtube.com/@GachaTalk":                        "",
		"too-short":                                                 "",
	}
	for in, want := range tests {
		assert.Equal(t, want, YouTubeVideoID(in), in)
	}
}

func TestYouTube_NewYouTube_NeedsAKey(t *testing.T) {
	_, err := NewYouTube(YouTubeConfig{})
	assert.ErrorIs(t, err, ErrWebNotConfigured)
}

func TestYouTube_ReadHistory_IsExhaustedOnlyOnceTheLastPageWasRead(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)
	src := SourceRef{ExternalID: "dQw4w9WgXcQ"}

	first, err := api.client(t, YouTubeConfig{CommentPages: 1}).ReadHistory(context.Background(), src, HistoryRequest{})
	require.NoError(t, err)
	assert.Equal(t, []int64{1791795600000656, 1791792000000776, 1791705600000383}, messageIDs(first))
	assert.Equal(t, Message{ID: 1791795600000656, SenderID: "UC2222222222222222222222", SenderUsername: "@kiko", SenderName: "@kiko",
		Time: ts(1791795600), ReplyToID: 1791792000000776, Text: first.Messages[0].Text, Link: first.Messages[0].Link}, first.Messages[0])
	assert.False(t, first.Exhausted)

	older, err := api.client(t, YouTubeConfig{CommentPages: 2}).ReadHistory(context.Background(), src, HistoryRequest{OffsetID: 1791705600000383})
	require.NoError(t, err)
	assert.Equal(t, []int64{1791648000000754}, messageIDs(older))
	assert.True(t, older.Exhausted)
}

func TestYouTube_ReadHistory_NewerReturnsTheCommentsJustAboveMinID(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)

	page, err := api.client(t, YouTubeConfig{}).ReadHistory(context.Background(), SourceRef{Link: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		HistoryRequest{MinID: 1791705600000383, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{1791792000000776}, messageIDs(page))
	assert.False(t, page.Exhausted)
}

func TestYouTube_Register_UsesEveryConfiguredKey(t *testing.T) {
	factory, ok := Lookup(PlatformYouTube)
	require.True(t, ok)
	_, err := factory(context.Background(), &config.Config{}, Account{Platform: PlatformYouTube})
	assert.ErrorIs(t, err, ErrWebNotConfigured)

	conn, err := factory(context.Background(), &config.Config{YouTubeAPIKey: "k1,k2"}, Account{Platform: PlatformYouTube})
	require.NoError(t, err)
	assert.Equal(t, []string{"k1", "k2"}, conn.(*YouTube).keyValues())

	_, ok = LookupCredentialCheck(PlatformYouTube)
	assert.True(t, ok)
}

func TestYouTube_ReadHistory_AChannelIsNotReadable(t *testing.T) {
	api := newFakeYouTubeAPI(t, threadsRoute)
	_, err := api.client(t, YouTubeConfig{}).ReadHistory(context.Background(), SourceRef{ExternalID: "UCabcdefghijklmnopqrstuv"}, HistoryRequest{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Empty(t, api.calls)
}

func TestYouTube_ResolveSource_LooksUpVideosAndChannels(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		switch resource {
		case "videos":
			return 200, "", `{"items":[{"id":"dQw4w9WgXcQ","snippet":{"title":"Is Genshin dying?","channelTitle":"GachaTalk","publishedAt":"2026-10-10T15:00:00Z"}}]}`
		case "channels":
			return 200, "", `{"items":[{"id":"UCabcdefghijklmnopqrstuv","snippet":{"title":"GachaTalk","description":"Gacha news"}}]}`
		}
		return 404, "", `{}`
	})
	y := api.client(t, YouTubeConfig{})

	video, err := y.ResolveSource(context.Background(), "https://youtu.be/dQw4w9WgXcQ")
	require.NoError(t, err)
	assert.Equal(t, "dQw4w9WgXcQ", video.ExternalID)
	assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", video.Link)
	assert.Equal(t, SourceVideo, video.Kind)
	assert.Equal(t, "Is Genshin dying?", video.Title)

	channel, err := y.ResolveSource(context.Background(), "@GachaTalk")
	require.NoError(t, err)
	assert.Equal(t, SourceKindChannel, channel.Kind)
	assert.Equal(t, "UCabcdefghijklmnopqrstuv", channel.ExternalID)
	assert.Equal(t, "@GachaTalk", api.calls[1].Get("forHandle"))

	_, err = y.ResolveSource(context.Background(), "https://example.com/")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestYouTube_RecommendSources_AChannelListsItsUploads(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		switch resource {
		case "channels":
			return 200, "channel.json", ""
		case "playlistItems":
			return 200, "uploads.json", ""
		}
		return 404, "", `{}`
	})

	sources, err := api.client(t, YouTubeConfig{}).RecommendSources(context.Background(), SourceRef{ExternalID: "UCabcdefghijklmnopqrstuv"}, 2)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.Equal(t, "aaaaaaaaaaa", sources[0].ExternalID)
	assert.Equal(t, SourceVideo, sources[0].Kind)
	assert.Equal(t, "UCabcdefghijklmnopqrstuv", api.calls[0].Get("id"))
}

func TestYouTube_GetUser_ReadsTheCommentersChannel(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		return 200, "", `{"items":[{"id":"UC1111111111111111111111","snippet":{"title":"Night Owl","description":"Open-world addict","customUrl":"@nightowlgamer"}}]}`
	})

	user, err := api.client(t, YouTubeConfig{}).GetUser(context.Background(), "UC1111111111111111111111")
	require.NoError(t, err)
	assert.Equal(t, UserProfile{ID: "UC1111111111111111111111", Username: "@nightowlgamer", DisplayName: "Night Owl", Bio: "Open-world addict"}, user)
	assert.Equal(t, "channels", api.paths[0])
}

func TestYouTube_Check_ReportsAnInvalidKey(t *testing.T) {
	api := newFakeYouTubeAPI(t, func(string, url.Values) (int, string, string) {
		return 400, "", `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"badRequest"}]}}`
	})
	err := api.client(t, YouTubeConfig{}).Check(context.Background())
	assert.Equal(t, WebErrNotConfigured, WebErrorCodeOf(err))
}

func TestYouTube_ReadComments_MovesToTheNextKeyWhenOneRunsOutOfQuota(t *testing.T) {
	const quota = `{"error":{"code":403,"message":"quota","errors":[{"reason":"quotaExceeded"}]}}`
	exhausted := map[string]bool{"k1": true}
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		if exhausted[q.Get("key")] {
			return 403, "", quota
		}
		return threadsRoute(resource, q)
	})
	y := api.client(t, YouTubeConfig{APIKeys: []string{"k1", " k2 ", "k1"}, CommentPages: 1})
	now := time.Date(2026, 10, 16, 21, 0, 0, 0, time.UTC)
	y.now = func() time.Time { return now }
	keys := func() []string {
		api.mu.Lock()
		defer api.mu.Unlock()
		var out []string
		for _, q := range api.calls {
			out = append(out, q.Get("key"))
		}
		api.calls = nil
		return out
	}

	page, err := y.ReadComments(context.Background(), "dQw4w9WgXcQ", "")
	require.NoError(t, err)
	assert.NotEmpty(t, page.Posts)
	assert.Equal(t, []string{"k1", "k2"}, keys())

	_, err = y.ReadComments(context.Background(), "dQw4w9WgXcQ", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"k2"}, keys(), "a key out of quota rests until it resets")

	now = time.Date(2026, 10, 17, 7, 0, 1, 0, time.UTC)
	exhausted["k1"] = false
	_, err = y.ReadComments(context.Background(), "dQw4w9WgXcQ", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"k1"}, keys(), "after Pacific midnight the first key is back")
}

func TestYouTube_ReadHistory_IsAFloodWaitOnlyWhenEveryKeyIsOut(t *testing.T) {
	const quota = `{"error":{"code":403,"message":"quota","errors":[{"reason":"quotaExceeded"}]}}`
	api := newFakeYouTubeAPI(t, func(string, url.Values) (int, string, string) { return 403, "", quota })
	y := api.client(t, YouTubeConfig{APIKeys: []string{"k1", "k2"}})
	y.now = func() time.Time { return time.Date(2026, 10, 16, 21, 0, 0, 0, time.UTC) }

	_, err := y.ReadHistory(context.Background(), SourceRef{ExternalID: "dQw4w9WgXcQ"}, HistoryRequest{})
	var flood *FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Equal(t, 10*time.Hour, flood.Wait)
	assert.Len(t, api.calls, 3)
}

func TestYouTube_ReadComments_DropsARejectedKey(t *testing.T) {
	const bad = `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"badRequest"}]}}`
	api := newFakeYouTubeAPI(t, func(resource string, q url.Values) (int, string, string) {
		if q.Get("key") == "k1" {
			return 400, "", bad
		}
		return threadsRoute(resource, q)
	})
	y := api.client(t, YouTubeConfig{APIKeys: []string{"k1"}, CommentPages: 1})

	for range 2 {
		_, err := y.ReadComments(context.Background(), "dQw4w9WgXcQ", "")
		require.NoError(t, err)
	}
	assert.Equal(t, "k1", api.calls[0].Get("key"))
	for _, q := range api.calls[1:] {
		assert.Equal(t, "yt-key", q.Get("key"))
	}
}

func TestYouTube_ParseYouTubeKeys_SplitsACommaList(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, ParseYouTubeKeys(" a, ,b,"))
	assert.Nil(t, ParseYouTubeKeys(""))
}
