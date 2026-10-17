package connectors

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"pentagi/pkg/config"
)

// TikTokConfig holds per-call defaults the operator can change. They are lower
// than Instagram's: TikTok flags busy accounts sooner.
type TikTokConfig struct {
	// Profile, when set, is registered on the browser client for this account.
	Profile       *BrowserProfile
	SearchScrolls int
	ReadScrolls   int
	MaxComments   int
	Settle        time.Duration
}

func (c TikTokConfig) withDefaults() TikTokConfig {
	if c.SearchScrolls <= 0 {
		c.SearchScrolls = 1
	}
	if c.ReadScrolls <= 0 {
		c.ReadScrolls = 3
	}
	if c.MaxComments <= 0 {
		c.MaxComments = 100
	}
	if c.Settle <= 0 {
		c.Settle = 4 * time.Second
	}
	return c
}

// tiktokCommentPanel is the comment list beside a video, which scrolls on its
// own; the service falls back to the page when the class names change.
const tiktokCommentPanel = `[data-e2e="comment-list"], [class*="DivCommentListContainer"], [class*="DivCommentContainer"]`

// TikTok reads search results, creators' videos and the comments under videos
// through one logged-in browser profile. The service never fetches the videos
// themselves: everything comes from the JSON the pages request.
type TikTok struct {
	browser *BrowserClient
	account string
	cfg     TikTokConfig
	baseURL string
}

func NewTikTok(browser *BrowserClient, account string, cfg TikTokConfig) *TikTok {
	if cfg.Profile != nil {
		browser.SetProfile(account, *cfg.Profile)
	}
	return &TikTok{
		browser: browser,
		account: account,
		cfg:     cfg.withDefaults(),
		baseURL: "https://www.tiktok.com",
	}
}

func (k *TikTok) Account() string { return k.account }

var _ Connector = (*TikTok)(nil)

func init() {
	RegisterRemoteLogin(PlatformTikTok, browserRemoteLogin{platform: PlatformTikTok})
	Register(PlatformTikTok, func(_ context.Context, cfg *config.Config, acc Account) (Connector, error) {
		browser, err := browserFor(cfg)
		if err != nil {
			return nil, err
		}
		profile := browserProfile(acc)
		return NewTikTok(browser, BrowserAccountID(PlatformTikTok, acc.ID), TikTokConfig{Profile: &profile}), nil
	})
}

func (k *TikTok) Platform() Platform { return PlatformTikTok }

// Capabilities: there is nothing to join, and RecommendSources on a creator
// lists their videos, each of whose comments can then be read.
func (k *TikTok) Capabilities() []Capability {
	return []Capability{CapSearchSources, CapResolveSource, CapRecommendSources, CapReadHistory}
}

func (k *TikTok) SearchSources(ctx context.Context, query string, limit int) ([]Source, error) {
	found, err := k.Search(ctx, query)
	if err != nil {
		return nil, contractError(err)
	}
	return webSources(found, limit), nil
}

func (k *TikTok) RecommendSources(ctx context.Context, ref SourceRef, limit int) ([]Source, error) {
	id := ttSourceID(ref)
	if !strings.HasPrefix(id, "@") {
		return nil, unsupported("recommendations work on a creator source")
	}
	videos, err := k.ReadCreator(ctx, id)
	if err != nil {
		return nil, contractError(err)
	}
	sources := []Source{}
	for _, v := range videos {
		if limit > 0 && len(sources) >= limit {
			break
		}
		sources = append(sources, Source{
			ExternalID:  v.PostID,
			Username:    v.Author.Username,
			Title:       truncate(v.Text, 80),
			Link:        v.URL,
			Kind:        SourceVideo,
			MemberCount: v.Replies,
		})
	}
	return sources, nil
}

var (
	ttVideoPath   = regexp.MustCompile(`tiktok\.com/@([A-Za-z0-9._]+)/(?:video|photo)/(\d+)`)
	ttProfilePath = regexp.MustCompile(`tiktok\.com/@([A-Za-z0-9._]+)`)
	ttUsername    = regexp.MustCompile(`^[A-Za-z0-9._]{2,24}$`)
	ttBareID      = regexp.MustCompile(`^\d{15,}$`)
)

// ResolveSource reads the kind from the link alone: a video (its page holds
// the comments) or a creator (@name or a profile URL). A video needs the full
// link, since its page address carries the creator's name; short vm.tiktok.com
// links are not followed.
func (k *TikTok) ResolveSource(_ context.Context, link string) (Source, error) {
	link = strings.TrimSpace(link)
	if m := ttVideoPath.FindStringSubmatch(link); m != nil {
		return Source{ExternalID: m[2], Username: m[1], Link: k.videoURL(m[1], m[2]), Kind: SourceVideo}, nil
	}
	name := strings.TrimPrefix(link, "@")
	if m := ttProfilePath.FindStringSubmatch(link); m != nil {
		name = m[1]
	}
	if ttBareID.MatchString(name) {
		return Source{}, fmt.Errorf("%w: a bare video id has no page address; pass the full video link", ErrNotFound)
	}
	if !ttUsername.MatchString(name) {
		return Source{}, fmt.Errorf("%w: not a TikTok creator or full video link: %s", ErrNotFound, link)
	}
	return Source{ExternalID: "@" + strings.ToLower(name), Username: name, Title: name,
		Link: k.baseURL + "/@" + name, Kind: SourceProfile}, nil
}

func (k *TikTok) JoinSource(context.Context, SourceRef) (JoinResult, error) {
	return JoinResult{}, unsupported("TikTok sources have no membership; read them directly")
}

// ReadHistory reads the comments and replies under a video, or a creator's
// videos. TikTok orders comments by its own ranking, not by time, so a read
// takes the configured number of panel scrolls and historyPage sorts by time.
func (k *TikTok) ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error) {
	id := ttSourceID(ref)
	var posts []WebPost
	if strings.HasPrefix(id, "@") {
		videos, err := k.ReadCreator(ctx, id)
		if err != nil {
			return HistoryPage{}, contractError(err)
		}
		posts = videos
	} else {
		target := ref.Link
		if target == "" {
			target = id
		}
		page, err := k.ReadComments(ctx, target)
		if err != nil {
			return HistoryPage{}, contractError(err)
		}
		posts = page.Posts
	}
	return historyPage(posts, req, k.cfg.MaxComments), nil
}

// ttSourceID recovers the @name or video id a source was resolved to.
func ttSourceID(ref SourceRef) string {
	if ref.ExternalID != "" {
		return ref.ExternalID
	}
	if m := ttVideoPath.FindStringSubmatch(ref.Link); m != nil {
		return m[2]
	}
	if ref.Username != "" {
		return "@" + strings.ToLower(ref.Username)
	}
	if m := ttProfilePath.FindStringSubmatch(ref.Link); m != nil {
		return "@" + strings.ToLower(m[1])
	}
	return ref.Link
}

func (k *TikTok) GetUser(context.Context, string) (UserProfile, error) {
	return UserProfile{}, unsupported("TikTok profiles are not read; the author's link is on each message")
}

func (k *TikTok) Close() error {
	return k.browser.Close(context.Background(), k.account)
}

// Search returns the videos and creators TikTok's search page shows for query.
func (k *TikTok) Search(ctx context.Context, query string) ([]WebSource, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &WebError{Code: WebErrBadRequest, Message: "query is empty"}
	}
	captures, err := k.collect(ctx, k.baseURL+"/search?q="+url.QueryEscape(query), k.cfg.SearchScrolls)
	if err != nil {
		return nil, err
	}
	return ParseTikTokSearch(captures), nil
}

// ReadCreator reads the videos on a creator's page, newest first.
func (k *TikTok) ReadCreator(ctx context.Context, username string) ([]WebPost, error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if m := ttProfilePath.FindStringSubmatch(username); m != nil {
		username = m[1]
	}
	if !ttUsername.MatchString(username) {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not a TikTok username: " + username}
	}
	captures, err := k.collect(ctx, k.baseURL+"/@"+username, k.cfg.SearchScrolls)
	if err != nil {
		return nil, err
	}
	sourceID := "@" + strings.ToLower(username)
	var own []WebPost
	for _, v := range ParseTikTokVideos(captures, sourceID) {
		if strings.EqualFold(v.Author.Username, username) {
			own = append(own, v)
		}
	}
	return own, nil
}

// ReadComments reads the comments and replies under one video, and the video
// itself when its page fetched it.
func (k *TikTok) ReadComments(ctx context.Context, videoURL string) (*WebPage, error) {
	m := ttVideoPath.FindStringSubmatch(videoURL)
	if m == nil {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not a full TikTok video link: " + videoURL}
	}
	videoID := m[2]
	link := k.videoURL(m[1], videoID)
	page, err := k.browser.Navigate(ctx, k.account, PlatformTikTok, link, k.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	posts := ParseTikTokComments(captures, videoID, link)
	reachedEnd := page.ReachedEnd
	for i := 0; i < k.cfg.ReadScrolls && !reachedEnd && len(posts) < k.cfg.MaxComments; i++ {
		if page, err = k.browser.ScrollIn(ctx, k.account, tiktokCommentPanel, 1, k.cfg.Settle); err != nil {
			return nil, err
		}
		reachedEnd = page.ReachedEnd
		captures = append(captures, page.Captures...)
		posts = ParseTikTokComments(captures, videoID, link)
	}
	for _, v := range ParseTikTokVideos(captures, videoID) {
		if v.PostID == videoID {
			posts = append(posts, v)
		}
	}
	return &WebPage{Posts: posts, Done: reachedEnd}, nil
}

func (k *TikTok) collect(ctx context.Context, pageURL string, scrolls int) ([]Capture, error) {
	page, err := k.browser.Navigate(ctx, k.account, PlatformTikTok, pageURL, k.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	for i := 0; i < scrolls && !page.ReachedEnd; i++ {
		if page, err = k.browser.Scroll(ctx, k.account, 1, k.cfg.Settle); err != nil {
			return nil, err
		}
		captures = append(captures, page.Captures...)
	}
	return captures, nil
}

func (k *TikTok) videoURL(username, id string) string {
	return k.baseURL + "/@" + username + "/video/" + id
}

// ParseTikTokVideos extracts videos from search, creator and detail responses:
// objects with id, createTime and an author carrying uniqueId.
func ParseTikTokVideos(captures []Capture, sourceID string) []WebPost {
	seen := map[string]bool{}
	var out []WebPost
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			id := jsonString(m, "id")
			created := jsonInt(m, "createTime")
			author, _ := m["author"].(map[string]any)
			if id == "" || created == 0 || author == nil {
				return true
			}
			a := tiktokAuthor(author)
			if a.Username == "" || seen[id] {
				return false
			}
			seen[id] = true
			out = append(out, WebPost{
				Platform:  PlatformTikTok,
				Kind:      WebPostPost,
				SourceID:  sourceID,
				PostID:    id,
				URL:       "https://www.tiktok.com/@" + a.Username + "/video/" + id,
				Author:    a,
				Text:      jsonString(m, "desc"),
				CreatedAt: unixTime(created),
				Likes:     jsonInt(m, "stats", "diggCount"),
				Replies:   jsonInt(m, "stats", "commentCount"),
			})
			return false
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ParseTikTokComments extracts the comments and replies under videoID. TikTok
// has no address for a single comment, so each links to its video.
func ParseTikTokComments(captures []Capture, videoID, videoURL string) []WebPost {
	seen := map[string]bool{}
	var out []WebPost
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			cid := jsonString(m, "cid")
			created := jsonInt(m, "create_time")
			user, _ := m["user"].(map[string]any)
			if cid == "" || created == 0 || user == nil {
				return true
			}
			if aweme := jsonString(m, "aweme_id"); aweme != "" && aweme != videoID {
				return false
			}
			author := tiktokAuthor(user)
			if author.UserID == "" || seen[cid] {
				return false
			}
			seen[cid] = true
			p := WebPost{
				Platform:  PlatformTikTok,
				Kind:      WebPostComment,
				SourceID:  videoID,
				PostID:    cid,
				ParentID:  videoID,
				URL:       videoURL,
				Author:    author,
				Text:      jsonString(m, "text"),
				CreatedAt: unixTime(created),
				Likes:     jsonInt(m, "digg_count"),
				Replies:   jsonInt(m, "reply_comment_total"),
			}
			if parent := jsonString(m, "reply_id"); parent != "" && parent != "0" {
				p.Kind = WebPostReply
				p.ParentID = parent
			}
			out = append(out, p)
			// A comment carries its first replies inline as reply_comment.
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ParseTikTokSearch extracts the videos and creators from search responses:
// the general search mixes items and user_list entries, the video and user
// tabs carry one kind each.
func ParseTikTokSearch(captures []Capture) []WebSource {
	seen := map[string]bool{}
	var out []WebSource
	for _, v := range ParseTikTokVideos(captures, "") {
		seen[v.PostID] = true
		published := v.CreatedAt
		out = append(out, WebSource{
			Platform:    PlatformTikTok,
			Kind:        SourceVideo,
			SourceID:    v.PostID,
			URL:         v.URL,
			Title:       truncate(v.Text, 80),
			Members:     v.Replies,
			PublishedAt: &published,
		})
	}
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			info, ok := m["user_info"].(map[string]any)
			if !ok {
				return true
			}
			a := tiktokAuthor(info)
			id := "@" + strings.ToLower(a.Username)
			if a.Username != "" && !seen[id] {
				seen[id] = true
				s := WebSource{
					Platform:    PlatformTikTok,
					Kind:        SourceProfile,
					SourceID:    id,
					URL:         a.ProfileURL,
					Title:       a.DisplayName,
					Description: jsonString(info, "signature"),
					Members:     jsonInt(info, "follower_count"),
				}
				if s.Title == "" {
					s.Title = a.Username
				}
				out = append(out, s)
			}
			return false
		})
	}
	return out
}

// tiktokAuthor reads both the web shape (id, uniqueId) and the app shape
// (uid, unique_id) of a user.
func tiktokAuthor(u map[string]any) WebAuthor {
	id := jsonString(u, "id")
	if id == "" {
		id = jsonString(u, "uid")
	}
	name := jsonString(u, "uniqueId")
	if name == "" {
		name = jsonString(u, "unique_id")
	}
	a := WebAuthor{UserID: id, Username: name, DisplayName: jsonString(u, "nickname")}
	if name != "" {
		a.ProfileURL = "https://www.tiktok.com/@" + name
	}
	return a
}
