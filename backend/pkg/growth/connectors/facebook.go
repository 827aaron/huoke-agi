package connectors

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/config"
)

// FacebookConfig holds per-call defaults; every value can be raised or lowered
// by the operator, none is a quota.
type FacebookConfig struct {
	// Profile, when set, is registered on the browser client for this account.
	Profile       *BrowserProfile
	SearchScrolls int
	ReadScrolls   int
	MaxPosts      int
	Settle        time.Duration
}

func (c FacebookConfig) withDefaults() FacebookConfig {
	if c.SearchScrolls <= 0 {
		c.SearchScrolls = 3
	}
	if c.ReadScrolls <= 0 {
		c.ReadScrolls = 8
	}
	if c.MaxPosts <= 0 {
		c.MaxPosts = 100
	}
	if c.Settle <= 0 {
		c.Settle = 2500 * time.Millisecond
	}
	return c
}

// Facebook reads groups through one logged-in browser profile. The Groups API
// was removed in 2024, so the data comes from the GraphQL responses the page
// itself fetches, with the HTML as a fallback for group search.
type Facebook struct {
	browser *BrowserClient
	account string
	cfg     FacebookConfig
	baseURL string
}

func NewFacebook(browser *BrowserClient, account string, cfg FacebookConfig) *Facebook {
	if cfg.Profile != nil {
		browser.SetProfile(account, *cfg.Profile)
	}
	return &Facebook{
		browser: browser,
		account: account,
		cfg:     cfg.withDefaults(),
		baseURL: "https://www.facebook.com",
	}
}

func (f *Facebook) Account() string { return f.account }

var _ Connector = (*Facebook)(nil)

func init() {
	RegisterRemoteLogin(PlatformFacebook, browserRemoteLogin{platform: PlatformFacebook})
	Register(PlatformFacebook, func(_ context.Context, cfg *config.Config, acc Account) (Connector, error) {
		browser, err := browserFor(cfg)
		if err != nil {
			return nil, err
		}
		profile := browserProfile(acc)
		return NewFacebook(browser, BrowserAccountID(PlatformFacebook, acc.ID), FacebookConfig{Profile: &profile}), nil
	})
}

func (f *Facebook) Platform() Platform { return PlatformFacebook }

func (f *Facebook) Capabilities() []Capability {
	return []Capability{CapSearchSources, CapResolveSource, CapJoinSource, CapReadHistory}
}

func (f *Facebook) SearchSources(ctx context.Context, query string, limit int) ([]Source, error) {
	groups, err := f.SearchGroups(ctx, query)
	if err != nil {
		return nil, contractError(err)
	}
	return webSources(groups, limit), nil
}

// ResolveSource opens a group to learn its title and privacy. A post link
// resolves without a page load.
func (f *Facebook) ResolveSource(ctx context.Context, link string) (Source, error) {
	if id := postIDFromURL(link); id != "" && FacebookGroupID(link) != "" {
		return Source{ExternalID: id, Link: link, Kind: SourcePost}, nil
	}
	target, err := f.groupURL(link)
	if err != nil {
		return Source{}, contractError(err)
	}
	page, err := f.browser.Navigate(ctx, f.account, PlatformFacebook, target, f.cfg.Settle)
	if err != nil {
		return Source{}, contractError(err)
	}
	content, err := f.browser.Content(ctx, f.account, "html", "")
	if err != nil {
		return Source{}, contractError(err)
	}
	src := Source{
		ExternalID: FacebookGroupID(target),
		Title:      strings.TrimSuffix(page.Title, " | Facebook"),
		Link:       target,
		Kind:       SourceKindGroup,
	}
	if fbPrivateMark.MatchString(content.Body) {
		src.Description = "private group: posts are visible to members only"
	}
	return src, nil
}

func (f *Facebook) RecommendSources(context.Context, SourceRef, int) ([]Source, error) {
	return nil, unsupported("Facebook offers no similar-group list to the web client")
}

func (f *Facebook) JoinSource(ctx context.Context, ref SourceRef) (JoinResult, error) {
	joined, err := f.JoinGroup(ctx, refString(ref))
	if err != nil {
		return JoinResult{}, contractError(err)
	}
	return JoinResult{Status: joined.State, Reason: joined.Reason}, nil
}

// ReadHistory reads a group's chronological feed, or one post's comments.
func (f *Facebook) ReadHistory(ctx context.Context, ref SourceRef, req HistoryRequest) (HistoryPage, error) {
	target := refString(ref)
	var page *WebPage
	var err error
	if postIDFromURL(target) == "" {
		page, err = f.ReadGroup(ctx, target, sinceCursor(req.MinID))
	} else {
		page, err = f.ReadPost(ctx, target)
	}
	if err != nil {
		return HistoryPage{}, contractError(err)
	}
	return historyPage(page.Posts, req, f.cfg.MaxPosts), nil
}

func (f *Facebook) GetUser(context.Context, string) (UserProfile, error) {
	return UserProfile{}, unsupported("Facebook profiles are not read; the author's link is on each message")
}

// Close shuts the account's browser; the service relaunches it on next use.
func (f *Facebook) Close() error {
	return f.browser.Close(context.Background(), f.account)
}

func (f *Facebook) SearchGroups(ctx context.Context, query string) ([]WebSource, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &WebError{Code: WebErrBadRequest, Message: "query is empty"}
	}
	page, err := f.browser.Navigate(ctx, f.account, PlatformFacebook, f.baseURL+"/search/groups/?q="+url.QueryEscape(query), f.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	for i := 0; i < f.cfg.SearchScrolls && !page.ReachedEnd; i++ {
		if page, err = f.browser.Scroll(ctx, f.account, 1, f.cfg.Settle); err != nil {
			return nil, err
		}
		captures = append(captures, page.Captures...)
	}
	groups := ParseFacebookGroups(captures)
	if len(groups) > 0 {
		return groups, nil
	}
	content, err := f.browser.Content(ctx, f.account, "html", "")
	if err != nil {
		return nil, err
	}
	return ParseFacebookGroupLinks(content.Body), nil
}

var (
	fbJoinedLabels  = []string{"Joined", "已加入", "Leave group", "退出小组"}
	fbPendingLabels = []string{"Cancel request", "取消申请", "取消请求"}
	fbJoinLabels    = []string{"Join group", "加入小组", "Join Group"}
	fbQuestionMark  = regexp.MustCompile(`(?i)answer questions|membership questions|回答问题|入组问题`)
	fbSubmitLabels  = []string{"Submit", "提交"}
	fbPrivateMark   = regexp.MustCompile(`(?i)private group|私密小组`)
)

// JoinGroup asks to join. Open groups admit at once; most others queue the
// request for an admin, which is reported as pending. Membership questions are
// submitted unanswered, since the agent has nothing truthful to put in them;
// a group that refuses that is not_joinable.
func (f *Facebook) JoinGroup(ctx context.Context, groupURL string) (*WebJoinResult, error) {
	target, err := f.groupURL(groupURL)
	if err != nil {
		return nil, err
	}
	if _, err := f.browser.Navigate(ctx, f.account, PlatformFacebook, target, f.cfg.Settle); err != nil {
		return nil, err
	}
	state, err := f.membership(ctx)
	if err != nil || state != "" {
		return membershipResult(state, ""), err
	}
	label := f.firstLabel(ctx, fbJoinLabels)
	if label == "" {
		return &WebJoinResult{State: JoinStatusNotJoinable, Reason: "the group page offers no join button"}, nil
	}
	if _, err := f.browser.Click(ctx, f.account, Target{Role: "button", Name: label}, f.cfg.Settle); err != nil {
		return nil, err
	}
	content, err := f.browser.Content(ctx, f.account, "html", "")
	if err != nil {
		return nil, err
	}
	if fbQuestionMark.MatchString(content.Body) {
		if submit := labelIn(content.Body, fbSubmitLabels); submit != "" {
			if _, err := f.browser.Click(ctx, f.account, Target{Role: "button", Name: submit}, f.cfg.Settle); err != nil {
				if WebErrorCodeOf(err) != WebErrNotFound {
					return nil, err
				}
			}
		}
	}
	state, err = f.membership(ctx)
	if err != nil {
		return nil, err
	}
	if state == "" {
		return &WebJoinResult{State: JoinStatusNotJoinable, Reason: "the request was not accepted; the group may require answers to its questions"}, nil
	}
	return membershipResult(state, ""), nil
}

func membershipResult(state JoinStatus, reason string) *WebJoinResult {
	if state == "" {
		return nil
	}
	return &WebJoinResult{State: state, Reason: reason}
}

func (f *Facebook) membership(ctx context.Context) (JoinStatus, error) {
	content, err := f.browser.Content(ctx, f.account, "html", "")
	if err != nil {
		return "", err
	}
	return FacebookMembership(content.Body), nil
}

// FacebookMembership reads the group header buttons from the page HTML. It
// returns "" when the account is neither a member nor waiting.
func FacebookMembership(pageHTML string) JoinStatus {
	switch {
	case labelIn(pageHTML, fbPendingLabels) != "":
		return JoinStatusPending
	case labelIn(pageHTML, fbJoinedLabels) != "":
		return JoinStatusJoined
	}
	return ""
}

func (f *Facebook) firstLabel(ctx context.Context, labels []string) string {
	content, err := f.browser.Content(ctx, f.account, "html", "")
	if err != nil {
		return ""
	}
	return labelIn(content.Body, labels)
}

// labelIn finds a button by its aria-label, which Facebook keeps stable and
// localised while class names change on every deploy.
func labelIn(pageHTML string, labels []string) string {
	for _, l := range labels {
		if strings.Contains(pageHTML, `aria-label="`+html.EscapeString(l)+`"`) {
			return l
		}
	}
	return ""
}

// ReadGroup reads a group's feed newest first. cursor is the one a previous
// read returned; posts at or before it are not returned again, and the read
// stops scrolling once the feed reaches them.
func (f *Facebook) ReadGroup(ctx context.Context, groupURL, cursor string) (*WebPage, error) {
	target, err := f.groupURL(groupURL)
	if err != nil {
		return nil, err
	}
	since, err := parseTimeCursor(cursor)
	if err != nil {
		return nil, err
	}
	feed := target + "?sorting_setting=CHRONOLOGICAL"
	page, err := f.browser.Navigate(ctx, f.account, PlatformFacebook, feed, f.cfg.Settle)
	if err != nil {
		return nil, err
	}
	sourceID := FacebookGroupID(target)
	captures := page.Captures
	posts := ParseFacebookPosts(captures, sourceID)
	reachedEnd := page.ReachedEnd
	for i := 0; i < f.cfg.ReadScrolls && !reachedEnd && countKind(posts, WebPostPost) < f.cfg.MaxPosts; i++ {
		if !since.IsZero() {
			if o := oldest(posts); !o.IsZero() && !o.After(since) {
				break
			}
		}
		if page, err = f.browser.Scroll(ctx, f.account, 1, f.cfg.Settle); err != nil {
			return nil, err
		}
		reachedEnd = page.ReachedEnd
		captures = append(captures, page.Captures...)
		posts = ParseFacebookPosts(captures, sourceID)
	}
	if len(posts) == 0 {
		content, err := f.browser.Content(ctx, f.account, "html", "")
		if err != nil {
			return nil, err
		}
		if labelIn(content.Body, fbJoinLabels) != "" && fbPrivateMark.MatchString(content.Body) {
			return nil, &WebError{Code: WebErrUnavailable, Message: "private group: its posts are visible only to members", AccountID: f.account}
		}
	}
	fresh := newerThan(posts, since)
	caughtUp := !since.IsZero() && len(posts) > len(fresh)
	return &WebPage{Posts: fresh, Cursor: timeCursor(fresh, since), Done: reachedEnd || caughtUp}, nil
}

// ReadPost reads one post and the comments loaded under it.
func (f *Facebook) ReadPost(ctx context.Context, postURL string) (*WebPage, error) {
	if !strings.Contains(postURL, "facebook.com/") {
		return nil, &WebError{Code: WebErrBadRequest, Message: "not a Facebook URL: " + postURL}
	}
	page, err := f.browser.Navigate(ctx, f.account, PlatformFacebook, postURL, f.cfg.Settle)
	if err != nil {
		return nil, err
	}
	captures := page.Captures
	for i := 0; i < f.cfg.SearchScrolls && !page.ReachedEnd; i++ {
		if page, err = f.browser.Scroll(ctx, f.account, 1, f.cfg.Settle); err != nil {
			return nil, err
		}
		captures = append(captures, page.Captures...)
	}
	return &WebPage{Posts: ParseFacebookPosts(captures, FacebookGroupID(postURL)), Done: true}, nil
}

func countKind(posts []WebPost, kind WebPostKind) int {
	n := 0
	for _, p := range posts {
		if p.Kind == kind {
			n++
		}
	}
	return n
}

var fbGroupPath = regexp.MustCompile(`facebook\.com/groups/([A-Za-z0-9._-]+)`)

// groupURL accepts a group URL, a numeric id or a vanity name and returns the
// canonical group URL.
func (f *Facebook) groupURL(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if m := fbGroupPath.FindStringSubmatch(ref); m != nil {
		return f.baseURL + "/groups/" + m[1] + "/", nil
	}
	if ref != "" && !strings.ContainsAny(ref, "/:?#") {
		return f.baseURL + "/groups/" + ref + "/", nil
	}
	return "", &WebError{Code: WebErrBadRequest, Message: "not a Facebook group: " + ref}
}

// FacebookGroupID is the id or vanity name in a group URL, or "".
func FacebookGroupID(u string) string {
	if m := fbGroupPath.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

// ParseFacebookGroups extracts groups from search-result GraphQL responses.
// A result wraps the Group in a view model whose snippets carry the privacy
// and member count ("Public · 12K members · 10+ posts a day").
func ParseFacebookGroups(captures []Capture) []WebSource {
	byID := map[string]*WebSource{}
	var order []string
	add := func(g map[string]any) *WebSource {
		id := jsonString(g, "id")
		if id == "" {
			return nil
		}
		s, ok := byID[id]
		if !ok {
			s = &WebSource{Platform: PlatformFacebook, Kind: SourceKindGroup, SourceID: id}
			byID[id] = s
			order = append(order, id)
		}
		if name := jsonString(g, "name"); name != "" {
			s.Title = name
		}
		if u := jsonString(g, "url"); u != "" {
			s.URL = u
		} else if s.URL == "" {
			s.URL = "https://www.facebook.com/groups/" + id + "/"
		}
		return s
	}
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, _ string) bool {
			profile, ok := m["profile"].(map[string]any)
			if ok && jsonString(profile, "__typename") == "Group" {
				s := add(profile)
				if s == nil {
					return true
				}
				for _, snippet := range snippetTexts(m) {
					applyGroupSnippet(s, snippet)
				}
				return true
			}
			if jsonString(m, "__typename") == "Group" && jsonString(m, "name") != "" {
				add(m)
			}
			return true
		})
	}
	out := make([]WebSource, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// snippetTexts returns the snippet texts under m ordered by key, so the
// primary snippet is read before the secondary one.
func snippetTexts(m map[string]any) []string {
	byKey := map[string]string{}
	walkJSON(m, "", func(n map[string]any, key string) bool {
		if key == "profile" {
			return false
		}
		if strings.Contains(key, "snippet") {
			if t := jsonString(n, "text"); t != "" && byKey[key] == "" {
				byKey[key] = t
			}
		}
		return true
	})
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	texts := make([]string, 0, len(keys))
	for _, k := range keys {
		texts = append(texts, byKey[k])
	}
	return texts
}

var (
	memberCount   = regexp.MustCompile(`(?i)([\d.,]+)\s*([KM万千]?)\s*(?:members|位成员|成员)`)
	privateWord   = regexp.MustCompile(`(?i)\bprivate\b|私密`)
	snippetSplits = regexp.MustCompile(`\s*[·•]\s*`)
)

func applyGroupSnippet(s *WebSource, text string) {
	for _, part := range snippetSplits.Split(text, -1) {
		if m := memberCount.FindStringSubmatch(part); m != nil {
			if n := scaleCount(m[1], m[2]); n > 0 {
				s.Members = n
			}
			continue
		}
		if privateWord.MatchString(part) {
			s.Private = true
		} else if s.Description == "" && part != "" && !strings.EqualFold(part, "Public") && part != "公开" {
			s.Description = part
		}
	}
}

func scaleCount(num, unit string) int64 {
	f, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(unit) {
	case "K", "千":
		f *= 1e3
	case "M":
		f *= 1e6
	case "万":
		f *= 1e4
	}
	return int64(f + 0.5)
}

var fbGroupHref = regexp.MustCompile(`href="(?:https://www\.facebook\.com)?/groups/([A-Za-z0-9._-]+)/?"[^>]*>([^<]{1,200})</a>`)

// ParseFacebookGroupLinks is the fallback when no GraphQL response was
// captured: group links in the result HTML, with the link text as the title.
func ParseFacebookGroupLinks(pageHTML string) []WebSource {
	seen := map[string]bool{}
	var out []WebSource
	for _, m := range fbGroupHref.FindAllStringSubmatch(pageHTML, -1) {
		id := m[1]
		if seen[id] || id == "feed" || id == "discover" || id == "joins" {
			continue
		}
		seen[id] = true
		out = append(out, WebSource{
			Platform: PlatformFacebook,
			Kind:     SourceKindGroup,
			SourceID: id,
			URL:      "https://www.facebook.com/groups/" + id + "/",
			Title:    strings.TrimSpace(html.UnescapeString(m[2])),
		})
	}
	return out
}

// ParseFacebookPosts extracts group posts (Story nodes) and their comments
// (Comment nodes) from feed and permalink GraphQL responses. A post shared
// into the group (attached_story) is neither, and a Story's own fields are
// looked up without descending into its comments, so a comment's text never
// becomes the post's.
func ParseFacebookPosts(captures []Capture, sourceID string) []WebPost {
	seen := map[string]bool{}
	var out []WebPost
	for _, doc := range decodeCaptureBodies(captures) {
		walkJSON(doc, "", func(m map[string]any, key string) bool {
			if key == "attached_story" {
				return false
			}
			switch jsonString(m, "__typename") {
			case "Story":
				if p, ok := facebookStory(m, sourceID); ok && !seen[p.PostID] {
					seen[p.PostID] = true
					out = append(out, p)
				}
			case "Comment":
				if p, ok := facebookComment(m, sourceID); ok && !seen["c"+p.PostID] {
					seen["c"+p.PostID] = true
					out = append(out, p)
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func facebookStory(m map[string]any, sourceID string) (WebPost, bool) {
	postID := jsonString(m, "post_id")
	var text, link string
	var created int64
	var actor map[string]any
	rootID := jsonString(m, "id")
	walkJSON(m, "", func(n map[string]any, key string) bool {
		switch jsonString(n, "__typename") {
		case "Comment":
			return false
		case "Story":
			// Comet nests the same story under comet_sections; a Story with
			// another id is a shared post and is not this one.
			if id := jsonString(n, "id"); id != "" && id != rootID {
				return false
			}
		}
		if key == "feedback" || key == "attached_story" {
			return false
		}
		if text == "" && key == "message" {
			text = jsonString(n, "text")
		}
		if created == 0 {
			created = jsonInt(n, "creation_time")
		}
		if link == "" {
			if u := jsonString(n, "url"); strings.Contains(u, "/posts/") || strings.Contains(u, "/permalink/") {
				link = u
			}
		}
		if actor == nil {
			if actors, ok := n["actors"].([]any); ok && len(actors) > 0 {
				actor, _ = actors[0].(map[string]any)
			}
		}
		return true
	})
	if postID == "" {
		postID = postIDFromURL(link)
	}
	if postID == "" || actor == nil || jsonString(actor, "id") == "" {
		return WebPost{}, false
	}
	if sourceID == "" {
		sourceID = FacebookGroupID(link)
	}
	if link == "" && sourceID != "" {
		link = "https://www.facebook.com/groups/" + sourceID + "/posts/" + postID + "/"
	}
	return WebPost{
		Platform:  PlatformFacebook,
		Kind:      WebPostPost,
		SourceID:  sourceID,
		PostID:    postID,
		URL:       link,
		Author:    facebookAuthor(actor),
		Text:      text,
		CreatedAt: unixTime(created),
		Replies:   jsonInt(m, "feedback", "comment_count", "total_count"),
	}, true
}

func facebookComment(m map[string]any, sourceID string) (WebPost, bool) {
	id := jsonString(m, "legacy_fbid")
	if id == "" {
		id = jsonString(m, "id")
	}
	author, _ := m["author"].(map[string]any)
	if id == "" || author == nil || jsonString(author, "id") == "" {
		return WebPost{}, false
	}
	link := jsonString(m, "url")
	if sourceID == "" {
		sourceID = FacebookGroupID(link)
	}
	p := WebPost{
		Platform:  PlatformFacebook,
		Kind:      WebPostComment,
		SourceID:  sourceID,
		PostID:    id,
		URL:       link,
		Author:    facebookAuthor(author),
		Text:      jsonString(m, "body", "text"),
		CreatedAt: unixTime(jsonInt(m, "created_time")),
	}
	if parent := jsonString(m, "comment_parent", "legacy_fbid"); parent != "" {
		p.Kind = WebPostReply
		p.ParentID = parent
	} else if parent := jsonString(m, "comment_parent", "id"); parent != "" {
		p.Kind = WebPostReply
		p.ParentID = parent
	} else {
		p.ParentID = postIDFromURL(link)
	}
	return p, true
}

func facebookAuthor(a map[string]any) WebAuthor {
	author := WebAuthor{
		UserID:      jsonString(a, "id"),
		DisplayName: jsonString(a, "name"),
		ProfileURL:  jsonString(a, "url"),
	}
	if author.ProfileURL == "" && author.UserID != "" {
		author.ProfileURL = "https://www.facebook.com/profile.php?id=" + author.UserID
	}
	if u, err := url.Parse(author.ProfileURL); err == nil {
		name := strings.Trim(u.Path, "/")
		if name != "" && name != "profile.php" && !strings.Contains(name, "/") {
			author.Username = name
		}
	}
	return author
}

var fbPostIDInURL = regexp.MustCompile(`/(?:posts|permalink)/(\d+)`)

func postIDFromURL(u string) string {
	if m := fbPostIDInURL.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}
