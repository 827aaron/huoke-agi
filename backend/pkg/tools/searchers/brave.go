package searchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"
	"pentagi/pkg/system"

	"github.com/sirupsen/logrus"
)

const (
	braveURL        = "https://api.search.brave.com/res/v1/web/search"
	braveMaxResults = 20

	// braveMaxThrottleWait bounds the wait a 429 asks for: a longer reset belongs to the
	// monthly window, which no retry within one query can outlast.
	braveMaxThrottleWait = time.Minute
)

// errBraveQuotaSpent is a 429 whose exhausted window resets in more than braveMaxThrottleWait,
// meaning the key's monthly credit is used up. Its text is a quota marker for key rotation.
var errBraveQuotaSpent = errors.New("brave search monthly quota is used up for this key")

var braveTag = regexp.MustCompile(`<[^>]*>`)

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

type brave struct {
	cfg *config.Config
}

func NewBrave(cfg *config.Config) Searcher {
	return &brave{cfg: cfg}
}

func (b *brave) Engine() database.SearchengineType {
	return database.SearchengineTypeBrave
}

func (b *brave) Handle(ctx context.Context, req Request) (string, error) {
	if !b.IsAvailable() {
		return "", ErrNotConfigured
	}

	ctx, observation := obs.Observer.NewObservation(ctx)

	numResults := req.MaxResults
	if numResults < 1 || numResults > braveMaxResults {
		numResults = braveMaxResults
	}

	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"engine":      "brave",
		"query":       req.Query[:min(len(req.Query), 1000)],
		"num_results": numResults,
	})

	result, err := b.search(ctx, req.Query, numResults)
	if err != nil {
		observation.Event(
			langfuse.WithEventName("search engine error"),
			langfuse.WithEventInput(req.Query),
			langfuse.WithEventStatus(err.Error()),
			langfuse.WithEventLevel(langfuse.ObservationLevelWarning),
			langfuse.WithEventMetadata(langfuse.Metadata{
				"engine":      "brave",
				"query":       req.Query,
				"max_results": numResults,
				"error":       err.Error(),
			}),
		)

		obs.LogErrorOrCancel(logger, err, "failed to search in brave")
		return "", err
	}

	return result, nil
}

func (b *brave) search(ctx context.Context, query string, numResults int) (string, error) {
	client, err := system.GetHTTPClient(b.cfg)
	if err != nil {
		return "", Fatal(fmt.Errorf("failed to create http client: %w", err))
	}

	q := url.Values{"q": {query}, "count": {strconv.Itoa(numResults)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, braveURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", Fatal(fmt.Errorf("failed to build request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey())

	resp, err := client.Do(req)
	if err != nil {
		return "", Retryable(fmt.Errorf("failed to do request: %w", err), 0)
	}
	defer resp.Body.Close()

	return b.parseHTTPResponse(resp)
}

func (b *brave) parseHTTPResponse(resp *http.Response) (string, error) {
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", braveThrottleError(resp.Header)
	}
	if resp.StatusCode != http.StatusOK {
		return "", ClassifyHTTPStatus(resp.StatusCode, "brave search failed")
	}

	var body braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", Fatal(fmt.Errorf("failed to decode response body: %w", err))
	}

	var writer strings.Builder
	for i, item := range body.Web.Results {
		fmt.Fprintf(&writer, "# %d. %s\n\n", i+1, braveText(item.Title))
		fmt.Fprintf(&writer, "## URL\n%s\n\n", item.URL)
		fmt.Fprintf(&writer, "## Snippet\n\n%s\n\n", braveText(item.Description))
	}
	return writer.String(), nil
}

// braveText drops the <strong> highlighting Brave puts around matched words.
func braveText(s string) string {
	return html.UnescapeString(braveTag.ReplaceAllString(s, ""))
}

// braveThrottleError reads which window a 429 exhausted. Brave sends X-RateLimit-Remaining
// and X-RateLimit-Reset as aligned comma-separated lists, one value per window (per second,
// per month), the reset in seconds. A per-second limit is waited out; a spent month is not.
func braveThrottleError(h http.Header) error {
	remaining := strings.Split(h.Get("X-RateLimit-Remaining"), ",")
	reset := strings.Split(h.Get("X-RateLimit-Reset"), ",")

	wait := time.Second
	for i := range min(len(remaining), len(reset)) {
		left, err1 := strconv.Atoi(strings.TrimSpace(remaining[i]))
		secs, err2 := strconv.Atoi(strings.TrimSpace(reset[i]))
		if err1 != nil || err2 != nil || left > 0 {
			continue
		}
		window := time.Duration(secs) * time.Second
		if window > braveMaxThrottleWait {
			return Fatal(fmt.Errorf("%w; it resets in %s (HTTP 429)", errBraveQuotaSpent, window.Round(time.Hour)))
		}
		wait = max(wait, window)
	}
	return Retryable(fmt.Errorf("brave search is rate-limiting this key (HTTP 429)"), wait)
}

func (b *brave) IsAvailable() bool {
	return b.apiKey() != ""
}

func (b *brave) apiKey() string {
	if b.cfg == nil {
		return ""
	}

	return b.cfg.BraveAPIKey
}
