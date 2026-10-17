package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"
)

// WebErrorCode is the error vocabulary of the browser service in
// services/browser/ and of the YouTube API client. The connectors turn it into
// an Outcome (webOutcome) before anything reaches a caller of Connector.
type WebErrorCode string

const (
	WebErrNeedsLogin    WebErrorCode = "needs_login"
	WebErrRateLimited   WebErrorCode = "rate_limited"
	WebErrNotFound      WebErrorCode = "not_found"
	WebErrUnavailable   WebErrorCode = "unavailable"
	WebErrBadRequest    WebErrorCode = "bad_request"
	WebErrUnauthorized  WebErrorCode = "unauthorized"
	WebErrNotConfigured WebErrorCode = "not_configured"
	WebErrTimeout       WebErrorCode = "timeout"
	WebErrInternal      WebErrorCode = "internal"
)

// WebError carries a code a tool can branch on. RetryAfter is set for
// rate_limited and is how long the platform, or our backoff default, asks to wait.
type WebError struct {
	Code       WebErrorCode
	Message    string
	RetryAfter time.Duration
	AccountID  string

	// quotaReset marks a YouTube key out of daily quota, as opposed to a
	// per-minute limit another key would hit just the same.
	quotaReset bool
}

func (e *WebError) Error() string {
	msg := string(e.Code)
	if e.AccountID != "" {
		msg += " (account " + e.AccountID + ")"
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(", retry after %s", e.RetryAfter)
	}
	return msg
}

// Is matches on the code alone, so errors.Is(err, ErrWebNeedsLogin) works for
// any account and message.
func (e *WebError) Is(target error) bool {
	var t *WebError
	return errors.As(target, &t) && t.Code == e.Code && t.Message == "" && t.AccountID == ""
}

var (
	ErrWebNeedsLogin    = &WebError{Code: WebErrNeedsLogin}
	ErrWebRateLimited   = &WebError{Code: WebErrRateLimited}
	ErrWebNotFound      = &WebError{Code: WebErrNotFound}
	ErrWebUnavailable   = &WebError{Code: WebErrUnavailable}
	ErrWebNotConfigured = &WebError{Code: WebErrNotConfigured}
)

// WebErrorCodeOf returns the code of a WebError anywhere in err's chain, or "".
func WebErrorCodeOf(err error) WebErrorCode {
	var we *WebError
	if errors.As(err, &we) {
		return we.Code
	}
	return ""
}

const (
	PlatformFacebook  Platform = "facebook"
	PlatformInstagram Platform = "instagram"
	PlatformYouTube   Platform = "youtube"
)

const (
	SourceHashtag SourceKind = "hashtag"
	SourceProfile SourceKind = "profile"
	SourcePost    SourceKind = "post"
	SourceVideo   SourceKind = "video"
)

// WebSource is a place leads are read from: a group, a hashtag, a profile, a
// channel or a single video. SourceID is stable per platform.
type WebSource struct {
	Platform    Platform   `json:"platform"`
	Kind        SourceKind `json:"kind"`
	SourceID    string     `json:"source_id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Members     int64      `json:"members,omitempty"`
	Private     bool       `json:"private,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// WebAuthor identifies a person by the platform's stable id. Username and
// DisplayName can change; UserID cannot.
type WebAuthor struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	ProfileURL  string `json:"profile_url,omitempty"`
}

type WebPostKind string

const (
	WebPostPost    WebPostKind = "post"
	WebPostComment WebPostKind = "comment"
	WebPostReply   WebPostKind = "reply"
)

// WebPost is one piece of public speech with what a lead's evidence needs:
// who, what, when and a link back to it.
type WebPost struct {
	Platform  Platform    `json:"platform"`
	Kind      WebPostKind `json:"kind"`
	SourceID  string      `json:"source_id"`
	PostID    string      `json:"post_id"`
	ParentID  string      `json:"parent_id,omitempty"`
	URL       string      `json:"url"`
	Author    WebAuthor   `json:"author"`
	Text      string      `json:"text"`
	CreatedAt time.Time   `json:"created_at"`
	Likes     int64       `json:"likes,omitempty"`
	Replies   int64       `json:"replies,omitempty"`
}

// WebPage is one batch of a read. Cursor is opaque; passing it back to the
// same read continues from where this one stopped.
type WebPage struct {
	Posts  []WebPost `json:"posts"`
	Cursor string    `json:"cursor,omitempty"`
	Done   bool      `json:"done"`
}

type WebJoinResult struct {
	State  JoinStatus `json:"state"`
	Reason string     `json:"reason,omitempty"`
}

// BrowserConfig points at services/browser/. MaxInlineBackoff is the longest
// rate-limit wait the client sits out itself before retrying once; the
// default, zero, never retries and hands rate_limited straight back.
type BrowserConfig struct {
	BaseURL          string
	Token            string
	HTTPClient       *http.Client
	MaxInlineBackoff time.Duration
}

type BrowserClient struct {
	cfg  BrowserConfig
	base *url.URL
	http *http.Client

	mu       sync.Mutex
	profiles map[string]BrowserProfile
}

// BrowserProfile is how one account reaches the platform. Proxy is http://,
// https:// or socks5:// with optional user:pass@ (socks5 only without them,
// which Chromium cannot send); empty connects directly. Region is the proxy's
// ISO country, used only when the service generates the account's first
// fingerprint. Fingerprint is one the backend stored earlier, from
// BrowserAccount.Fingerprint; nil keeps whatever the service holds.
// ExternalBrowserWS is a CDP endpoint of a fingerprint browser such as AdsPower
// or BitBrowser, which then owns proxy and fingerprint itself.
type BrowserProfile struct {
	Proxy             string
	Region            string
	Fingerprint       json.RawMessage
	ExternalBrowserWS string
}

func (p BrowserProfile) apply(body map[string]any) {
	body["proxy"] = p.Proxy
	body["external_browser_ws"] = p.ExternalBrowserWS
	if p.Region != "" {
		body["region"] = p.Region
	}
	if len(p.Fingerprint) > 0 && string(p.Fingerprint) != "null" {
		body["fingerprint"] = p.Fingerprint
	}
}

// SetProfile makes every later call for the account carry its profile, so the
// service launches the browser (operator login window included) with it. A
// change closes the account's running browser and relaunches it on next use.
func (c *BrowserClient) SetProfile(account string, p BrowserProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profiles == nil {
		c.profiles = map[string]BrowserProfile{}
	}
	c.profiles[account] = p
}

func (c *BrowserClient) profile(account string) (BrowserProfile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.profiles[account]
	return p, ok
}

// Configure sends the account's profile without touching its browser and
// returns the account, whose Fingerprint the backend stores.
func (c *BrowserClient) Configure(ctx context.Context, account string, platform Platform) (*BrowserAccount, error) {
	var out BrowserAccount
	return &out, c.post(ctx, account, "config", map[string]any{"platform": platform}, &out)
}

func NewBrowserClient(cfg BrowserConfig) (*BrowserClient, error) {
	if cfg.BaseURL == "" {
		return nil, &WebError{Code: WebErrNotConfigured, Message: "browser service URL is empty"}
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse browser service URL: %w", err)
	}
	client := cfg.HTTPClient
	if client == nil {
		// Navigations wait for the page and the service paces each action, so
		// a call can legitimately take minutes.
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &BrowserClient{cfg: cfg, base: base, http: client}, nil
}

// Capture is a response the page itself fetched (GraphQL, /api/v1/), which is
// steadier to parse than the obfuscated DOM.
type Capture struct {
	Seq       int64  `json:"seq"`
	URL       string `json:"url"`
	Status    int    `json:"status"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated"`
}

type BrowserPage struct {
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	HTTPStatus int       `json:"http_status"`
	Captures   []Capture `json:"captures"`
	Cursor     int64     `json:"cursor"`
	ReachedEnd bool      `json:"reached_end"`
}

type BrowserContent struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Format string `json:"format"`
	Body   string `json:"body"`
}

// BrowserAccount is the service's view of one account profile. State is new
// (never logged in), ok or needs_login.
type BrowserAccount struct {
	AccountID        string     `json:"account_id"`
	Platform         string     `json:"platform"`
	State            string     `json:"state"`
	StateReason      string     `json:"state_reason"`
	Reason           string     `json:"reason,omitempty"`
	LoginConfirmedAt *time.Time `json:"login_confirmed_at"`
	CooldownUntil    *time.Time `json:"cooldown_until"`
	WarmingUp        bool       `json:"warming_up"`
	Open             bool       `json:"open"`
	URL              *string    `json:"url"`
	// Proxy has its credentials masked.
	Proxy           string          `json:"proxy"`
	ExternalBrowser bool            `json:"external_browser"`
	Fingerprint     json.RawMessage `json:"fingerprint"`
}

// LoginSession is an operator's way into an account's browser. LoginURL opens
// a page showing the browser that forwards clicks and keys; it carries a
// ticket good for that account's login page only, until ExpiresAt. It is
// relative to the service unless the service has BROWSER_PUBLIC_URL set.
type LoginSession struct {
	BrowserAccount
	LoginURL  string    `json:"login_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Target picks an element by CSS selector, by ARIA role and accessible name,
// or by visible text. Role and text survive the platforms' class renames.
type Target struct {
	Selector string `json:"selector,omitempty"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Exact    bool   `json:"exact,omitempty"`
}

func (c *BrowserClient) Navigate(ctx context.Context, account string, platform Platform, pageURL string, wait time.Duration) (*BrowserPage, error) {
	var out BrowserPage
	body := map[string]any{"platform": platform, "url": pageURL, "wait_ms": wait.Milliseconds()}
	return &out, c.post(ctx, account, "navigate", body, &out)
}

func (c *BrowserClient) Scroll(ctx context.Context, account string, times int, wait time.Duration) (*BrowserPage, error) {
	var out BrowserPage
	body := map[string]any{"times": times, "wait_ms": wait.Milliseconds()}
	return &out, c.post(ctx, account, "scroll", body, &out)
}

// Content returns the page as "html" or "text"; selector narrows it to the
// first matching element.
func (c *BrowserClient) Content(ctx context.Context, account, format, selector string) (*BrowserContent, error) {
	var out BrowserContent
	body := map[string]any{"format": format, "selector": selector}
	return &out, c.post(ctx, account, "content", body, &out)
}

func (c *BrowserClient) Click(ctx context.Context, account string, target Target, wait time.Duration) (*BrowserPage, error) {
	var out BrowserPage
	body := targetBody(target)
	body["wait_ms"] = wait.Milliseconds()
	return &out, c.post(ctx, account, "click", body, &out)
}

func (c *BrowserClient) Type(ctx context.Context, account string, target Target, text string, submit bool) (*BrowserPage, error) {
	var out BrowserPage
	body := targetBody(target)
	body["text"] = text
	body["submit"] = submit
	return &out, c.post(ctx, account, "type", body, &out)
}

func (c *BrowserClient) Captures(ctx context.Context, account string, since int64) (*BrowserPage, error) {
	var out BrowserPage
	return &out, c.post(ctx, account, "captures", map[string]any{"since": since}, &out)
}

// Health opens the platform's home page if the account's browser is idle and
// reports whether it is still logged in. It also creates the account's profile
// the first time, which is why it takes the platform.
func (c *BrowserClient) Health(ctx context.Context, account string, platform Platform) (*BrowserAccount, error) {
	var out BrowserAccount
	return &out, c.post(ctx, account, "health", map[string]any{"platform": platform}, &out)
}

// Account reads the service's record of an account without touching its
// browser, for polling after an operator was sent to log in.
func (c *BrowserClient) Account(ctx context.Context, account string) (*BrowserAccount, error) {
	var out BrowserAccount
	return &out, c.call(ctx, http.MethodGet, account, "", nil, &out)
}

// StartLogin opens the platform's login page in the account's browser (or
// loginURL when given) and returns the link an operator opens to log in and
// pass any verification. Nothing here fills a form or a code.
func (c *BrowserClient) StartLogin(ctx context.Context, account string, platform Platform, loginURL string) (*LoginSession, error) {
	var out LoginSession
	body := map[string]any{"platform": platform}
	if loginURL != "" {
		body["url"] = loginURL
	}
	return &out, c.post(ctx, account, "login/start", body, &out)
}

// ConfirmLogin checks the page the operator left the browser on. When it is
// logged in the account becomes active, the login link stops working, the
// browser closes so the profile is written, and work resumes on the next call;
// otherwise the account keeps its state and Reason says what the page shows.
func (c *BrowserClient) ConfirmLogin(ctx context.Context, account string) (*BrowserAccount, error) {
	var out BrowserAccount
	return &out, c.post(ctx, account, "login/confirm", map[string]any{}, &out)
}

// CancelLogin ends an operator session without changing the account: the
// login link stops working and the browser closes.
func (c *BrowserClient) CancelLogin(ctx context.Context, account string) (*BrowserAccount, error) {
	var out BrowserAccount
	return &out, c.post(ctx, account, "login/cancel", map[string]any{}, &out)
}

func (c *BrowserClient) Close(ctx context.Context, account string) error {
	return c.post(ctx, account, "close", map[string]any{}, nil)
}

// LoginURL is the operator page authorised by the service token itself. Prefer
// StartLogin, whose link holds a short-lived ticket instead.
func (c *BrowserClient) LoginURL(account string, platform Platform) string {
	u := c.endpoint("login", account)
	q := url.Values{}
	if platform != "" {
		q.Set("platform", string(platform))
	}
	if c.cfg.Token != "" {
		q.Set("token", c.cfg.Token)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// endpoint appends path segments to the base URL, escaping each one so an
// account id cannot reach another route.
func (c *BrowserClient) endpoint(segments ...string) url.URL {
	u := *c.base
	path := strings.TrimRight(u.Path, "/")
	raw := strings.TrimRight(u.EscapedPath(), "/")
	for _, s := range segments {
		path += "/" + s
		raw += "/" + url.PathEscape(s)
	}
	u.Path, u.RawPath = path, raw
	return u
}

func targetBody(t Target) map[string]any {
	body := map[string]any{}
	if t.Selector != "" {
		body["selector"] = t.Selector
	}
	if t.Role != "" {
		body["role"] = t.Role
	}
	if t.Name != "" {
		body["name"] = t.Name
	}
	if t.Text != "" {
		body["text"] = t.Text
	}
	if t.Exact {
		body["exact"] = true
	}
	return body
}

func (c *BrowserClient) post(ctx context.Context, account, action string, body map[string]any, out any) error {
	if p, ok := c.profile(account); ok {
		p.apply(body)
	}
	err := c.call(ctx, http.MethodPost, account, action, body, out)
	var we *WebError
	if errors.As(err, &we) && we.Code == WebErrRateLimited && we.RetryAfter > 0 && we.RetryAfter <= c.cfg.MaxInlineBackoff {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(we.RetryAfter):
		}
		err = c.call(ctx, http.MethodPost, account, action, body, out)
	}
	return err
}

func (c *BrowserClient) call(ctx context.Context, method, account, action string, body map[string]any, out any) error {
	segments := []string{"sessions", account}
	if action != "" {
		segments = append(segments, strings.Split(action, "/")...)
	}
	u := c.endpoint(segments...)
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", action, err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &WebError{Code: WebErrInternal, Message: "browser service unreachable: " + err.Error(), AccountID: account}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", action, err)
	}
	if resp.StatusCode >= 400 {
		return decodeServiceError(resp, raw, account)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s response: %w", action, err)
	}
	return nil
}

func decodeServiceError(resp *http.Response, raw []byte, account string) *WebError {
	var envelope struct {
		Error struct {
			Code          string  `json:"code"`
			Message       string  `json:"message"`
			RetryAfterSec float64 `json:"retry_after_sec"`
		} `json:"error"`
	}
	we := &WebError{AccountID: account}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error.Code != "" {
		we.Code = WebErrorCode(envelope.Error.Code)
		we.Message = envelope.Error.Message
		we.RetryAfter = time.Duration(envelope.Error.RetryAfterSec * float64(time.Second))
	} else {
		we.Code = WebErrInternal
		we.Message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if we.RetryAfter == 0 {
		if sec, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			we.RetryAfter = time.Duration(sec) * time.Second
		}
	}
	return we
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// contractError turns a browser-service or API failure into the errors the
// growth service acts on: ErrUnauthorized moves the account to needs_login,
// FloodWaitError rests it until the wait is over, ErrNotFound marks the source.
// Every other error is returned as it is and counted against the account.
func contractError(err error) error {
	var we *WebError
	if err == nil || !errors.As(err, &we) {
		return err
	}
	switch we.Code {
	case WebErrNeedsLogin:
		return fmt.Errorf("%w: %s", ErrUnauthorized, we.Message)
	case WebErrRateLimited:
		wait := we.RetryAfter
		if wait <= 0 {
			wait = time.Minute
		}
		return &FloodWaitError{Wait: wait}
	case WebErrNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, we.Message)
	}
	return err
}

func (s WebSource) source() Source {
	return Source{
		ExternalID:  s.SourceID,
		Title:       s.Title,
		Link:        s.URL,
		Kind:        s.Kind,
		MemberCount: s.Members,
		Description: s.Description,
	}
}

func webSources(in []WebSource, limit int) []Source {
	if limit > 0 && len(in) > limit {
		in = in[:limit]
	}
	out := make([]Source, 0, len(in))
	for _, s := range in {
		out = append(out, s.source())
	}
	return out
}

// refString picks what a web connector can address a source by: the link,
// which keeps a vanity name, else the id it reported.
func refString(ref SourceRef) string {
	if ref.Link != "" {
		return ref.Link
	}
	if ref.ExternalID != "" {
		return ref.ExternalID
	}
	return ref.Username
}

// messageID maps a post onto the int64 ids the history cursors compare: its
// time in milliseconds, times a thousand, plus a hash of the post id to keep
// two posts of the same millisecond apart. Older posts get smaller ids, so
// OffsetID and MinID work as time cursors.
func messageID(p WebPost) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(p.PostID))
	return p.CreatedAt.UnixMilli()*1000 + int64(h.Sum32()%1000)
}

// historyPage answers a HistoryRequest from the posts one read loaded. Web
// feeds cannot be entered at an older point, so an older-direction read returns
// what the loaded window holds below OffsetID and is exhausted once that is
// all of it. A newer-direction read returns the posts just above MinID, so a
// busy source is caught up over several reads rather than skipped. ReplyToID
// is set when the parent was loaded by the same read.
func historyPage(posts []WebPost, req HistoryRequest, limit int) HistoryPage {
	if req.Limit > 0 && req.Limit < limit {
		limit = req.Limit
	}
	ids := make(map[string]int64, len(posts))
	var kept []WebPost
	for _, p := range posts {
		id := messageID(p)
		ids[p.PostID] = id
		if req.OffsetID != 0 && id >= req.OffsetID {
			continue
		}
		if id <= req.MinID {
			continue
		}
		kept = append(kept, p)
	}
	newerFirst := func(i, j int) bool { return ids[kept[i].PostID] > ids[kept[j].PostID] }
	sort.SliceStable(kept, newerFirst)
	page := HistoryPage{Messages: []Message{}}
	if limit > 0 && len(kept) > limit {
		if req.MinID != 0 {
			kept = kept[len(kept)-limit:]
		} else {
			kept = kept[:limit]
		}
	} else {
		page.Exhausted = req.MinID == 0
	}
	for _, p := range kept {
		msg := Message{
			ID:             ids[p.PostID],
			SenderID:       p.Author.UserID,
			SenderUsername: p.Author.Username,
			SenderName:     p.Author.DisplayName,
			Text:           p.Text,
			Time:           p.CreatedAt,
			Link:           p.URL,
		}
		if p.Kind != WebPostPost {
			msg.ReplyToID = ids[p.ParentID]
		}
		page.Messages = append(page.Messages, msg)
	}
	return page
}

// sinceCursor turns MinID back into the time cursor the readers stop at, a
// second early so a post of that same second is still loaded; historyPage then
// filters exactly.
func sinceCursor(minID int64) string {
	if minID <= 0 {
		return ""
	}
	return strconv.FormatInt(minID/1000/1000-1, 10)
}

func unsupported(what string) error { return fmt.Errorf("%w: %s", ErrUnsupported, what) }

// browserProfile carries the account's resolved route and fingerprint to the
// browser service.
func browserProfile(acc Account) BrowserProfile {
	return BrowserProfile{Proxy: acc.Proxy, Fingerprint: acc.Fingerprint, ExternalBrowserWS: acc.ExternalBrowserWS}
}

// BrowserAccountID is the account's name in the browser service, which keeps
// one profile per name.
func BrowserAccountID(platform Platform, accountID int64) string {
	return fmt.Sprintf("%s-%d", platform, accountID)
}

// browserRemoteLogin is the operator sign-in of a browser_session account:
// the service's mirrored login page, opened in the account's own browser so
// login and later automation share one proxy and fingerprint.
type browserRemoteLogin struct {
	platform Platform
}

func (l browserRemoteLogin) client(acc Account) (*BrowserClient, string, error) {
	browser, err := browserFromEnv()
	if err != nil {
		return nil, "", err
	}
	name := BrowserAccountID(l.platform, acc.ID)
	browser.SetProfile(name, browserProfile(acc))
	return browser, name, nil
}

func (l browserRemoteLogin) Start(ctx context.Context, _ *config.Config, acc Account) (RemoteLoginView, error) {
	browser, name, err := l.client(acc)
	if err != nil {
		return RemoteLoginView{}, err
	}
	login, err := browser.StartLogin(ctx, name, l.platform, "")
	if err != nil {
		return RemoteLoginView{}, err
	}
	view := RemoteLoginView{ViewURL: login.LoginURL, ExpiresAt: login.ExpiresAt}
	if len(login.Fingerprint) > 0 && string(login.Fingerprint) != "null" {
		view.Fingerprint = login.Fingerprint
	}
	return view, nil
}

func (l browserRemoteLogin) Finish(ctx context.Context, _ *config.Config, acc Account) (bool, string, error) {
	browser, name, err := l.client(acc)
	if err != nil {
		return false, "", err
	}
	res, err := browser.ConfirmLogin(ctx, name)
	if err != nil {
		return false, "", err
	}
	if res.State == "ok" {
		return true, "", nil
	}
	reason := res.Reason
	if reason == "" {
		reason = res.StateReason
	}
	return false, reason, nil
}

func (l browserRemoteLogin) Cancel(ctx context.Context, _ *config.Config, acc Account) error {
	browser, name, err := l.client(acc)
	if err != nil {
		return err
	}
	_, err = browser.CancelLogin(ctx, name)
	return err
}

var (
	sharedBrowserMu sync.Mutex
	sharedBrowser   *BrowserClient
)

// browserFromEnv is the one client of the browser service the factories
// share. BROWSER_SERVICE_URL and BROWSER_TOKEN are read from the environment
// until the config carries them.
func browserFromEnv() (*BrowserClient, error) {
	sharedBrowserMu.Lock()
	defer sharedBrowserMu.Unlock()
	if sharedBrowser != nil {
		return sharedBrowser, nil
	}
	c, err := NewBrowserClient(BrowserConfig{BaseURL: os.Getenv("BROWSER_SERVICE_URL"), Token: os.Getenv("BROWSER_TOKEN")})
	if err != nil {
		return nil, err
	}
	sharedBrowser = c
	return c, nil
}

// decodeCaptureBodies turns captured bodies into JSON values. Facebook prefixes
// some responses with "for (;;);" and streams several documents in one body,
// one per line; a body that is not JSON is skipped.
func decodeCaptureBodies(captures []Capture) []any {
	var docs []any
	for _, c := range captures {
		body := strings.TrimSpace(c.Body)
		body = strings.TrimPrefix(body, "for (;;);")
		dec := json.NewDecoder(strings.NewReader(body))
		dec.UseNumber()
		for {
			var v any
			if err := dec.Decode(&v); err != nil {
				break
			}
			docs = append(docs, v)
		}
	}
	return docs
}

// walkJSON calls fn for every object in v, depth first, with the key it sits
// under (the array's key for array elements). fn returns false to skip the
// object's children.
func walkJSON(v any, key string, fn func(m map[string]any, key string) bool) {
	switch t := v.(type) {
	case map[string]any:
		if !fn(t, key) {
			return
		}
		for k, child := range t {
			walkJSON(child, k, fn)
		}
	case []any:
		for _, child := range t {
			walkJSON(child, key, fn)
		}
	}
}

// jsonPath follows keys through nested objects.
func jsonPath(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func jsonString(v any, keys ...string) string {
	switch t := jsonPath(v, keys...).(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func jsonInt(v any, keys ...string) int64 {
	switch t := jsonPath(v, keys...).(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
	case float64:
		return int64(t)
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// newerThan keeps posts created strictly after since; a zero since keeps all.
func newerThan(posts []WebPost, since time.Time) []WebPost {
	if since.IsZero() {
		return posts
	}
	out := posts[:0:0]
	for _, p := range posts {
		if p.CreatedAt.After(since) {
			out = append(out, p)
		}
	}
	return out
}

// timeCursor encodes the newest creation time seen, the incremental-sync
// cursor of the browser connectors.
func timeCursor(posts []WebPost, prev time.Time) string {
	newest := prev
	for _, p := range posts {
		if p.CreatedAt.After(newest) {
			newest = p.CreatedAt
		}
	}
	if newest.IsZero() {
		return ""
	}
	return strconv.FormatInt(newest.Unix(), 10)
}

func parseTimeCursor(cursor string) (time.Time, error) {
	if cursor == "" {
		return time.Time{}, nil
	}
	sec, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil {
		return time.Time{}, &WebError{Code: WebErrBadRequest, Message: "cursor is not one this connector returned: " + cursor}
	}
	return unixTime(sec), nil
}

func oldest(posts []WebPost) time.Time {
	var t time.Time
	for _, p := range posts {
		if !p.CreatedAt.IsZero() && (t.IsZero() || p.CreatedAt.Before(t)) {
			t = p.CreatedAt
		}
	}
	return t
}
