package connectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const igAPI = "https://www.instagram.com/api/v1/"

func newTestInstagram(t *testing.T, fb *fakeBrowser) *Instagram {
	return NewInstagram(fb.client(t, BrowserConfig{}), "ig-acct-1", InstagramConfig{})
}

var pixelRen = WebAuthor{UserID: "51234567", Username: "pixel.ren", DisplayName: "Ren | gaming", ProfileURL: "https://www.instagram.com/pixel.ren/"}

func TestInstagram_Search_ReturnsHashtagsAndProfiles(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/explore/search/keyword/?q=genshin", &fakePage{Steps: [][]Capture{
		sample(t, "instagram/search.json", igAPI+"fbsearch/web/top_serp/?query=genshin"),
	}})

	sources, err := newTestInstagram(t, fb).Search(context.Background(), "genshin")
	require.NoError(t, err)
	assert.ElementsMatch(t, []WebSource{
		{Platform: "instagram", Kind: "profile", SourceID: "@pixel.ren", URL: "https://www.instagram.com/pixel.ren/", Title: "Ren | gaming"},
		{Platform: "instagram", Kind: "profile", SourceID: "@genshin.cn.fans", URL: "https://www.instagram.com/genshin.cn.fans/", Title: "genshin.cn.fans", Private: true},
		{Platform: "instagram", Kind: "hashtag", SourceID: "#genshinimpact", URL: "https://www.instagram.com/explore/tags/genshinimpact/", Title: "#genshinimpact", Members: 14523000},
	}, sources)
}

func TestInstagram_SearchHashtag_ReturnsTheTagAndItsPosts(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/explore/tags/hoyoverse/", &fakePage{Steps: [][]Capture{
		sample(t, "instagram/tag_sections.json", igAPI+"tags/web_info/?tag_name=hoyoverse"),
	}})

	source, posts, err := newTestInstagram(t, fb).SearchHashtag(context.Background(), "#hoyoverse")
	require.NoError(t, err)
	assert.Equal(t, &WebSource{Platform: "instagram", Kind: "hashtag", SourceID: "#hoyoverse", URL: "https://www.instagram.com/explore/tags/hoyoverse/", Title: "#hoyoverse"}, source)
	assert.Equal(t, []WebPost{
		{Platform: "instagram", Kind: "post", SourceID: "#hoyoverse", PostID: "DAbC123xyZ", URL: "https://www.instagram.com/p/DAbC123xyZ/",
			Author: pixelRen, Text: "Finally hit max level in #hoyoverse games, what should I play next? Looking for something with real PvP",
			CreatedAt: ts(1760500000), Likes: 230, Replies: 14},
		{Platform: "instagram", Kind: "post", SourceID: "#hoyoverse", PostID: "DAbC456uvW", URL: "https://www.instagram.com/p/DAbC456uvW/",
			Author:    WebAuthor{UserID: "61234567", Username: "gachaqueen", DisplayName: "Mia", ProfileURL: "https://www.instagram.com/gachaqueen/"},
			CreatedAt: ts(1760300000), Likes: 40, Replies: 3},
	}, posts)
}

func TestInstagram_ReadProfile_ReadsTheOlderGraphQLShapeAfterCursor(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/tired.gamer/", &fakePage{Steps: [][]Capture{
		sample(t, "instagram/profile_timeline.json", "https://www.instagram.com/graphql/query"),
	}})

	page, err := newTestInstagram(t, fb).ReadProfile(context.Background(), "https://www.instagram.com/tired.gamer/", "1759500000")
	require.NoError(t, err)
	assert.Equal(t, []WebPost{
		{Platform: "instagram", Kind: "post", SourceID: "@tired.gamer", PostID: "C9Old0001x", URL: "https://www.instagram.com/p/C9Old0001x/",
			Author:    WebAuthor{UserID: "71234567", Username: "tired.gamer", ProfileURL: "https://www.instagram.com/tired.gamer/"},
			Text:      "Server maintenance again... thinking of quitting",
			CreatedAt: ts(1760000000), Replies: 7},
	}, page.Posts)
	assert.Equal(t, "1760000000", page.Cursor)
}

func commentPages(t *testing.T) *fakePage {
	return &fakePage{Steps: [][]Capture{
		sample(t, "instagram/post_comments_1.json", igAPI+"media/3456789012345678901/comments/"),
		sample(t, "instagram/post_comments_2.json", "https://www.instagram.com/graphql/query"),
	}}
}

func TestInstagram_ReadComments_ReturnsCommentsRepliesAndThePost(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/p/DAbC123xyZ/", commentPages(t))

	page, err := newTestInstagram(t, fb).ReadComments(context.Background(), "https://www.instagram.com/reel/DAbC123xyZ/?igsh=abc", "")
	require.NoError(t, err)
	link := "https://www.instagram.com/p/DAbC123xyZ/"
	assert.Equal(t, []WebPost{
		{Platform: "instagram", Kind: "reply", SourceID: "DAbC123xyZ", PostID: "18000000000000004", ParentID: "18000000000000003",
			URL:    link + "c/18000000000000003/r/18000000000000004/",
			Author: WebAuthor{UserID: "91234567", Username: "lumi.ps", DisplayName: "Lumi", ProfileURL: "https://www.instagram.com/lumi.ps/"},
			Text:   "is it on PS5?", CreatedAt: ts(1760512000)},
		{Platform: "instagram", Kind: "comment", SourceID: "DAbC123xyZ", PostID: "18000000000000003", ParentID: "DAbC123xyZ",
			URL:    link + "c/18000000000000003/",
			Author: WebAuthor{UserID: "81234567", Username: "kai_plays", DisplayName: "Kai", ProfileURL: "https://www.instagram.com/kai_plays/"},
			Text:   "Wuthering Waves! I switched last month and spent way less", CreatedAt: ts(1760510000), Likes: 5, Replies: 1},
		{Platform: "instagram", Kind: "comment", SourceID: "DAbC123xyZ", PostID: "18000000000000002", ParentID: "DAbC123xyZ",
			URL:    link + "c/18000000000000002/",
			Author: WebAuthor{UserID: "10123456", Username: "noodle.gg", ProfileURL: "https://www.instagram.com/noodle.gg/"},
			Text:   "same question, following", CreatedAt: ts(1760505000)},
		{Platform: "instagram", Kind: "comment", SourceID: "DAbC123xyZ", PostID: "18000000000000001", ParentID: "DAbC123xyZ",
			URL:    link + "c/18000000000000001/",
			Author: WebAuthor{UserID: "11223344", Username: "zzz.fan", DisplayName: "Z", ProfileURL: "https://www.instagram.com/zzz.fan/"},
			Text:   "Try ZZZ, the combat is great", CreatedAt: ts(1760501000), Likes: 2},
		{Platform: "instagram", Kind: "post", SourceID: "DAbC123xyZ", PostID: "DAbC123xyZ", URL: link,
			Author: pixelRen, Text: "Finally hit max level in #hoyoverse games, what should I play next? Looking for something with real PvP",
			CreatedAt: ts(1760500000), Likes: 230, Replies: 14},
	}, page.Posts)
	assert.Equal(t, "1760512000", page.Cursor)
	assert.True(t, page.Done)
}

func TestInstagram_ReadComments_WithCursorSkipsSeenCommentsAndScrolling(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/p/DAbC123xyZ/", commentPages(t))

	page, err := newTestInstagram(t, fb).ReadComments(context.Background(), "DAbC123xyZ", "1760505000")
	require.NoError(t, err)
	ids := make([]string, 0, len(page.Posts))
	for _, p := range page.Posts {
		ids = append(ids, p.PostID)
	}
	assert.Equal(t, []string{"18000000000000004", "18000000000000003"}, ids)
	assert.True(t, page.Done)
	assert.Equal(t, []string{"navigate https://www.instagram.com/p/DAbC123xyZ/"}, fb.callLog())
}

func TestInstagram_ReadComments_RateLimitIsReturnedWithItsWait(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.fail["navigate"] = fakeFailure{status: 429, body: `{"error":{"code":"rate_limited","message":"page says \"Please wait a few minutes before you try again\"","retry_after_sec":3600}}`}

	_, err := newTestInstagram(t, fb).ReadComments(context.Background(), "DAbC123xyZ", "")
	require.ErrorIs(t, err, ErrWebRateLimited)
	var we *WebError
	require.ErrorAs(t, err, &we)
	assert.Equal(t, "1h0m0s", we.RetryAfter.String())
	assert.Equal(t, "ig-acct-1", we.AccountID)
}

func TestInstagram_InstagramShortcode_AcceptsPostReelAndBareCode(t *testing.T) {
	tests := map[string]string{
		"https://www.instagram.com/p/DAbC123xyZ/":           "DAbC123xyZ",
		"https://instagram.com/reel/C9-old_01x/?igsh=xyz":   "C9-old_01x",
		"https://www.instagram.com/pixel.ren/p/DAbC123xyZ/": "DAbC123xyZ",
		"DAbC123xyZ":                           "DAbC123xyZ",
		"https://www.instagram.com/pixel.ren/": "",
		"not a code!":                          "",
	}
	for in, want := range tests {
		assert.Equal(t, want, InstagramShortcode(in), in)
	}
}

func TestInstagram_ResolveSource_TellsHashtagPostAndProfileApart(t *testing.T) {
	g := newTestInstagram(t, newFakeBrowser(t))
	tests := []struct {
		link string
		want Source
	}{
		{"#GenshinImpact", Source{ExternalID: "#genshinimpact", Title: "#GenshinImpact", Link: "https://www.instagram.com/explore/tags/GenshinImpact/", Kind: SourceHashtag}},
		{"https://www.instagram.com/explore/tags/%E5%8E%9F%E7%A5%9E/", Source{ExternalID: "#原神", Title: "#原神", Link: "https://www.instagram.com/explore/tags/%E5%8E%9F%E7%A5%9E/", Kind: SourceHashtag}},
		{"https://www.instagram.com/reel/DAbC123xyZ/?igsh=1", Source{ExternalID: "DAbC123xyZ", Link: "https://www.instagram.com/p/DAbC123xyZ/", Kind: SourcePost}},
		{"@Pixel.Ren", Source{ExternalID: "@pixel.ren", Username: "Pixel.Ren", Title: "Pixel.Ren", Link: "https://www.instagram.com/Pixel.Ren/", Kind: SourceProfile}},
		{"https://www.instagram.com/pixel.ren/", Source{ExternalID: "@pixel.ren", Username: "pixel.ren", Title: "pixel.ren", Link: "https://www.instagram.com/pixel.ren/", Kind: SourceProfile}},
	}
	for _, tt := range tests {
		src, err := g.ResolveSource(context.Background(), tt.link)
		require.NoError(t, err, tt.link)
		assert.Equal(t, tt.want, src, tt.link)
	}
	_, err := g.ResolveSource(context.Background(), "https://example.com/a b")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestInstagram_RecommendSources_AHashtagListsItsPosts(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/explore/tags/hoyoverse/", &fakePage{Steps: [][]Capture{
		sample(t, "instagram/tag_sections.json", igAPI+"tags/web_info/?tag_name=hoyoverse"),
	}})
	g := newTestInstagram(t, fb)

	sources, err := g.RecommendSources(context.Background(), SourceRef{ExternalID: "#hoyoverse"}, 1)
	require.NoError(t, err)
	assert.Equal(t, []Source{{
		ExternalID:  "DAbC123xyZ",
		Kind:        SourcePost,
		Title:       "Finally hit max level in #hoyoverse games, what should I play next? Looking for …",
		Link:        "https://www.instagram.com/p/DAbC123xyZ/",
		MemberCount: 14,
	}}, sources)

	_, err = g.RecommendSources(context.Background(), SourceRef{ExternalID: "@pixel.ren"}, 1)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestInstagram_ReadHistory_ReadsAPostsCommentsNewerThanMinID(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.instagram.com/p/DAbC123xyZ/", commentPages(t))

	page, err := newTestInstagram(t, fb).ReadHistory(context.Background(),
		SourceRef{ExternalID: "DAbC123xyZ", Link: "https://www.instagram.com/p/DAbC123xyZ/"},
		HistoryRequest{MinID: 1760505000000999, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, []Message{
		{ID: 1760512000000088, SenderID: "91234567", SenderUsername: "lumi.ps", SenderName: "Lumi", Time: ts(1760512000),
			ReplyToID: 1760510000000421, Text: "is it on PS5?", Link: "https://www.instagram.com/p/DAbC123xyZ/c/18000000000000003/r/18000000000000004/"},
		{ID: 1760510000000421, SenderID: "81234567", SenderUsername: "kai_plays", SenderName: "Kai", Time: ts(1760510000),
			Text: "Wuthering Waves! I switched last month and spent way less",
			Link: "https://www.instagram.com/p/DAbC123xyZ/c/18000000000000003/"},
	}, page.Messages)
	assert.False(t, page.Exhausted)
}

func TestInstagram_JoinSource_IsUnsupported(t *testing.T) {
	_, err := newTestInstagram(t, newFakeBrowser(t)).JoinSource(context.Background(), SourceRef{ExternalID: "#x"})
	assert.ErrorIs(t, err, ErrUnsupported)
}
