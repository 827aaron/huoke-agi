package connectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ttVideo    = "https://www.tiktok.com/@pixel.ren/video/7412345678901234567"
	ttComments = "https://www.tiktok.com/api/comment/list/?aweme_id=7412345678901234567"
)

func newTestTikTok(t *testing.T, fb *fakeBrowser) *TikTok {
	return NewTikTok(fb.client(t, BrowserConfig{}), "tt-acct-1", TikTokConfig{})
}

func ttCommentPages(t *testing.T) *fakePage {
	return &fakePage{Steps: [][]Capture{
		sample(t, "tiktok/comments_1.json", ttComments+"&cursor=0"),
		sample(t, "tiktok/comments_2.json", ttComments+"&cursor=20"),
	}}
}

var ren = WebAuthor{UserID: "6800000000000000001", Username: "pixel.ren", DisplayName: "Ren", ProfileURL: "https://www.tiktok.com/@pixel.ren"}

func TestTikTok_Search_ReturnsVideosAndCreators(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.tiktok.com/search?q=gacha+mmo", &fakePage{Steps: [][]Capture{
		sample(t, "tiktok/search_general.json", "https://www.tiktok.com/api/search/general/full/?keyword=gacha%20mmo"),
	}})

	sources, err := newTestTikTok(t, fb).Search(context.Background(), "gacha mmo")
	require.NoError(t, err)
	first, second := ts(1760500000), ts(1760400000)
	assert.Equal(t, []WebSource{
		{Platform: "tiktok", Kind: "video", SourceID: "7412345678901234567", URL: ttVideo,
			Title: "Which mobile MMO has the fairest gacha? I'm done with pay-to-win", Members: 321, PublishedAt: &first},
		{Platform: "tiktok", Kind: "video", SourceID: "7412345678901234999", URL: "https://www.tiktok.com/@tired.gamer/video/7412345678901234999",
			Members: 4, PublishedAt: &second},
		{Platform: "tiktok", Kind: "profile", SourceID: "@gachaqueen", URL: "https://www.tiktok.com/@gachaqueen",
			Title: "Mia plays", Description: "daily gacha pulls", Members: 88000},
		{Platform: "tiktok", Kind: "profile", SourceID: "@pixel.ren", URL: "https://www.tiktok.com/@pixel.ren", Title: "Ren", Members: 1500},
	}, sources)
}

func TestTikTok_ReadCreator_KeepsOnlyTheCreatorsOwnVideosNewestFirst(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.tiktok.com/@pixel.ren", &fakePage{Steps: [][]Capture{
		sample(t, "tiktok/creator_item_list.json", "https://www.tiktok.com/api/post/item_list/?secUid=x"),
	}})

	videos, err := newTestTikTok(t, fb).ReadCreator(context.Background(), "https://www.tiktok.com/@pixel.ren?lang=en")
	require.NoError(t, err)
	assert.Equal(t, []WebPost{
		{Platform: "tiktok", Kind: "post", SourceID: "@pixel.ren", PostID: "7412345678900000002",
			URL: "https://www.tiktok.com/@pixel.ren/video/7412345678900000002", Author: ren,
			Text: "Server maintenance again, thinking of quitting", CreatedAt: ts(1760600000), Likes: 150, Replies: 12},
		{Platform: "tiktok", Kind: "post", SourceID: "@pixel.ren", PostID: "7412345678900000001",
			URL: "https://www.tiktok.com/@pixel.ren/video/7412345678900000001", Author: ren,
			Text: "New season tier list, who are you maining?", CreatedAt: ts(1760300000), Likes: 800, Replies: 40},
	}, videos)
}

func TestTikTok_ReadComments_ScrollsTheCommentPanelForCommentsAndReplies(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(ttVideo, ttCommentPages(t))

	page, err := newTestTikTok(t, fb).ReadComments(context.Background(), ttVideo+"?is_from_webapp=1")
	require.NoError(t, err)
	assert.Equal(t, []WebPost{
		{Platform: "tiktok", Kind: "reply", SourceID: "7412345678901234567", PostID: "7412000000000000102", ParentID: "7412000000000000101",
			URL: ttVideo, Author: ren, Text: "yes, the Singapore one", CreatedAt: ts(1760520000), Likes: 1},
		{Platform: "tiktok", Kind: "comment", SourceID: "7412345678901234567", PostID: "7412000000000000101", ParentID: "7412345678901234567",
			URL:    ttVideo,
			Author: WebAuthor{UserID: "6900000000000000001", Username: "sea_player", DisplayName: "Ari", ProfileURL: "https://www.tiktok.com/@sea_player"},
			Text:   "is there a server for SEA players?", CreatedAt: ts(1760510000), Likes: 5, Replies: 1},
		{Platform: "tiktok", Kind: "comment", SourceID: "7412345678901234567", PostID: "7412000000000000103", ParentID: "7412345678901234567",
			URL:    ttVideo,
			Author: WebAuthor{UserID: "6900000000000000002", Username: "whale.kid", DisplayName: "K", ProfileURL: "https://www.tiktok.com/@whale.kid"},
			Text:   "spent $200 and still no 5 star", CreatedAt: ts(1760505000), Likes: 40},
		{Platform: "tiktok", Kind: "comment", SourceID: "7412345678901234567", PostID: "7412000000000000105", ParentID: "7412345678901234567",
			URL:    ttVideo,
			Author: WebAuthor{UserID: "6900000000000000003", Username: "angry.dad", DisplayName: "Dad", ProfileURL: "https://www.tiktok.com/@angry.dad"},
			Text:   "refund please, the event was rigged", CreatedAt: ts(1760501000), Likes: 2},
	}, page.Posts)
	assert.True(t, page.Done)
	assert.Equal(t, []string{"navigate " + ttVideo, "scroll"}, fb.callLog())
	assert.Contains(t, fb.lastBody("/sessions/tt-acct-1/scroll")["selector"], `[data-e2e="comment-list"]`)
}

func TestTikTok_ReadHistory_ReturnsCommentsNewerThanMinIDWithTheirParents(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(ttVideo, ttCommentPages(t))

	page, err := newTestTikTok(t, fb).ReadHistory(context.Background(),
		SourceRef{ExternalID: "7412345678901234567", Link: ttVideo},
		HistoryRequest{MinID: 1760505000000485, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, []Message{
		{ID: 1760520000000866, SenderID: "6800000000000000001", SenderUsername: "pixel.ren", SenderName: "Ren", Time: ts(1760520000),
			ReplyToID: 1760510000000247, Text: "yes, the Singapore one", Link: ttVideo},
		{ID: 1760510000000247, SenderID: "6900000000000000001", SenderUsername: "sea_player", SenderName: "Ari", Time: ts(1760510000),
			Text: "is there a server for SEA players?", Link: ttVideo},
	}, page.Messages)
	assert.False(t, page.Exhausted)
}

func TestTikTok_ReadHistory_ACreatorSourceReadsTheirVideos(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.tiktok.com/@pixel.ren", &fakePage{Steps: [][]Capture{
		sample(t, "tiktok/creator_item_list.json", "https://www.tiktok.com/api/post/item_list/?secUid=x"),
	}})

	page, err := newTestTikTok(t, fb).ReadHistory(context.Background(), SourceRef{ExternalID: "@pixel.ren"}, HistoryRequest{Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, []int64{1760600000000279, 1760300000000898}, messageIDs(page))
	assert.True(t, page.Exhausted)
}

func TestTikTok_ReadHistory_AVerificationPageNeedsTheOperator(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.fail["navigate"] = fakeFailure{status: 423, body: `{"error":{"code":"needs_login","message":"verification on page: \"Drag the slider to fit the puzzle\"","account_state":"needs_login"}}`}

	_, err := newTestTikTok(t, fb).ReadHistory(context.Background(), SourceRef{Link: ttVideo}, HistoryRequest{})
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestTikTok_ResolveSource_TellsVideoAndCreatorApart(t *testing.T) {
	k := newTestTikTok(t, newFakeBrowser(t))
	tests := []struct {
		link string
		want Source
	}{
		{ttVideo + "?is_from_webapp=1&sender_device=pc", Source{ExternalID: "7412345678901234567", Username: "pixel.ren", Link: ttVideo, Kind: SourceVideo}},
		{"https://www.tiktok.com/@Pixel.Ren", Source{ExternalID: "@pixel.ren", Username: "Pixel.Ren", Title: "Pixel.Ren", Link: "https://www.tiktok.com/@Pixel.Ren", Kind: SourceProfile}},
		{"@gachaqueen", Source{ExternalID: "@gachaqueen", Username: "gachaqueen", Title: "gachaqueen", Link: "https://www.tiktok.com/@gachaqueen", Kind: SourceProfile}},
	}
	for _, tt := range tests {
		src, err := k.ResolveSource(context.Background(), tt.link)
		require.NoError(t, err, tt.link)
		assert.Equal(t, tt.want, src, tt.link)
	}
	for _, bad := range []string{"https://vm.tiktok.com/ZMabc123/", "7412345678901234567", "a b"} {
		_, err := k.ResolveSource(context.Background(), bad)
		assert.ErrorIs(t, err, ErrNotFound, bad)
	}
}

func TestTikTok_RecommendSources_ACreatorListsTheirVideos(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.tiktok.com/@pixel.ren", &fakePage{Steps: [][]Capture{
		sample(t, "tiktok/creator_item_list.json", "https://www.tiktok.com/api/post/item_list/?secUid=x"),
	}})
	k := newTestTikTok(t, fb)

	sources, err := k.RecommendSources(context.Background(), SourceRef{ExternalID: "@pixel.ren"}, 1)
	require.NoError(t, err)
	assert.Equal(t, []Source{{
		ExternalID: "7412345678900000002", Username: "pixel.ren", Title: "Server maintenance again, thinking of quitting",
		Link: "https://www.tiktok.com/@pixel.ren/video/7412345678900000002", Kind: SourceVideo, MemberCount: 12,
	}}, sources)

	_, err = k.RecommendSources(context.Background(), SourceRef{ExternalID: "7412345678901234567"}, 1)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTikTok_Register_BuildsAConnectorAndARemoteLoginForTheAccount(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(ttVideo, ttCommentPages(t))
	t.Setenv("BROWSER_SERVICE_URL", fb.server.URL)
	t.Setenv("BROWSER_TOKEN", fb.token)
	resetSharedBrowser(t)

	factory, ok := Lookup(PlatformTikTok)
	require.True(t, ok)
	conn, err := factory(context.Background(), nil, Account{ID: 9, Platform: PlatformTikTok, Kind: "browser_session",
		Proxy: "http://u:p@proxy:8080", Fingerprint: []byte(`{"platform":"Win32"}`)})
	require.NoError(t, err)
	assert.Equal(t, []Capability{CapSearchSources, CapResolveSource, CapRecommendSources, CapReadHistory}, conn.Capabilities())

	_, err = conn.ReadHistory(context.Background(), SourceRef{Link: ttVideo}, HistoryRequest{Limit: 1})
	require.NoError(t, err)
	body := fb.lastBody("/sessions/tiktok-9/navigate")
	assert.Equal(t, "tiktok", body["platform"])
	assert.Equal(t, "http://u:p@proxy:8080", body["proxy"])
	assert.Equal(t, map[string]any{"platform": "Win32"}, body["fingerprint"])

	_, ok = LookupRemoteLogin(PlatformTikTok)
	assert.True(t, ok)
}
