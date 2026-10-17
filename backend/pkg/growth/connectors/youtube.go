package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"pentagi/pkg/config"
)

// YouTubeConfig configures the Data API v3 client. MaxInlineBackoff is the
// longest per-minute rate-limit wait sat out before one retry; the daily quota
// is never waited out inline.
type YouTubeConfig struct {
	// APIKeys are used in order: a key whose daily quota runs out rests until
	// Pacific midnight and the call moves on to the next. APIKey is appended,
	// so a single key still works.
	APIKeys          []string
	APIKey           string
	BaseURL          string
	HTTPClient       *http.Client
	MaxInlineBackoff time.Duration
	// CommentPages bounds one ReadComments call, at 100 threads a page.
	CommentPages int
	// AllReplies fetches every reply of a thread instead of the five the
	// thread resource embeds, at one more quota unit per page.
	AllReplies bool
}

// YouTube reads public videos and their comment threads through the official
// Data API, so it needs only an API key and no logged-in account. Commenters
// are identified by their channel id.
type YouTube struct {
	cfg  YouTubeConfig
	http *http.Client
	now  func() time.Time

	mu   sync.Mutex
	keys []youtubeKey
}

type youtubeKey struct {
	value     string
	restUntil time.Time
	invalid   bool
}

// ParseYouTubeKeys splits a comma-separated YOUTUBE_API_KEY.
func ParseYouTubeKeys(s string) []string {
	var keys []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func NewYouTube(cfg YouTubeConfig) (*YouTube, error) {
	var keys []youtubeKey
	seen := map[string]bool{}
	for _, k := range append(append([]string{}, cfg.APIKeys...), ParseYouTubeKeys(cfg.APIKey)...) {
		if k = strings.TrimSpace(k); k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, youtubeKey{value: k})
		}
	}
	if len(keys) == 0 {
		return nil, &WebError{Code: WebErrNotConfigured, Message: "YouTube API key is empty"}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://www.googleapis.com/youtube/v3"
	}
	if cfg.CommentPages <= 0 {
		cfg.CommentPages = 5
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	y := &YouTube{cfg: cfg, http: client, now: time.Now, keys: keys}
	return y, nil
}

type YouTubeSearch struct {
	Query string
	// Order is relevance (default), date, viewCount or rating.
	Order          string
	PublishedAfter time.Time
	RegionCode     string
	Language       string
	MaxResults     int
	PageToken      string
}

// SearchVideos costs 100 quota units a call, the most expensive read; reading
// the comments it leads to costs 1 a page.
func (y *YouTube) SearchVideos(ctx context.Context, s YouTubeSearch) ([]WebSource, string, error) {
	return y.search(ctx, s, "video")
}

func (y *YouTube) SearchChannels(ctx context.Context, s YouTubeSearch) ([]WebSource, string, error) {
	return y.search(ctx, s, "channel")
}

func (y *YouTube) search(ctx context.Context, s YouTubeSearch, kind string) ([]WebSource, string, error) {
	if strings.TrimSpace(s.Query) == "" {
		return nil, "", &WebError{Code: WebErrBadRequest, Message: "query is empty"}
	}
	q := url.Values{"part": {"snippet"}, "type": {kind}, "q": {s.Query}}
	q.Set("maxResults", fmt.Sprint(clampInt(s.MaxResults, 25, 1, 50)))
	if s.Order != "" {
		q.Set("order", s.Order)
	}
	if !s.PublishedAfter.IsZero() {
		q.Set("publishedAfter", s.PublishedAfter.UTC().Format(time.RFC3339))
	}
	if s.RegionCode != "" {
		q.Set("regionCode", s.RegionCode)
	}
	if s.Language != "" {
		q.Set("relevanceLanguage", s.Language)
	}
	if s.PageToken != "" {
		q.Set("pageToken", s.PageToken)
	}
	var resp struct {
		NextPageToken string `json:"nextPageToken"`
		Items         []struct {
			ID struct {
				VideoID   string `json:"videoId"`
				ChannelID string `json:"channelId"`
			} `json:"id"`
			Snippet ytSnippet `json:"snippet"`
		} `json:"items"`
	}
	if err := y.get(ctx, "search", q, &resp); err != nil {
		return nil, "", err
	}
	out := make([]WebSource, 0, len(resp.Items))
	for _, it := range resp.Items {
		if kind == "video" && it.ID.VideoID != "" {
			out = append(out, videoSource(it.ID.VideoID, it.Snippet))
		} else if kind == "channel" && it.ID.ChannelID != "" {
			out = append(out, channelSource(it.ID.ChannelID, it.Snippet))
		}
	}
	return out, resp.NextPageToken, nil
}

type ytSnippet struct {
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	ChannelID    string    `json:"channelId"`
	ChannelTitle string    `json:"channelTitle"`
	PublishedAt  time.Time `json:"publishedAt"`
	ResourceID   struct {
		VideoID string `json:"videoId"`
	} `json:"resourceId"`
}

func videoSource(id string, sn ytSnippet) WebSource {
	s := WebSource{
		Platform:    PlatformYouTube,
		Kind:        SourceVideo,
		SourceID:    id,
		URL:         "https://www.youtube.com/watch?v=" + id,
		Title:       sn.Title,
		Description: sn.ChannelTitle,
	}
	if !sn.PublishedAt.IsZero() {
		t := sn.PublishedAt.UTC()
		s.PublishedAt = &t
	}
	return s
}

func channelSource(id string, sn ytSnippet) WebSource {
	return WebSource{
		Platform:    PlatformYouTube,
		Kind:        SourceKindChannel,
		SourceID:    id,
		URL:         "https://www.youtube.com/channel/" + id,
		Title:       sn.Title,
		Description: truncate(sn.Description, 300),
	}
}

// ChannelVideos lists a channel's uploads newest first, stopping at
// publishedAfter. channel is a channel id, an @handle or a channel URL. It
// reads the uploads playlist, at 1 unit a page instead of search's 100.
func (y *YouTube) ChannelVideos(ctx context.Context, channel string, publishedAfter time.Time, max int) ([]WebSource, error) {
	q := url.Values{"part": {"contentDetails"}}
	id, handle := youtubeChannelRef(channel)
	switch {
	case id != "":
		q.Set("id", id)
	case handle != "":
		q.Set("forHandle", handle)
	default:
		return nil, &WebError{Code: WebErrBadRequest, Message: "not a YouTube channel: " + channel}
	}
	var ch struct {
		Items []struct {
			ContentDetails struct {
				RelatedPlaylists struct {
					Uploads string `json:"uploads"`
				} `json:"relatedPlaylists"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := y.get(ctx, "channels", q, &ch); err != nil {
		return nil, err
	}
	if len(ch.Items) == 0 || ch.Items[0].ContentDetails.RelatedPlaylists.Uploads == "" {
		return nil, &WebError{Code: WebErrNotFound, Message: "channel not found: " + channel}
	}
	max = clampInt(max, 50, 1, 500)
	playlist := ch.Items[0].ContentDetails.RelatedPlaylists.Uploads
	var out []WebSource
	token := ""
	for len(out) < max {
		q := url.Values{"part": {"snippet"}, "playlistId": {playlist}, "maxResults": {"50"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				Snippet ytSnippet `json:"snippet"`
			} `json:"items"`
		}
		if err := y.get(ctx, "playlistItems", q, &page); err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			if !publishedAfter.IsZero() && !it.Snippet.PublishedAt.After(publishedAfter) {
				return out, nil
			}
			if it.Snippet.ResourceID.VideoID != "" && len(out) < max {
				out = append(out, videoSource(it.Snippet.ResourceID.VideoID, it.Snippet))
			}
		}
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	return out, nil
}

var _ Connector = (*YouTube)(nil)

func init() {
	Register(PlatformYouTube, func(_ context.Context, cfg *config.Config, _ Account) (Connector, error) {
		return NewYouTube(YouTubeConfig{APIKeys: cfg.YouTubeAPIKeys()})
	})
	RegisterCredentialCheck(PlatformYouTube, func(ctx context.Context, cfg *config.Config) error {
		y, err := NewYouTube(YouTubeConfig{APIKeys: cfg.YouTubeAPIKeys()})
		if err != nil {
			return err
		}
		return y.Check(ctx)
	})
}

var _ Connector = (*YouTube)(nil)

func (y *YouTube) Platform() Platform { return PlatformYouTube }

// Capabilities: RecommendSources on a channel lists its uploads; a video's
// comments are the messages.
func (y *YouTube) Capabilities() []Capability {
	return []Capability{CapSearchSources, CapResolveSource, CapRecommendSources, CapReadHistory, CapUserProfile}
}

// Check spends one quota unit to learn whether a key works and has quota left.
func (y *YouTube) Check(ctx context.Context) error {
	var out struct{}
	return contractError(y.get(ctx, "i18nRegions", url.Values{"part": {"snippet"}}, &out))
}

func (y *YouTube) SearchSources(ctx context.Context, query string, limit int) ([]Source, error) {
	found, _, err := y.SearchVideos(ctx, YouTubeSearch{Query: query, MaxResults: limit})
	if err != nil {
		return nil, contractError(err)
	}
	return webSources(found, limit), nil
}

func (y *YouTube) RecommendSources(ctx context.Context, ref SourceRef, limit int) ([]Source, error) {
	target := ytRef(ref)
	if id, handle := youtubeChannelRef(target); id == "" && handle == "" {
		return nil, unsupported("recommendations work on a channel source")
	}
	found, err := y.ChannelVideos(ctx, target, time.Time{}, limit)
	if err != nil {
		return nil, contractError(err)
	}
	return webSources(found, limit), nil
}

// ResolveSource accepts a video link or id, or a channel link, id or @handle,
// and looks it up for its title at one quota unit.
func (y *YouTube) ResolveSource(ctx context.Context, link string) (Source, error) {
	var resp struct {
		Items []struct {
			ID      string    `json:"id"`
			Snippet ytSnippet `json:"snippet"`
		} `json:"items"`
	}
	if videoID := YouTubeVideoID(link); videoID != "" {
		if err := y.get(ctx, "videos", url.Values{"part": {"snippet"}, "id": {videoID}}, &resp); err != nil {
			return Source{}, contractError(err)
		}
		if len(resp.Items) == 0 {
			return Source{}, fmt.Errorf("%w: no such video: %s", ErrNotFound, link)
		}
		return videoSource(resp.Items[0].ID, resp.Items[0].Snippet).source(), nil
	}
	q := url.Values{"part": {"snippet"}}
	id, handle := youtubeChannelRef(link)
	switch {
	case id != "":
		q.Set("id", id)
	case handle != "":
		q.Set("forHandle", handle)
	default:
		return Source{}, fmt.Errorf("%w: not a YouTube video or channel: %s", ErrNotFound, link)
	}
	if err := y.get(ctx, "channels", q, &resp); err != nil {
		return Source{}, contractError(err)
	}
	if len(resp.Items) == 0 {
		return Source{}, fmt.Errorf("%w: no such channel: %s", ErrNotFound, link)
	}
	return channelSource(resp.Items[0].ID, resp.Items[0].Snippet).source(), nil
}

func (y *YouTube) JoinSource(context.Context, SourceRef) (JoinResult, error) {
	return JoinResult{}, unsupported("YouTube sources have no membership; read them directly")
}

// ReadHistory reads a video's comments and replies. The API lists threads
// newest first only, so an older-direction read walks from the newest page
// and is exhausted only when the last page was reached within CommentPages.
// A reply posted later under an older thread is not seen by a newer read.
func (y *YouTube) ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error) {
	videoID := YouTubeVideoID(ytRef(ref))
	if videoID == "" {
		return HistoryPage{}, unsupported("only a video's comments are read; list a channel's videos with recommendations")
	}
	since, err := parseTimeCursor(sinceCursor(req.MinID))
	if err != nil {
		return HistoryPage{}, err
	}
	posts, next, err := y.commentPages(ctx, videoID, since, "")
	if err != nil {
		return HistoryPage{}, contractError(err)
	}
	page := historyPage(posts, req, y.cfg.CommentPages*100)
	if next != "" {
		page.Exhausted = false
	}
	return page, nil
}

func ytRef(ref SourceRef) string {
	if ref.ExternalID != "" {
		return ref.ExternalID
	}
	if ref.Link != "" {
		return ref.Link
	}
	return ref.Username
}

func (y *YouTube) GetUser(ctx context.Context, userID string) (UserProfile, error) {
	if !ytBareChannel.MatchString(userID) {
		return UserProfile{}, fmt.Errorf("%w: not a YouTube channel id: %s", ErrNotFound, userID)
	}
	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				CustomURL   string `json:"customUrl"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := y.get(ctx, "channels", url.Values{"part": {"snippet"}, "id": {userID}}, &resp); err != nil {
		return UserProfile{}, contractError(err)
	}
	if len(resp.Items) == 0 {
		return UserProfile{}, fmt.Errorf("%w: no such channel: %s", ErrNotFound, userID)
	}
	it := resp.Items[0]
	return UserProfile{ID: it.ID, Username: it.Snippet.CustomURL, DisplayName: it.Snippet.Title, Bio: it.Snippet.Description}, nil
}

func (y *YouTube) Close() error { return nil }

type ytComment struct {
	ID      string `json:"id"`
	Snippet struct {
		VideoID               string `json:"videoId"`
		TextDisplay           string `json:"textDisplay"`
		TextOriginal          string `json:"textOriginal"`
		ParentID              string `json:"parentId"`
		AuthorDisplayName     string `json:"authorDisplayName"`
		AuthorChannelURL      string `json:"authorChannelUrl"`
		AuthorChannelIDObject struct {
			Value string `json:"value"`
		} `json:"authorChannelId"`
		LikeCount   int64     `json:"likeCount"`
		PublishedAt time.Time `json:"publishedAt"`
	} `json:"snippet"`
}

// ReadComments reads a video's comment threads newest first and returns the
// comments and replies published after cursor. A reply posted later under a
// thread older than cursor is not seen again; that is the cost of the time
// order the API offers.
func (y *YouTube) ReadComments(ctx context.Context, video, cursor string) (*WebPage, error) {
	videoID := YouTubeVideoID(video)
	if videoID == "" {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not a YouTube video: " + video}
	}
	since, err := parseTimeCursor(cursor)
	if err != nil {
		return nil, err
	}
	posts, next, err := y.commentPages(ctx, videoID, since, "")
	if err != nil {
		return nil, err
	}
	return &WebPage{Posts: posts, Cursor: timeCursor(posts, since), Done: next == ""}, nil
}

// commentPages reads up to CommentPages pages of threads from token, newest
// first, stopping at since. next is the page token to continue from, empty
// when the read reached since or the oldest thread.
func (y *YouTube) commentPages(ctx context.Context, videoID string, since time.Time, token string) (posts []WebPost, next string, err error) {
	next = token
	for page := 0; page < y.cfg.CommentPages; page++ {
		q := url.Values{
			"part":       {"snippet,replies"},
			"videoId":    {videoID},
			"order":      {"time"},
			"maxResults": {"100"},
			"textFormat": {"plainText"},
		}
		if next != "" {
			q.Set("pageToken", next)
		}
		var resp struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				Snippet struct {
					TopLevelComment ytComment `json:"topLevelComment"`
					TotalReplyCount int64     `json:"totalReplyCount"`
				} `json:"snippet"`
				Replies struct {
					Comments []ytComment `json:"comments"`
				} `json:"replies"`
			} `json:"items"`
		}
		if err := y.get(ctx, "commentThreads", q, &resp); err != nil {
			return nil, "", err
		}
		next = resp.NextPageToken
		for _, th := range resp.Items {
			top := th.Snippet.TopLevelComment
			if !since.IsZero() && !top.Snippet.PublishedAt.After(since) {
				next = ""
				break
			}
			p := youtubeComment(videoID, top, "")
			p.Replies = th.Snippet.TotalReplyCount
			posts = append(posts, p)
			replies := th.Replies.Comments
			if y.cfg.AllReplies && th.Snippet.TotalReplyCount > int64(len(replies)) {
				if replies, err = y.replies(ctx, top.ID); err != nil {
					return nil, "", err
				}
			}
			for _, r := range replies {
				posts = append(posts, youtubeComment(videoID, r, top.ID))
			}
		}
		if next == "" {
			break
		}
	}
	posts = newerThan(posts, since)
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].CreatedAt.After(posts[j].CreatedAt) })
	return posts, next, nil
}

func (y *YouTube) replies(ctx context.Context, parentID string) ([]ytComment, error) {
	var all []ytComment
	token := ""
	for {
		q := url.Values{"part": {"snippet"}, "parentId": {parentID}, "maxResults": {"100"}, "textFormat": {"plainText"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			NextPageToken string      `json:"nextPageToken"`
			Items         []ytComment `json:"items"`
		}
		if err := y.get(ctx, "comments", q, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextPageToken == "" {
			return all, nil
		}
		token = resp.NextPageToken
	}
}

func youtubeComment(videoID string, c ytComment, parent string) WebPost {
	text := c.Snippet.TextOriginal
	if text == "" {
		text = c.Snippet.TextDisplay
	}
	kind := WebPostComment
	parentID := videoID
	if parent != "" {
		kind = WebPostReply
		parentID = parent
	}
	author := WebAuthor{
		UserID:      c.Snippet.AuthorChannelIDObject.Value,
		DisplayName: c.Snippet.AuthorDisplayName,
		ProfileURL:  c.Snippet.AuthorChannelURL,
	}
	if i := strings.LastIndex(author.ProfileURL, "/@"); i >= 0 {
		author.Username = author.ProfileURL[i+1:]
	}
	return WebPost{
		Platform:  PlatformYouTube,
		Kind:      kind,
		SourceID:  videoID,
		PostID:    c.ID,
		ParentID:  parentID,
		URL:       "https://www.youtube.com/watch?v=" + videoID + "&lc=" + url.QueryEscape(c.ID),
		Author:    author,
		Text:      text,
		CreatedAt: c.Snippet.PublishedAt.UTC(),
		Likes:     c.Snippet.LikeCount,
	}
}

var (
	ytVideoInURL   = regexp.MustCompile(`(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|live/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})`)
	ytBareVideo    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	ytChannelInURL = regexp.MustCompile(`youtube\.com/channel/(UC[A-Za-z0-9_-]{22})`)
	ytBareChannel  = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	ytHandle       = regexp.MustCompile(`(?:youtube\.com/|^)(@[A-Za-z0-9._-]{3,30})`)
)

// YouTubeVideoID is the 11-character id in a watch, shorts, live, embed or
// youtu.be URL, or the argument itself when it already is one.
func YouTubeVideoID(ref string) string {
	ref = strings.TrimSpace(ref)
	if m := ytVideoInURL.FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	if ytBareVideo.MatchString(ref) {
		return ref
	}
	return ""
}

func youtubeChannelRef(ref string) (id, handle string) {
	ref = strings.TrimSpace(ref)
	if m := ytChannelInURL.FindStringSubmatch(ref); m != nil {
		return m[1], ""
	}
	if ytBareChannel.MatchString(ref) {
		return ref, ""
	}
	if m := ytHandle.FindStringSubmatch(ref); m != nil {
		return "", m[1]
	}
	return "", ""
}

func (y *YouTube) get(ctx context.Context, resource string, q url.Values, out any) error {
	err := y.getOnce(ctx, resource, q, out)
	if we, ok := err.(*WebError); ok && we.Code == WebErrRateLimited && we.RetryAfter > 0 && we.RetryAfter <= y.cfg.MaxInlineBackoff {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(we.RetryAfter):
		}
		err = y.getOnce(ctx, resource, q, out)
	}
	return err
}

// getOnce tries the keys in order. A key out of daily quota rests until it
// resets and a rejected key is dropped; either way the call moves on, and only
// when no key is left does the caller see the error, with the earliest reset.
func (y *YouTube) getOnce(ctx context.Context, resource string, q url.Values, out any) error {
	var last error
	for range len(y.keys) + 1 {
		i, key, wait := y.nextKey()
		if key == "" {
			if wait > 0 {
				return &WebError{Code: WebErrRateLimited, RetryAfter: wait, quotaReset: true, Message: "every YouTube API key is out of daily quota"}
			}
			if last == nil {
				last = &WebError{Code: WebErrNotConfigured, Message: "every YouTube API key was rejected"}
			}
			return last
		}
		err := y.request(ctx, resource, q, key, out)
		var we *WebError
		switch {
		case errors.As(err, &we) && we.quotaReset:
			y.restKey(i, y.now().Add(we.RetryAfter), false)
		case errors.As(err, &we) && we.Code == WebErrNotConfigured:
			y.restKey(i, time.Time{}, true)
		default:
			return err
		}
		last = err
	}
	return last
}

func (y *YouTube) keyValues() []string {
	y.mu.Lock()
	defer y.mu.Unlock()
	out := make([]string, 0, len(y.keys))
	for _, k := range y.keys {
		out = append(out, k.value)
	}
	return out
}

func (y *YouTube) nextKey() (int, string, time.Duration) {
	y.mu.Lock()
	defer y.mu.Unlock()
	now := y.now()
	var soonest time.Time
	for i, k := range y.keys {
		if k.invalid {
			continue
		}
		if !now.Before(k.restUntil) {
			return i, k.value, 0
		}
		if soonest.IsZero() || k.restUntil.Before(soonest) {
			soonest = k.restUntil
		}
	}
	if soonest.IsZero() {
		return -1, "", 0
	}
	return -1, "", soonest.Sub(now).Round(time.Second)
}

func (y *YouTube) restKey(i int, until time.Time, invalid bool) {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.keys[i].restUntil = until
	y.keys[i].invalid = invalid
}

func (y *YouTube) request(ctx context.Context, resource string, q url.Values, key string, out any) error {
	q = cloneValues(q)
	q.Set("key", key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(y.cfg.BaseURL, "/")+"/"+resource+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := y.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &WebError{Code: WebErrInternal, Message: "YouTube API unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read YouTube %s response: %w", resource, err)
	}
	if resp.StatusCode >= 400 {
		return youtubeError(resp, raw, y.now())
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode YouTube %s response: %w", resource, err)
	}
	return nil
}

var pacific = mustLoadLocation("America/Los_Angeles")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// youtubeError maps the API's error reasons onto WebError codes. The daily
// quota resets at midnight Pacific time, which becomes RetryAfter.
func youtubeError(resp *http.Response, raw []byte, now time.Time) error {
	status := resp.StatusCode
	var body struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	reason := ""
	if len(body.Error.Errors) > 0 {
		reason = body.Error.Errors[0].Reason
	}
	msg := body.Error.Message
	if msg == "" {
		msg = truncate(string(raw), 200)
	}
	we := &WebError{Message: fmt.Sprintf("%s (HTTP %d, %s)", msg, status, reason)}
	switch reason {
	case "quotaExceeded", "dailyLimitExceeded":
		we.Code = WebErrRateLimited
		we.quotaReset = true
		local := now.In(pacific)
		reset := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, pacific)
		we.RetryAfter = reset.Sub(now).Round(time.Second)
	case "rateLimitExceeded", "userRateLimitExceeded":
		we.Code = WebErrRateLimited
		we.RetryAfter = retryAfterHeader(resp, time.Minute)
	case "commentsDisabled", "forbidden", "channelClosed", "channelSuspended", "videoChartNotFound":
		we.Code = WebErrUnavailable
	case "videoNotFound", "channelNotFound", "playlistNotFound", "commentNotFound", "notFound":
		we.Code = WebErrNotFound
	case "keyInvalid", "keyExpired", "accessNotConfigured", "ipRefererBlocked":
		we.Code = WebErrNotConfigured
	default:
		switch {
		case strings.Contains(msg, "API key not valid"):
			we.Code = WebErrNotConfigured
		case status == http.StatusNotFound:
			we.Code = WebErrNotFound
		case status == http.StatusForbidden:
			we.Code = WebErrUnavailable
		case status == http.StatusBadRequest:
			we.Code = WebErrBadRequest
		case status == http.StatusTooManyRequests:
			we.Code = WebErrRateLimited
			we.RetryAfter = retryAfterHeader(resp, time.Minute)
		default:
			we.Code = WebErrInternal
		}
	}
	return we
}

func retryAfterHeader(resp *http.Response, def time.Duration) time.Duration {
	if sec, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return def
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func clampInt(v, def, lo, hi int) int {
	if v <= 0 {
		v = def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
