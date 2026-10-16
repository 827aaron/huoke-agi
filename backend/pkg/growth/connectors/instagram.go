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

// InstagramConfig holds per-call defaults the operator can change.
type InstagramConfig struct {
	// Profile, when set, is registered on the browser client for this account.
	Profile       *BrowserProfile
	SearchScrolls int
	ReadScrolls   int
	MaxComments   int
	Settle        time.Duration
}

func (c InstagramConfig) withDefaults() InstagramConfig {
	if c.SearchScrolls <= 0 {
		c.SearchScrolls = 2
	}
	if c.ReadScrolls <= 0 {
		c.ReadScrolls = 6
	}
	if c.MaxComments <= 0 {
		c.MaxComments = 200
	}
	if c.Settle <= 0 {
		c.Settle = 3 * time.Second
	}
	return c
}

// Instagram reads hashtags, profiles and the comment threads under posts
// through one logged-in browser profile. There is nothing to join: hashtags
// and public profiles are readable by any logged-in account.
type Instagram struct {
	browser *BrowserClient
	account string
	cfg     InstagramConfig
	baseURL string
}

func NewInstagram(browser *BrowserClient, account string, cfg InstagramConfig) *Instagram {
	if cfg.Profile != nil {
		browser.SetProfile(account, *cfg.Profile)
	}
	return &Instagram{
		browser: browser,
		account: account,
		cfg:     cfg.withDefaults(),
		baseURL: "https://www.instagram.com",
	}
}

func (g *Instagram) Account() string { return g.account }

var _ Connector = (*Instagram)(nil)

func init() {
	Register(PlatformInstagram, func(_ context.Context, _ *config.Config, acc Account) (Connector, error) {
		browser, err := browserFromEnv()
		if err != nil {
			return nil, err
		}
		profile := browserProfile(acc)
		return NewInstagram(browser, BrowserAccountID(PlatformInstagram, acc.ID), InstagramConfig{Profile: &profile}), nil
	})
}

func (g *Instagram) Platform() Platform { return PlatformInstagram }

// Capabilities: there is nothing to join, and RecommendSources on a hashtag
// lists its posts, each of whose comment threads can then be read.
func (g *Instagram) Capabilities() []Capability {
	return []Capability{CapSearchSources, CapResolveSource, CapRecommendSources, CapReadHistory}
}

func (g *Instagram) SearchSources(ctx context.Context, query string, limit int) ([]Source, error) {
	found, err := g.Search(ctx, query)
	if err != nil {
		return nil, contractError(err)
	}
	return webSources(found, limit), nil
}

func (g *Instagram) RecommendSources(ctx context.Context, ref SourceRef, limit int) ([]Source, error) {
	tag := igSourceID(ref)
	if !strings.HasPrefix(tag, "#") {
		return nil, unsupported("recommendations work on a hashtag source")
	}
	_, posts, err := g.SearchHashtag(ctx, tag)
	if err != nil {
		return nil, contractError(err)
	}
	sources := []Source{}
	for _, p := range posts {
		if limit > 0 && len(sources) >= limit {
			break
		}
		sources = append(sources, Source{
			ExternalID:  p.PostID,
			Title:       truncate(p.Text, 80),
			Link:        p.URL,
			Kind:        SourcePost,
			MemberCount: p.Replies,
		})
	}
	return sources, nil
}

var igTagPath = regexp.MustCompile(`instagram\.com/explore/tags/([^/?#]+)`)

// ResolveSource reads the kind from the link alone: a hashtag (#tag or a tag
// page), a post or reel, or a profile (@name, a profile URL or a bare name).
// Nothing is loaded, so a post's title is left empty.
func (g *Instagram) ResolveSource(_ context.Context, link string) (Source, error) {
	link = strings.TrimSpace(link)
	switch {
	case strings.HasPrefix(link, "#") || igTagPath.MatchString(link):
		tag := strings.TrimPrefix(link, "#")
		if m := igTagPath.FindStringSubmatch(link); m != nil {
			tag, _ = url.PathUnescape(m[1])
		}
		if tag == "" || strings.ContainsAny(tag, " /?#") {
			return Source{}, fmt.Errorf("%w: not a hashtag: %s", ErrNotFound, link)
		}
		u := g.baseURL + "/explore/tags/" + url.PathEscape(tag) + "/"
		return Source{ExternalID: "#" + strings.ToLower(tag), Title: "#" + tag, Link: u, Kind: SourceHashtag}, nil
	case igShortcode.MatchString(link):
		code := InstagramShortcode(link)
		return Source{ExternalID: code, Link: g.baseURL + "/p/" + code + "/", Kind: SourcePost}, nil
	default:
		name := strings.Trim(strings.TrimPrefix(link, "@"), "/")
		if m := igProfilePath.FindStringSubmatch(name); m != nil {
			name = m[1]
		}
		if name == "" || strings.ContainsAny(name, " /?#:") {
			return Source{}, fmt.Errorf("%w: not an Instagram link: %s", ErrNotFound, link)
		}
		return Source{ExternalID: "@" + strings.ToLower(name), Username: name, Title: name,
			Link: g.baseURL + "/" + name + "/", Kind: SourceProfile}, nil
	}
}

func (g *Instagram) JoinSource(context.Context, SourceRef) (JoinResult, error) {
	return JoinResult{}, unsupported("Instagram sources have no membership; read them directly")
}

// ReadHistory reads a hashtag's posts, a profile's posts or a post's comments.
func (g *Instagram) ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error) {
	id := igSourceID(ref)
	since := sinceCursor(req.MinID)
	var posts []WebPost
	switch {
	case strings.HasPrefix(id, "#"):
		_, found, err := g.SearchHashtag(ctx, id)
		if err != nil {
			return HistoryPage{}, contractError(err)
		}
		posts = found
	case strings.HasPrefix(id, "@"):
		page, err := g.ReadProfile(ctx, id, since)
		if err != nil {
			return HistoryPage{}, contractError(err)
		}
		posts = page.Posts
	default:
		target := ref.Link
		if target == "" {
			target = id
		}
		page, err := g.ReadComments(ctx, target, since)
		if err != nil {
			return HistoryPage{}, contractError(err)
		}
		posts = page.Posts
	}
	return historyPage(posts, req, g.cfg.MaxComments), nil
}

// igSourceID recovers the #tag, @name or shortcode a source was resolved to.
func igSourceID(ref SourceRef) string {
	if ref.ExternalID != "" {
		return ref.ExternalID
	}
	if ref.Username != "" {
		return "@" + strings.ToLower(ref.Username)
	}
	if m := igTagPath.FindStringSubmatch(ref.Link); m != nil {
		tag, _ := url.PathUnescape(m[1])
		return "#" + strings.ToLower(tag)
	}
	if igShortcode.MatchString(ref.Link) {
		return InstagramShortcode(ref.Link)
	}
	return ref.Link
}

func (g *Instagram) GetUser(context.Context, string) (UserProfile, error) {
	return UserProfile{}, unsupported("Instagram profiles are not read; the author's link is on each message")
}

func (g *Instagram) Close() error {
	return g.browser.Close(context.Background(), g.account)
}

// Search returns the hashtags and profiles Instagram's search box offers for
// query, as sources to read.
func (g *Instagram) Search(ctx context.Context, query string) ([]WebSource, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &WebError{Code: WebErrBadRequest, Message: "query is empty"}
	}
	page, err := g.browser.Navigate(ctx, g.account, PlatformInstagram, g.baseURL+"/explore/search/keyword/?q="+url.QueryEscape(query), g.cfg.Settle)
	if err != nil {
		return nil, err
	}
	return ParseInstagramSearch(page.Captures), nil
}

// SearchHashtag reads the posts shown for a hashtag. The posts' authors are
// candidates themselves, and each post is a source whose comments ReadComments
// reads.
func (g *Instagram) SearchHashtag(ctx context.Context, tag string) (*WebSource, []WebPost, error) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
	if tag == "" || strings.ContainsAny(tag, " /?#") {
		return nil, nil, &WebError{Code: WebErrBadRequest, Message: "not a hashtag: " + tag}
	}
	tagURL := g.baseURL + "/explore/tags/" + url.PathEscape(tag) + "/"
	captures, err := g.collect(ctx, tagURL, g.cfg.SearchScrolls)
	if err != nil {
		return nil, nil, err
	}
	source := &WebSource{Platform: PlatformInstagram, Kind: SourceHashtag, SourceID: "#" + strings.ToLower(tag), URL: tagURL, Title: "#" + tag}
	return source, ParseInstagramMedia(captures, source.SourceID), nil
}

// ReadProfile reads a profile's recent posts, newer than cursor.
func (g *Instagram) ReadProfile(ctx context.Context, username, cursor string) (*WebPage, error) {
	username = strings.Trim(strings.TrimPrefix(strings.TrimSpace(username), "@"), "/")
	if m := igProfilePath.FindStringSubmatch(username); m != nil {
		username = m[1]
	}
	if username == "" || strings.ContainsAny(username, " /?#") {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not an Instagram username: " + username}
	}
	since, err := parseTimeCursor(cursor)
	if err != nil {
		return nil, err
	}
	captures, err := g.collect(ctx, g.baseURL+"/"+username+"/", g.cfg.SearchScrolls)
	if err != nil {
		return nil, err
	}
	fresh := newerThan(ParseInstagramMedia(captures, "@"+strings.ToLower(username)), since)
	return &WebPage{Posts: fresh, Cursor: timeCursor(fresh, since), Done: true}, nil
}

// ReadComments reads the comments and replies under one post, newer than
// cursor. Instagram loads comments newest first as the panel scrolls.
func (g *Instagram) ReadComments(ctx context.Context, postURL, cursor string) (*WebPage, error) {
	code := InstagramShortcode(postURL)
	if code == "" {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not an Instagram post: " + postURL}
	}
	since, err := parseTimeCursor(cursor)
	if err != nil {
		return nil, err
	}
	link := g.baseURL + "/p/" + code + "/"
	page, err := g.browser.Navigate(ctx, g.account, PlatformInstagram, link, g.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	posts := ParseInstagramComments(captures, code)
	reachedEnd := page.ReachedEnd
	for i := 0; i < g.cfg.ReadScrolls && !reachedEnd && len(posts) < g.cfg.MaxComments; i++ {
		if !since.IsZero() {
			if o := oldest(posts); !o.IsZero() && !o.After(since) {
				break
			}
		}
		if page, err = g.browser.Scroll(ctx, g.account, 1, g.cfg.Settle); err != nil {
			return nil, err
		}
		reachedEnd = page.ReachedEnd
		captures = append(captures, page.Captures...)
		posts = ParseInstagramComments(captures, code)
	}
	// The post itself is evidence too: its author asked or said something.
	for _, media := range ParseInstagramMedia(captures, code) {
		if media.PostID == code {
			posts = append(posts, media)
		}
	}
	fresh := newerThan(posts, since)
	caughtUp := !since.IsZero() && len(posts) > len(fresh)
	return &WebPage{Posts: fresh, Cursor: timeCursor(fresh, since), Done: reachedEnd || caughtUp}, nil
}

func (g *Instagram) collect(ctx context.Context, pageURL string, scrolls int) ([]Capture, error) {
	page, err := g.browser.Navigate(ctx, g.account, PlatformInstagram, pageURL, g.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	for i := 0; i < scrolls && !page.ReachedEnd; i++ {
		if page, err = g.browser.Scroll(ctx, g.account, 1, g.cfg.Settle); err != nil {
			return nil, err
		}
		captures = append(captures, page.Captures...)
	}
	return captures, nil
}

var (
	igShortcode   = regexp.MustCompile(`instagram\.com/(?:[A-Za-z0-9._]+/)?(?:p|reel|reels|tv)/([A-Za-z0-9_-]+)`)
	igProfilePath = regexp.MustCompile(`instagram\.com/([A-Za-z0-9._]+)`)
	igBareCode    = regexp.MustCompile(`^[A-Za-z0-9_-]{5,40}$`)
)

// InstagramShortcode is the post code in a post or reel URL, or the argument
// itself when it already is one.
func InstagramShortcode(ref string) string {
	ref = strings.TrimSpace(ref)
	if m := igShortcode.FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	if igBareCode.MatchString(ref) {
		return ref
	}
	return ""
}

// ParseInstagramMedia extracts posts from web API and GraphQL responses. It
// reads both the current shape (code, taken_at, caption.text, user) and the
// older one (shortcode, taken_at_timestamp, edge_media_to_caption, owner).
func ParseInstagramMedia(captures []Capture, sourceID string) []WebPost {
	seen := map[string]bool{}
	var out []WebPost
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			code := jsonString(m, "code")
			if code == "" {
				code = jsonString(m, "shortcode")
			}
			if code == "" {
				return true
			}
			user, _ := m["user"].(map[string]any)
			if user == nil {
				user, _ = m["owner"].(map[string]any)
			}
			if user == nil || seen[code] {
				return true
			}
			author := instagramAuthor(user)
			if author.UserID == "" {
				return true
			}
			seen[code] = true
			text := jsonString(m, "caption", "text")
			if text == "" {
				if edges, ok := jsonPath(m, "edge_media_to_caption", "edges").([]any); ok && len(edges) > 0 {
					text = jsonString(edges[0], "node", "text")
				}
			}
			created := jsonInt(m, "taken_at")
			if created == 0 {
				created = jsonInt(m, "taken_at_timestamp")
			}
			comments := jsonInt(m, "comment_count")
			if comments == 0 {
				comments = jsonInt(m, "edge_media_to_comment", "count")
			}
			out = append(out, WebPost{
				Platform:  PlatformInstagram,
				Kind:      WebPostPost,
				SourceID:  sourceID,
				PostID:    code,
				URL:       "https://www.instagram.com/p/" + code + "/",
				Author:    author,
				Text:      text,
				CreatedAt: unixTime(created),
				Likes:     jsonInt(m, "like_count"),
				Replies:   comments,
			})
			return false
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ParseInstagramComments extracts comments and replies under the post with
// shortcode code. A comment is an object with text, created_at and a user; a
// caption has the same fields and is skipped by its key.
func ParseInstagramComments(captures []Capture, code string) []WebPost {
	seen := map[string]bool{}
	var out []WebPost
	link := "https://www.instagram.com/p/" + code + "/"
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, key string) bool {
			if key == "caption" {
				return false
			}
			if _, isMedia := m["code"]; isMedia {
				return true
			}
			user, _ := m["user"].(map[string]any)
			pk := jsonString(m, "pk")
			if pk == "" {
				pk = jsonString(m, "id")
			}
			created := jsonInt(m, "created_at")
			if created == 0 {
				created = jsonInt(m, "created_at_utc")
			}
			if user == nil || pk == "" || created == 0 || seen[pk] {
				return true
			}
			if _, hasText := m["text"]; !hasText {
				return true
			}
			author := instagramAuthor(user)
			if author.UserID == "" {
				return true
			}
			seen[pk] = true
			p := WebPost{
				Platform:  PlatformInstagram,
				Kind:      WebPostComment,
				SourceID:  code,
				PostID:    pk,
				ParentID:  code,
				URL:       link + "c/" + pk + "/",
				Author:    author,
				Text:      jsonString(m, "text"),
				CreatedAt: unixTime(created),
				Likes:     jsonInt(m, "comment_like_count"),
				Replies:   jsonInt(m, "child_comment_count"),
			}
			parent := jsonString(m, "parent_comment_id")
			if parent == "" {
				parent = jsonString(m, "replied_to_comment_id")
			}
			if parent != "" && parent != "0" {
				p.Kind = WebPostReply
				p.ParentID = parent
				p.URL = link + "c/" + parent + "/r/" + pk + "/"
			}
			out = append(out, p)
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ParseInstagramSearch extracts the hashtags and profiles from a search
// response ({"hashtags":[{"hashtag":{…}}], "users":[{"user":{…}}]}).
func ParseInstagramSearch(captures []Capture) []WebSource {
	seen := map[string]bool{}
	var out []WebSource
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			if tag, ok := m["hashtag"].(map[string]any); ok {
				name := jsonString(tag, "name")
				id := "#" + strings.ToLower(name)
				if name != "" && !seen[id] {
					seen[id] = true
					out = append(out, WebSource{
						Platform: PlatformInstagram,
						Kind:     SourceHashtag,
						SourceID: id,
						URL:      "https://www.instagram.com/explore/tags/" + url.PathEscape(name) + "/",
						Title:    "#" + name,
						Members:  jsonInt(tag, "media_count"),
					})
				}
				return false
			}
			if user, ok := m["user"].(map[string]any); ok {
				a := instagramAuthor(user)
				id := "@" + strings.ToLower(a.Username)
				if a.Username != "" && !seen[id] {
					seen[id] = true
					s := WebSource{
						Platform: PlatformInstagram,
						Kind:     SourceProfile,
						SourceID: id,
						URL:      a.ProfileURL,
						Title:    a.DisplayName,
						Private:  jsonString(user, "is_private") == "true" || user["is_private"] == true,
					}
					if s.Title == "" {
						s.Title = a.Username
					}
					out = append(out, s)
				}
				return false
			}
			return true
		})
	}
	return out
}

func instagramAuthor(u map[string]any) WebAuthor {
	id := jsonString(u, "pk")
	if id == "" {
		id = jsonString(u, "pk_id")
	}
	if id == "" {
		id = jsonString(u, "id")
	}
	a := WebAuthor{UserID: id, Username: jsonString(u, "username"), DisplayName: jsonString(u, "full_name")}
	if a.Username != "" {
		a.ProfileURL = "https://www.instagram.com/" + a.Username + "/"
	}
	return a
}
