package searchers

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
)

func TestBrave_Handle_SendsTheQueryAndReturnsTheResults(t *testing.T) {
	up := &searchersUpstream{}
	p := newTestProxy(t, "api.search.brave.com", up)
	b := NewBrave(&config.Config{BraveAPIKey: "test-key", ProxyURL: p.URL(), ExternalSSLCAPath: p.CACertPath()})
	if b.Engine() != database.SearchengineType("brave") {
		t.Errorf("Engine() = %q, want brave", b.Engine())
	}

	for _, tt := range []struct {
		name          string
		maxResults    int
		status        int
		body          string
		wantCount     string
		want          string
		wantRetryable bool
	}{
		{
			name:       "results come back numbered with the highlighting removed",
			maxResults: 2,
			status:     http.StatusOK,
			body: `{"web":{"results":[` +
				`{"title":"Best <strong>slots</strong> forum","url":"https://a.com/x","description":"Players &amp; <strong>slots</strong> talk"},` +
				`{"title":"Second","url":"https://b.com","description":"plain"}]}}`,
			wantCount: "2",
			want: "# 1. Best slots forum\n\n## URL\nhttps://a.com/x\n\n## Snippet\n\nPlayers & slots talk\n\n" +
				"# 2. Second\n\n## URL\nhttps://b.com\n\n## Snippet\n\nplain\n\n",
		},
		{
			name:       "an out-of-range count asks for the maximum",
			maxResults: 50,
			status:     http.StatusOK,
			body:       `{"web":{"results":[]}}`,
			wantCount:  "20",
		},
		{name: "an upstream 502 is retryable", maxResults: 5, status: http.StatusBadGateway, wantCount: "5", wantRetryable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			up.reset(tt.status, tt.body)

			got, err := b.Handle(t.Context(), Request{Query: "slots forum", MaxResults: tt.maxResults})

			if up.hits != 1 {
				t.Fatalf("upstream saw %d requests, want 1", up.hits)
			}
			if up.method != http.MethodGet || up.path != "/res/v1/web/search" {
				t.Errorf("request = %s %s, want GET /res/v1/web/search", up.method, up.path)
			}
			if key := up.header.Get("X-Subscription-Token"); key != "test-key" {
				t.Errorf("X-Subscription-Token = %q, want test-key", key)
			}
			if q := up.query.Get("q"); q != "slots forum" {
				t.Errorf("q = %q, want %q", q, "slots forum")
			}
			if c := up.query.Get("count"); c != tt.wantCount {
				t.Errorf("count = %q, want %q", c, tt.wantCount)
			}

			if tt.wantRetryable {
				if got != "" || !IsRetryable(err) {
					t.Fatalf("Handle() = %q, %v; want no result and a RetryableError", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Handle() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Handle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrave_ParseHTTPResponse_ClassifiesTheFailure(t *testing.T) {
	b := &brave{}

	for _, tt := range []struct {
		name          string
		status        int
		remaining     string
		reset         string
		body          string
		errContain    string
		wantRetryable bool
		wantWait      time.Duration
		wantSpent     bool
	}{
		{
			name: "the per-second limit is waited out", status: http.StatusTooManyRequests,
			remaining: "0, 950", reset: "1, 1300000",
			errContain: "HTTP 429", wantRetryable: true, wantWait: time.Second,
		},
		{
			name: "a 429 without rate-limit headers waits a second", status: http.StatusTooManyRequests,
			errContain: "HTTP 429", wantRetryable: true, wantWait: time.Second,
		},
		{
			name: "a spent month is not retried", status: http.StatusTooManyRequests,
			remaining: "1, 0", reset: "1, 1300000",
			errContain: "HTTP 429", wantSpent: true,
		},
		{name: "a rejected key is fatal", status: http.StatusUnauthorized, errContain: "HTTP 401"},
		{name: "a server error is retryable", status: http.StatusServiceUnavailable, errContain: "HTTP 503", wantRetryable: true},
		{name: "an undecodable body is fatal", status: http.StatusOK, body: "{invalid", errContain: "failed to decode response body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tt.body))}
			if tt.remaining != "" {
				resp.Header.Set("X-RateLimit-Remaining", tt.remaining)
				resp.Header.Set("X-RateLimit-Reset", tt.reset)
			}

			_, err := b.parseHTTPResponse(resp)

			if err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Fatalf("parseHTTPResponse() error = %v, want it to contain %q", err, tt.errContain)
			}
			if IsRetryable(err) != tt.wantRetryable || IsFatal(err) == tt.wantRetryable {
				t.Errorf("parseHTTPResponse() error = %T, want retryable=%v", err, tt.wantRetryable)
			}
			if errors.Is(err, errBraveQuotaSpent) != tt.wantSpent {
				t.Errorf("errors.Is(err, errBraveQuotaSpent) = %v, want %v", !tt.wantSpent, tt.wantSpent)
			}
			var retry *RetryableError
			if errors.As(err, &retry) && retry.RetryAfter != tt.wantWait {
				t.Errorf("RetryAfter = %v, want %v", retry.RetryAfter, tt.wantWait)
			}
		})
	}
}

func TestBrave_IsAvailable_RequiresAnAPIKey(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"available when API key is set", &config.Config{BraveAPIKey: "test-key"}, true},
		{"unavailable when API key is empty", &config.Config{}, false},
		{"unavailable when nil config", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &brave{cfg: tt.cfg}
			if got := b.IsAvailable(); got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}
