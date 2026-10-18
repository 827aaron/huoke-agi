package searchers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"
	"pentagi/pkg/system"

	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

const (
	duckduckgoMaxResults  = 10
	duckduckgoMaxAttempts = 4
	duckduckgoTimeout     = 30 * time.Second
	duckduckgoUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// duckduckgoEndpoints are tried in turn: when DuckDuckGo throttles an IP on one front end the
// other one often still answers. Both take the same form fields.
var duckduckgoEndpoints = []struct{ origin, path string }{
	{"https://html.duckduckgo.com", "/html/"},
	{"https://lite.duckduckgo.com", "/lite/"},
}

// errDuckduckgoThrottled is DuckDuckGo refusing this IP: it answers 202 (or 403/429) with a
// bot-check page instead of results, and a different query will not help.
var errDuckduckgoThrottled = errors.New("DuckDuckGo is rate-limiting this server's IP and answered " +
	"with a bot check instead of results; route search through another IP with PROXY_URL, or add " +
	"another search engine (Google, Tavily, Perplexity, Firecrawl, Traversaal) as a fallback")

// Region constants for DuckDuckGo search
const (
	RegionUS = "us-en" // USA
	RegionUK = "uk-en" // United Kingdom
	RegionDE = "de-de" // Germany
	RegionFR = "fr-fr" // France
	RegionJP = "jp-jp" // Japan
	RegionCN = "cn-zh" // China
	RegionRU = "ru-ru" // Russia
)

// Safe search levels for DuckDuckGo
const (
	DuckDuckGoSafeSearchStrict   = "strict"   // Strict filtering
	DuckDuckGoSafeSearchModerate = "moderate" // Moderate filtering
	DuckDuckGoSafeSearchOff      = "off"      // No filtering
)

// Time range constants for DuckDuckGo search
const (
	TimeRangeDay   = "d" // Day
	TimeRangeWeek  = "w" // Week
	TimeRangeMonth = "m" // Month
	TimeRangeYear  = "y" // Year
)

// searchResult represents a single search result from DuckDuckGo
type searchResult struct {
	Title       string `json:"t"`
	URL         string `json:"u"`
	Description string `json:"a"`
}

// searchResponse represents the response from DuckDuckGo search API
type searchResponse struct {
	Results   []searchResult `json:"results"`
	NoResults bool           `json:"noResults"`
}

type duckduckgo struct {
	cfg *config.Config
	// backoff is how long to wait before attempt n (n >= 1).
	backoff func(n int, throttled bool) time.Duration
}

func NewDuckDuckGo(cfg *config.Config) Searcher {
	return &duckduckgo{cfg: cfg, backoff: duckduckgoBackoff}
}

// duckduckgoBackoff waits a second after an ordinary failure, and 2s, 4s, 8s plus jitter
// after a throttled one, since an immediate retry from the same IP is throttled again.
func duckduckgoBackoff(n int, throttled bool) time.Duration {
	if !throttled {
		return time.Second
	}
	return time.Second<<n + rand.N(time.Second)
}

func duckduckgoThrottled(status int, body []byte) bool {
	switch status {
	case http.StatusAccepted, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	case http.StatusOK:
		return strings.Contains(string(body), "anomaly-modal")
	default:
		return false
	}
}

func (d *duckduckgo) Engine() database.SearchengineType {
	return database.SearchengineTypeDuckduckgo
}

// Handle processes the search request from the orchestrator
func (d *duckduckgo) Handle(ctx context.Context, req Request) (string, error) {
	if !d.IsAvailable() {
		return "", ErrNotConfigured
	}

	ctx, observation := obs.Observer.NewObservation(ctx)

	// Set default number of results if invalid
	numResults := int(req.MaxResults)
	if numResults < 1 || numResults > duckduckgoMaxResults {
		numResults = duckduckgoMaxResults
	}

	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"engine":      "duckduckgo",
		"query":       req.Query[:min(len(req.Query), 1000)],
		"num_results": numResults,
		"region":      d.region(),
		"safe_search": d.safeSearch(),
		"time_range":  d.timeRange(),
	})

	// Perform search
	result, err := d.search(ctx, req.Query, numResults)
	if err != nil {
		observation.Event(
			langfuse.WithEventName("search engine error"),
			langfuse.WithEventInput(req.Query),
			langfuse.WithEventStatus(err.Error()),
			langfuse.WithEventLevel(langfuse.ObservationLevelWarning),
			langfuse.WithEventMetadata(langfuse.Metadata{
				"engine":      "duckduckgo",
				"query":       req.Query,
				"max_results": numResults,
				"region":      d.region(),
				"error":       err.Error(),
			}),
		)

		obs.LogErrorOrCancel(logger, err, "failed to search in DuckDuckGo")
		// DuckDuckGo already retries transient failures internally (see search);
		// by the time an error surfaces here, moving to the next engine is the
		// right call rather than burning another round-trip on the same one.
		return "", Fatal(fmt.Errorf("duckduckgo search failed: %w", err))
	}

	return result, nil
}

// search performs a web search using DuckDuckGo
func (d *duckduckgo) search(ctx context.Context, query string, maxResults int) (string, error) {
	// Build form data for POST request
	formData := d.buildFormData(query)

	// Create HTTP client with proper configuration
	client, err := system.GetHTTPClient(d.cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create http client: %w", err)
	}

	client.Timeout = duckduckgoTimeout

	var (
		response *searchResponse
		lastErr  error
	)
	for attempt := 0; attempt < duckduckgoMaxAttempts && response == nil; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(d.backoff(attempt, errors.Is(lastErr, errDuckduckgoThrottled))):
			}
		}

		endpoint := duckduckgoEndpoints[attempt%len(duckduckgoEndpoints)]
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint.origin+endpoint.path, strings.NewReader(formData))
		if err != nil {
			return "", fmt.Errorf("failed to create search request: %w", err)
		}

		req.Header.Set("User-Agent", duckduckgoUserAgent)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Origin", endpoint.origin)
		req.Header.Set("Referer", endpoint.origin+"/")

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lastErr = fmt.Errorf("failed to execute search after %d attempts: %w", attempt+1, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case duckduckgoThrottled(resp.StatusCode, body):
			lastErr = fmt.Errorf("%w (HTTP %d on %d attempts across the html and lite endpoints)",
				errDuckduckgoThrottled, resp.StatusCode, attempt+1)
			continue
		case resp.StatusCode != http.StatusOK:
			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			continue
		case err != nil:
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		response, err = d.parseHTMLResponse(body)
		if err != nil {
			return "", fmt.Errorf("failed to parse search response: %w", err)
		}
	}
	if response == nil {
		return "", lastErr
	}

	if len(response.Results) == 0 {
		return "No results found", nil
	}

	// Limit results to requested number
	if len(response.Results) > maxResults {
		response.Results = response.Results[:maxResults]
	}

	// Format results in readable text format
	return d.formatSearchResults(response.Results), nil
}

// buildFormData creates form data for DuckDuckGo POST request
func (d *duckduckgo) buildFormData(query string) string {
	params := url.Values{}
	params.Set("q", query)
	params.Set("b", "")
	params.Set("df", "")

	if region := d.region(); region != "" {
		params.Set("kl", region)
	}

	if safeSearch := d.safeSearch(); safeSearch != "" {
		params.Set("kp", safeSearch)
	}

	if timeRange := d.timeRange(); timeRange != "" {
		params.Set("df", timeRange)
	}

	return params.Encode()
}

// parseHTMLResponse parses the HTML search response from DuckDuckGo
func (d *duckduckgo) parseHTMLResponse(body []byte) (*searchResponse, error) {
	// Try structured HTML parsing first
	results, err := d.parseHTMLStructured(body)
	if err == nil && len(results) > 0 {
		return &searchResponse{
			Results:   results,
			NoResults: false,
		}, nil
	}

	if results := d.parseLite(body); len(results) > 0 {
		return &searchResponse{Results: results}, nil
	}

	// Fallback to regex-based parsing
	results, err = d.parseHTMLRegex(body)
	if err != nil {
		return nil, err
	}

	return &searchResponse{
		Results:   results,
		NoResults: len(results) == 0,
	}, nil
}

// parseHTMLStructured uses golang.org/x/net/html for structured HTML parsing
func (d *duckduckgo) parseHTMLStructured(body []byte) ([]searchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	results := make([]searchResult, 0)
	d.findResultNodes(doc, &results)

	return results, nil
}

// parseLite reads lite.duckduckgo.com, where a result is an a.result-link row followed by a
// td.result-snippet row of the same table.
func (d *duckduckgo) parseLite(body []byte) []searchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	var results []searchResult
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "a" && d.hasClass(n, "result-link"):
				results = append(results, searchResult{
					Title: d.cleanText(d.getTextContent(n)),
					URL:   d.getAttr(n, "href"),
				})
				return
			case n.Data == "td" && d.hasClass(n, "result-snippet") && len(results) > 0:
				results[len(results)-1].Description = d.cleanText(d.getTextContent(n))
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return slices.DeleteFunc(results, func(r searchResult) bool { return r.Title == "" || r.URL == "" })
}

// findResultNodes recursively finds and extracts search result nodes
func (d *duckduckgo) findResultNodes(n *html.Node, results *[]searchResult) {
	// Look for div with class "result results_links"
	if n.Type == html.ElementNode && n.Data == "div" {
		if d.hasClass(n, "result") && d.hasClass(n, "results_links") {
			result := d.extractResultFromNode(n)
			if result.Title != "" && result.URL != "" {
				*results = append(*results, result)
			}
		}
	}

	// Recurse through children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		d.findResultNodes(c, results)
	}
}

// extractResultFromNode extracts title, URL, and description from a result node
func (d *duckduckgo) extractResultFromNode(n *html.Node) searchResult {
	result := searchResult{}

	// Find title link (a.result__a)
	d.findElement(n, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "a" && d.hasClass(node, "result__a") {
			result.URL = d.getAttr(node, "href")
			result.Title = d.getTextContent(node)
			return true
		}
		return false
	})

	// Find snippet (a.result__snippet)
	d.findElement(n, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "a" && d.hasClass(node, "result__snippet") {
			result.Description = d.getTextContent(node)
			return true
		}
		return false
	})

	// Clean text
	result.Title = d.cleanText(result.Title)
	result.Description = d.cleanText(result.Description)

	return result
}

// findElement finds the first element matching the predicate
func (d *duckduckgo) findElement(n *html.Node, predicate func(*html.Node) bool) bool {
	if predicate(n) {
		return true
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if d.findElement(c, predicate) {
			return true
		}
	}

	return false
}

// hasClass checks if a node has a specific CSS class
func (d *duckduckgo) hasClass(n *html.Node, className string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			classes := strings.Fields(attr.Val)
			for _, c := range classes {
				if c == className {
					return true
				}
			}
		}
	}
	return false
}

// getAttr gets an attribute value from a node
func (d *duckduckgo) getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// getTextContent extracts all text content from a node and its children
func (d *duckduckgo) getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}

	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(d.getTextContent(c))
	}

	return text.String()
}

// parseHTMLRegex is a fallback regex-based parser
func (d *duckduckgo) parseHTMLRegex(body []byte) ([]searchResult, error) {
	htmlStr := string(body)

	// Check for "no results" message
	if strings.Contains(htmlStr, "No results found") || strings.Contains(htmlStr, "noResults") {
		return []searchResult{}, nil
	}

	results := make([]searchResult, 0)

	// Pattern to find result blocks (web results)
	// Each block starts with <div class="result results_links..."> and ends with <div class="clear"></div>
	// followed by closing tags </div></div>
	resultPattern := regexp.MustCompile(`(?s)<div class="result results_links[^"]*">.*?<div class="clear"></div>\s*</div>\s*</div>`)
	resultBlocks := resultPattern.FindAllString(htmlStr, -1)

	// Extract title, URL, and description from each result block
	titlePattern := regexp.MustCompile(`<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>([^<]+)</a>`)
	snippetPattern := regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]+href="[^"]*">(.+?)</a>`)

	for _, block := range resultBlocks {
		// Extract title and URL
		titleMatches := titlePattern.FindStringSubmatch(block)
		if len(titleMatches) < 3 {
			continue
		}

		resultURL := titleMatches[1]
		title := d.cleanText(titleMatches[2])

		// Extract description
		description := ""
		snippetMatches := snippetPattern.FindStringSubmatch(block)
		if len(snippetMatches) > 1 {
			description = d.cleanText(snippetMatches[1])
		}

		if title == "" || resultURL == "" {
			continue
		}

		results = append(results, searchResult{
			Title:       title,
			URL:         resultURL,
			Description: description,
		})
	}

	return results, nil
}

// cleanText removes HTML tags and decodes HTML entities
func (d *duckduckgo) cleanText(text string) string {
	// Remove HTML tags (like <b>, </b>, etc.)
	re := regexp.MustCompile(`<[^>]*>`)
	text = re.ReplaceAllString(text, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&#x27;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&apos;", "'")

	// Decode hex HTML entities (&#xNN;)
	hexEntityRe := regexp.MustCompile(`&#x([0-9A-Fa-f]+);`)
	text = hexEntityRe.ReplaceAllStringFunc(text, func(match string) string {
		// Extract hex value
		hex := hexEntityRe.FindStringSubmatch(match)
		if len(hex) > 1 {
			var codePoint int
			_, err := fmt.Sscanf(hex[1], "%x", &codePoint)
			if err == nil && codePoint < 128 {
				return string(rune(codePoint))
			}
		}
		return match
	})

	// Decode decimal HTML entities (&#NNN;)
	decEntityRe := regexp.MustCompile(`&#([0-9]+);`)
	text = decEntityRe.ReplaceAllStringFunc(text, func(match string) string {
		dec := decEntityRe.FindStringSubmatch(match)
		if len(dec) > 1 {
			var codePoint int
			_, err := fmt.Sscanf(dec[1], "%d", &codePoint)
			if err == nil && codePoint < 128 {
				return string(rune(codePoint))
			}
		}
		return match
	})

	// Trim whitespace and normalize spaces
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	return text
}

// formatSearchResults formats search results in a readable text format
func (d *duckduckgo) formatSearchResults(results []searchResult) string {
	var builder strings.Builder

	for i, result := range results {
		fmt.Fprintf(&builder, "# %d. %s\n\n", i+1, result.Title)
		fmt.Fprintf(&builder, "## URL\n%s\n\n", result.URL)
		fmt.Fprintf(&builder, "## Description\n\n%s\n\n", result.Description)

		if i < len(results)-1 {
			builder.WriteString("---\n\n")
		}
	}

	return builder.String()
}

func (d *duckduckgo) IsAvailable() bool {
	// DuckDuckGo is a free search engine that doesn't require API keys or additional configuration.
	// We only need to check if it's enabled in the settings according to the user config.
	return d.enabled()
}

func (d *duckduckgo) enabled() bool {
	return d.cfg != nil && d.cfg.DuckDuckGoEnabled
}

func (d *duckduckgo) region() string {
	if d.cfg == nil || d.cfg.DuckDuckGoRegion == "" {
		return RegionUS
	}

	return d.cfg.DuckDuckGoRegion
}

func (d *duckduckgo) safeSearch() string {
	switch d.cfg.DuckDuckGoSafeSearch {
	case DuckDuckGoSafeSearchStrict:
		return "1"
	case DuckDuckGoSafeSearchModerate:
		return "0"
	case DuckDuckGoSafeSearchOff:
		return "-1"
	default:
		return ""
	}
}

func (d *duckduckgo) timeRange() string {
	if d.cfg == nil || d.cfg.DuckDuckGoTimeRange == "" {
		return ""
	}

	return d.cfg.DuckDuckGoTimeRange
}
