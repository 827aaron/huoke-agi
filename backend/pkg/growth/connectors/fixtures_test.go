package connectors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePage is what the fake browser service shows for a URL: the captures of
// the navigation (Steps[0]) and of each later scroll (Steps[1:]), and the page
// HTML. The feed ends when the scrolls run out.
type fakePage struct {
	Steps [][]Capture
	HTML  string
}

// fakeBrowser stands in for services/browser/: it answers the same routes and
// error envelopes from scripted pages.
type fakeBrowser struct {
	t      *testing.T
	server *httptest.Server
	token  string

	mu      sync.Mutex
	pages   map[string]*fakePage
	current string
	step    int
	calls   []string
	bodies  map[string]map[string]any
	// fail, when set for an action, is returned instead of running it.
	fail map[string]fakeFailure
	// onClick changes the page in response to a click on the named target.
	onClick func(target map[string]any, page *fakePage)
}

type fakeFailure struct {
	status int
	body   string
}

func newFakeBrowser(t *testing.T) *fakeBrowser {
	t.Helper()
	fb := &fakeBrowser{t: t, token: "svc-token", pages: map[string]*fakePage{}, bodies: map[string]map[string]any{}, fail: map[string]fakeFailure{}}
	fb.server = httptest.NewServer(http.HandlerFunc(fb.handle))
	t.Cleanup(fb.server.Close)
	return fb
}

func (fb *fakeBrowser) client(t *testing.T, cfg BrowserConfig) *BrowserClient {
	t.Helper()
	cfg.BaseURL = fb.server.URL
	cfg.Token = fb.token
	c, err := NewBrowserClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (fb *fakeBrowser) page(url string, p *fakePage) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.pages[url] = p
}

func (fb *fakeBrowser) callLog() []string {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return append([]string(nil), fb.calls...)
}

func (fb *fakeBrowser) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+fb.token {
		writeJSON(w, 401, map[string]any{"error": map[string]any{"code": "unauthorized", "message": "wrong token"}})
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	action := parts[1]
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	fb.mu.Lock()
	defer fb.mu.Unlock()
	entry := action
	if u, ok := body["url"].(string); ok {
		entry += " " + u
	}
	fb.calls = append(fb.calls, entry)
	fb.bodies[r.URL.Path] = body
	if f, ok := fb.fail[action]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
		return
	}

	switch action {
	case "navigate":
		u, _ := body["url"].(string)
		p, ok := fb.pages[u]
		if !ok {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found", "message": u + " returned 404"}})
			return
		}
		fb.current, fb.step = u, 0
		writeJSON(w, 200, BrowserPage{URL: u, HTTPStatus: 200, Captures: stepCaptures(p, 0), ReachedEnd: len(p.Steps) <= 1})
	case "scroll":
		p := fb.pages[fb.current]
		fb.step++
		writeJSON(w, 200, BrowserPage{URL: fb.current, Captures: stepCaptures(p, fb.step), ReachedEnd: fb.step >= len(p.Steps)-1})
	case "content":
		writeJSON(w, 200, BrowserContent{URL: fb.current, Format: "html", Body: fb.pages[fb.current].HTML})
	case "click":
		p := fb.pages[fb.current]
		if fb.onClick == nil || !strings.Contains(p.HTML, `aria-label="`+str(body["name"])+`"`) {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found", "message": "nothing on the page matches the click target"}})
			return
		}
		fb.onClick(body, p)
		writeJSON(w, 200, BrowserPage{URL: fb.current})
	case "health":
		writeJSON(w, 200, map[string]any{"account_id": parts[0], "platform": body["platform"], "state": "ok"})
	default:
		http.NotFound(w, r)
	}
}

func stepCaptures(p *fakePage, step int) []Capture {
	if step < len(p.Steps) {
		return p.Steps[step]
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sample is a saved response body under testdata/, as one capture.
func sample(t *testing.T, name, url string) []Capture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return []Capture{{URL: url, Status: 200, Body: string(raw)}}
}

func ts(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

func (fb *fakeBrowser) lastBody(path string) map[string]any {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.bodies[path]
}

func messageIDs(page HistoryPage) []int64 {
	ids := []int64{}
	for _, m := range page.Messages {
		ids = append(ids, m.ID)
	}
	return ids
}

// resetSharedBrowser makes the factories build their client from the
// environment the test set.
func resetSharedBrowser(t *testing.T) {
	t.Helper()
	sharedBrowserMu.Lock()
	sharedBrowser = nil
	sharedBrowserMu.Unlock()
	t.Cleanup(func() {
		sharedBrowserMu.Lock()
		sharedBrowser = nil
		sharedBrowserMu.Unlock()
	})
}
