package connectors

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fbGroupURL = "https://www.facebook.com/groups/genshinplayersglobal/"
	fbFeedURL  = fbGroupURL + "?sorting_setting=CHRONOLOGICAL"
	fbGraphQL  = "https://www.facebook.com/api/graphql/"
)

func newTestFacebook(t *testing.T, fb *fakeBrowser) *Facebook {
	return NewFacebook(fb.client(t, BrowserConfig{}), "fb-acct-1", FacebookConfig{})
}

func TestFacebook_SearchGroups_ParsesGraphQLResultsAcrossScrolls(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.facebook.com/search/groups/?q=genshin+impact", &fakePage{Steps: [][]Capture{
		sample(t, "facebook/group_search.json", fbGraphQL),
		sample(t, "facebook/group_search_page2.json", fbGraphQL),
	}})

	groups, err := newTestFacebook(t, fb).SearchGroups(context.Background(), " genshin impact ")
	require.NoError(t, err)
	assert.Equal(t, []WebSource{
		{Platform: "facebook", Kind: "group", SourceID: "1784512345678901", URL: "https://www.facebook.com/groups/genshinplayersglobal/", Title: "Genshin Impact Players Global", Description: "10+ posts a day", Members: 12000},
		{Platform: "facebook", Kind: "group", SourceID: "5566778899001122", URL: "https://www.facebook.com/groups/5566778899001122/", Title: "原神玩家交流群", Description: "每天 2 篇帖子", Members: 3456, Private: true},
		{Platform: "facebook", Kind: "group", SourceID: "9988776655443322", URL: "https://www.facebook.com/groups/gachagamersph/", Title: "Gacha Gamers Philippines", Members: 1200000},
	}, groups)
}

func TestFacebook_SearchGroups_FallsBackToLinksWithoutGraphQL(t *testing.T) {
	fb := newFakeBrowser(t)
	html := sample(t, "facebook/group_search.html", "")[0].Body
	fb.page("https://www.facebook.com/search/groups/?q=genshin", &fakePage{Steps: [][]Capture{nil}, HTML: html})

	groups, err := newTestFacebook(t, fb).SearchGroups(context.Background(), "genshin")
	require.NoError(t, err)
	assert.Equal(t, []WebSource{
		{Platform: "facebook", Kind: "group", SourceID: "genshinplayersglobal", URL: "https://www.facebook.com/groups/genshinplayersglobal/", Title: "Genshin Impact Players Global"},
		{Platform: "facebook", Kind: "group", SourceID: "5566778899001122", URL: "https://www.facebook.com/groups/5566778899001122/", Title: "原神玩家交流群 & 攻略"},
	}, groups)
}

func TestFacebook_SearchGroups_RejectsEmptyQuery(t *testing.T) {
	fb := newFakeBrowser(t)
	_, err := newTestFacebook(t, fb).SearchGroups(context.Background(), "  ")
	assert.Equal(t, WebErrBadRequest, WebErrorCodeOf(err))
	assert.Empty(t, fb.callLog())
}

func feedPages(t *testing.T) *fakePage {
	return &fakePage{Steps: [][]Capture{
		sample(t, "facebook/group_feed_1.json", fbGraphQL),
		sample(t, "facebook/group_feed_2.json", fbGraphQL),
	}}
}

var (
	lina  = WebAuthor{UserID: "100012345", Username: "lina.chen.77", DisplayName: "Lina Chen", ProfileURL: "https://www.facebook.com/lina.chen.77"}
	marco = WebAuthor{UserID: "100055555", DisplayName: "Marco Reyes", ProfileURL: "https://www.facebook.com/profile.php?id=100055555"}
)

func TestFacebook_ReadGroup_ReturnsPostsCommentsAndRepliesNewestFirst(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))

	page, err := newTestFacebook(t, fb).ReadGroup(context.Background(), "genshinplayersglobal", "")
	require.NoError(t, err)
	assert.Equal(t, []WebPost{
		{Platform: "facebook", Kind: "reply", SourceID: "genshinplayersglobal", PostID: "4444", ParentID: "2222",
			URL:    "https://www.facebook.com/groups/genshinplayersglobal/posts/1111/?comment_id=2222&reply_comment_id=4444",
			Author: lina, Text: "Same here, I would pay for a good PvP mode", CreatedAt: ts(1760587200)},
		{Platform: "facebook", Kind: "comment", SourceID: "genshinplayersglobal", PostID: "2222", ParentID: "1111",
			URL:    "https://www.facebook.com/groups/genshinplayersglobal/posts/1111/?comment_id=2222",
			Author: marco, Text: "Try Wuthering Waves, combat is way better", CreatedAt: ts(1760583600)},
		{Platform: "facebook", Kind: "post", SourceID: "genshinplayersglobal", PostID: "1111",
			URL:    "https://www.facebook.com/groups/genshinplayersglobal/posts/1111/",
			Author: lina, Text: "Bored of Genshin after 3 years. Any open-world gacha with better endgame? I spend about $50 a month.",
			CreatedAt: ts(1760580000), Replies: 2},
		{Platform: "facebook", Kind: "post", SourceID: "genshinplayersglobal", PostID: "3333",
			URL:    "https://www.facebook.com/groups/genshinplayersglobal/posts/3333/",
			Author: WebAuthor{UserID: "100077777", Username: "sam.ortiz", DisplayName: "Sam Ortiz", ProfileURL: "https://www.facebook.com/sam.ortiz"},
			Text:   "Sharing this guide", CreatedAt: ts(1760570000)},
		{Platform: "facebook", Kind: "post", SourceID: "genshinplayersglobal", PostID: "5555",
			URL:    "https://www.facebook.com/groups/genshinplayersglobal/posts/5555/",
			Author: WebAuthor{UserID: "100099999", Username: "aiko.t", DisplayName: "Aiko Tanaka", ProfileURL: "https://www.facebook.com/aiko.t"},
			Text:   "Selling my AR60 account, DM me", CreatedAt: ts(1760400000)},
	}, page.Posts)
	assert.Equal(t, "1760587200", page.Cursor)
	assert.True(t, page.Done)
	assert.Equal(t, []string{"navigate " + fbFeedURL, "scroll"}, fb.callLog())
}

func TestFacebook_ReadGroup_WithCursorReturnsOnlyNewerAndStopsScrolling(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))

	page, err := newTestFacebook(t, fb).ReadGroup(context.Background(), fbGroupURL, "1760583600")
	require.NoError(t, err)
	require.Len(t, page.Posts, 1)
	assert.Equal(t, "4444", page.Posts[0].PostID)
	assert.Equal(t, "1760587200", page.Cursor)
	assert.True(t, page.Done)
	assert.Equal(t, []string{"navigate " + fbFeedURL}, fb.callLog())
}

func TestFacebook_ReadGroup_KeepsCursorWhenNothingIsNew(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))

	page, err := newTestFacebook(t, fb).ReadGroup(context.Background(), fbGroupURL, "1760587200")
	require.NoError(t, err)
	assert.Empty(t, page.Posts)
	assert.Equal(t, "1760587200", page.Cursor)
}

func TestFacebook_ReadGroup_PrivateGroupIsUnavailable(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, &fakePage{Steps: [][]Capture{nil}, HTML: `<span>Private group · 3K members</span><div aria-label="Join group" role="button">Join group</div>`})

	_, err := newTestFacebook(t, fb).ReadGroup(context.Background(), fbGroupURL, "")
	assert.ErrorIs(t, err, ErrWebUnavailable)
}

func TestFacebook_ReadGroup_RejectsWhatIsNotAGroup(t *testing.T) {
	fb := newFakeBrowser(t)
	f := newTestFacebook(t, fb)
	for _, ref := range []string{"https://www.facebook.com/somepage/", "", "https://example.com/x"} {
		_, err := f.ReadGroup(context.Background(), ref, "")
		assert.Equal(t, WebErrBadRequest, WebErrorCodeOf(err), ref)
	}
	_, err := f.ReadGroup(context.Background(), fbGroupURL, "yesterday")
	assert.Equal(t, WebErrBadRequest, WebErrorCodeOf(err))
	assert.Empty(t, fb.callLog())
}

const (
	fbNotMember = `<div aria-label="Join group" role="button">Join group</div>`
	fbJoined    = `<div aria-label="Joined" role="button">Joined</div><div aria-label="Invite" role="button">Invite</div>`
	fbPending   = `<div aria-label="Cancel request" role="button">Cancel request</div>`
	fbQuestions = `<div role="dialog"><h2>Answer questions</h2><textarea></textarea><div aria-label="Submit" role="button">Submit</div></div>` + fbNotMember
)

func TestFacebook_JoinGroup_ReportsTheResultingMembership(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		afterJoin  string
		afterSub   string
		want       JoinStatus
		wantClicks []string
	}{
		{name: "already a member", html: fbJoined, want: JoinStatusJoined},
		{name: "open group admits at once", html: fbNotMember, afterJoin: fbJoined, want: JoinStatusJoined, wantClicks: []string{"Join group"}},
		{name: "admin approval leaves the request pending", html: fbNotMember, afterJoin: fbPending, want: JoinStatusPending, wantClicks: []string{"Join group"}},
		{name: "already requested", html: fbPending, want: JoinStatusPending},
		{name: "questions submitted unanswered", html: fbNotMember, afterJoin: fbQuestions, afterSub: fbPending, want: JoinStatusPending, wantClicks: []string{"Join group", "Submit"}},
		{name: "questions that must be answered", html: fbNotMember, afterJoin: fbQuestions, afterSub: fbQuestions, want: JoinStatusNotJoinable, wantClicks: []string{"Join group", "Submit"}},
		{name: "no join button", html: `<p>This content isn't available right now</p>`, want: JoinStatusNotJoinable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := newFakeBrowser(t)
			fb.page(fbGroupURL, &fakePage{Steps: [][]Capture{nil}, HTML: tt.html})
			var clicks []string
			fb.onClick = func(target map[string]any, p *fakePage) {
				name := str(target["name"])
				clicks = append(clicks, name)
				switch name {
				case "Join group":
					p.HTML = tt.afterJoin
				case "Submit":
					p.HTML = tt.afterSub
				}
			}

			res, err := newTestFacebook(t, fb).JoinGroup(context.Background(), "https://www.facebook.com/groups/genshinplayersglobal/about")
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.State)
			assert.Equal(t, tt.wantClicks, clicks)
			if tt.want == JoinStatusNotJoinable {
				assert.NotEmpty(t, res.Reason)
			}
		})
	}
}

func TestFacebook_JoinSource_NeedsLoginIsUnauthorized(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.fail["navigate"] = fakeFailure{status: 423, body: `{"error":{"code":"needs_login","message":"redirected to /checkpoint/","account_state":"needs_login"}}`}

	_, err := newTestFacebook(t, fb).JoinSource(context.Background(), SourceRef{ExternalID: "1784512345678901", Link: fbGroupURL})
	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.ErrorContains(t, err, "redirected to /checkpoint/")
	assert.False(t, slices.ContainsFunc(fb.callLog(), func(c string) bool { return c == "click" }))
}

func TestFacebook_JoinSource_RateLimitIsAFloodWait(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.fail["navigate"] = fakeFailure{status: 429, body: `{"error":{"code":"rate_limited","message":"temporarily blocked","retry_after_sec":600}}`}

	_, err := newTestFacebook(t, fb).JoinSource(context.Background(), SourceRef{Username: "genshinplayersglobal"})
	var flood *FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Equal(t, 10*time.Minute, flood.Wait)
}

func TestFacebook_JoinSource_ReportsPendingApproval(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbGroupURL, &fakePage{Steps: [][]Capture{nil}, HTML: fbNotMember})
	fb.onClick = func(_ map[string]any, p *fakePage) { p.HTML = fbPending }

	res, err := newTestFacebook(t, fb).JoinSource(context.Background(), SourceRef{ExternalID: "genshinplayersglobal"})
	require.NoError(t, err)
	assert.Equal(t, JoinStatusPending, res.Status)
}

func TestFacebook_ReadHistory_ReturnsTheNewestPageNewestFirst(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))

	page, err := newTestFacebook(t, fb).ReadHistory(context.Background(), SourceRef{ExternalID: "genshinplayersglobal"}, HistoryRequest{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []Message{
		{ID: 1760587200000757, SenderID: "100012345", SenderUsername: "lina.chen.77", SenderName: "Lina Chen", Time: ts(1760587200),
			ReplyToID: 1760583600000093,
			Text:      "Same here, I would pay for a good PvP mode",
			Link:      "https://www.facebook.com/groups/genshinplayersglobal/posts/1111/?comment_id=2222&reply_comment_id=4444"},
		{ID: 1760583600000093, SenderID: "100055555", SenderName: "Marco Reyes", Time: ts(1760583600),
			ReplyToID: 1760580000000497,
			Text:      "Try Wuthering Waves, combat is way better",
			Link:      "https://www.facebook.com/groups/genshinplayersglobal/posts/1111/?comment_id=2222"},
	}, page.Messages)
	assert.False(t, page.Exhausted)
}

func TestFacebook_ReadHistory_PagesOlderUntilTheLoadedFeedRunsOut(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))
	f := newTestFacebook(t, fb)
	src := SourceRef{ExternalID: "1784512345678901", Link: fbGroupURL}

	page, err := f.ReadHistory(context.Background(), src, HistoryRequest{OffsetID: 1760583600000093, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, []int64{1760580000000497, 1760570000000401, 1760400000000953}, messageIDs(page))
	assert.True(t, page.Exhausted)
}

func TestFacebook_ReadHistory_NewerReturnsThePostsJustAboveMinID(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))

	page, err := newTestFacebook(t, fb).ReadHistory(context.Background(), SourceRef{Link: fbGroupURL},
		HistoryRequest{MinID: 1760580000000497, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{1760583600000093}, messageIDs(page))
	assert.False(t, page.Exhausted)
}

func TestFacebook_ResolveSource_ReadsTitleAndPrivacy(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbGroupURL, &fakePage{Steps: [][]Capture{nil}, HTML: `<span>Private group · 12K members</span>` + fbPending})
	f := newTestFacebook(t, fb)

	src, err := f.ResolveSource(context.Background(), "https://m.facebook.com/groups/genshinplayersglobal/?ref=share")
	require.NoError(t, err)
	assert.Equal(t, Source{
		ExternalID:  "genshinplayersglobal",
		Kind:        SourceKindGroup,
		Link:        fbGroupURL,
		Description: "private group: posts are visible to members only",
	}, src)

	_, err = f.ResolveSource(context.Background(), "https://example.com/groups")
	assert.Equal(t, WebErrBadRequest, WebErrorCodeOf(err))
}

func TestFacebook_Register_BuildsAConnectorThatCarriesTheAccountProfile(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page(fbFeedURL, feedPages(t))
	t.Setenv("BROWSER_SERVICE_URL", fb.server.URL)
	t.Setenv("BROWSER_TOKEN", fb.token)
	resetSharedBrowser(t)

	factory, ok := Lookup(PlatformFacebook)
	require.True(t, ok)
	conn, err := factory(context.Background(), nil, Account{ID: 42, Platform: PlatformFacebook, Kind: "browser_session",
		Proxy: "http://u:p@proxy:8080", Fingerprint: []byte(`{"platform":"Win32"}`)})
	require.NoError(t, err)
	assert.Equal(t, []Capability{CapSearchSources, CapResolveSource, CapJoinSource, CapReadHistory}, conn.Capabilities())

	_, err = conn.ReadHistory(context.Background(), SourceRef{Link: fbGroupURL}, HistoryRequest{Limit: 1})
	require.NoError(t, err)
	body := fb.lastBody("/sessions/facebook-42/navigate")
	assert.Equal(t, "http://u:p@proxy:8080", body["proxy"])
	assert.Equal(t, map[string]any{"platform": "Win32"}, body["fingerprint"])
}
