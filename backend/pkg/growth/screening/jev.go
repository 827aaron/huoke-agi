// Package screening asks the Jev model, one yes/no judgment per message,
// whether a message's sender may be a customer of the business. It knows nothing
// about platforms: the caller passes the platform and source as plain text.
package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Jev is served by TypeSafe and, under the same System One API, by OpenRouter, which
// names its models with a "typesafe/" prefix. The default models track the newest Jev.
const (
	DefaultURL             = "https://api.typesafe.ai"
	DefaultModel           = "jev-latest"
	OpenRouterURL          = "https://openrouter.ai/api"
	OpenRouterDefaultModel = "typesafe/jev-latest"
	OpenRouterModelPrefix  = "typesafe/"
)

// maxRetryAfter caps the wait a server asks for, so one read is not held for minutes.
const maxRetryAfter = time.Minute

var (
	// ErrUnauthorized means the key was refused; asking again with it is pointless.
	ErrUnauthorized = errors.New("the Jev API key was refused")
	// ErrUnavailable means Jev stayed rate-limited, out of balance, overloaded or
	// unreachable through every retry.
	ErrUnavailable = errors.New("jev is unavailable")
)

// Message is one message to judge. Sender is a display form ("@ann Ann").
type Message struct {
	ID     string `json:"id"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

// Batch is a set of messages from one source judged against one business.
type Batch struct {
	Platform string    `json:"platform"`
	Source   string    `json:"source"`
	Business string    `json:"business"`
	Messages []Message `json:"messages"`
}

// Client calls the System One HTTP API. The zero HTTP client and retry delay take
// defaults; Sleep is replaced by tests.
type Client struct {
	URL   string
	Key   string
	Model string
	HTTP  *http.Client
	// Retries is how many times a 402, a 429, a 529, a 5xx or a transport error is retried.
	Retries int
	Backoff time.Duration
	Sleep   func(ctx context.Context, d time.Duration) error
}

// question is asked of each message in turn; %s is its key under `messages`.
const question = "Could the sender of `messages.%s` (posted on `platform`, in `source`) be a potential customer of " +
	"the business in `business`: a person who plays, wants, asks about, compares or pays for what it offers, " +
	"or complains about an alternative?"

var criteria = map[string]string{
	"true":  "The message shows the sender may want or use what the business offers, even indirectly",
	"false": "Spam, advertising, a bot, a seller or booster, a competitor, or nothing to do with the business",
}

// Judge returns, for each message id, the probability that its sender is a potential
// customer, and the model version that answered. A message missing from the answer is
// missing from the map.
func (c *Client) Judge(ctx context.Context, b Batch) (map[string]float64, string, error) {
	body, ids := request(c.ResolvedModel(), b)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encode the Jev request: %w", err)
	}

	var (
		lastErr error
		wait    time.Duration
	)
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, max(c.backoff()<<(attempt-1), wait)); err != nil {
				return nil, "", err
			}
		}
		res, err := c.post(ctx, payload)
		if err == nil {
			out := make(map[string]float64, len(res.answers))
			for qid, id := range ids {
				if a, ok := res.answers[qid]; ok && a.Noul != nil {
					out[id] = *a.Noul
				}
			}
			return out, res.model, nil
		}
		if !res.retry || ctx.Err() != nil {
			return nil, "", err
		}
		lastErr, wait = err, res.retryAfter
	}
	return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
}

type answer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

type result struct {
	answers    map[string]answer
	model      string
	retry      bool
	retryAfter time.Duration
}

func (c *Client) post(ctx context.Context, payload []byte) (result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.ResolvedURL(), "/")+"/v1/systemone", bytes.NewReader(payload))
	if err != nil {
		return result{}, fmt.Errorf("failed to build the Jev request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return result{retry: true}, fmt.Errorf("jev request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return result{retry: true}, fmt.Errorf("failed to read the Jev response: %w", err)
	}

	retryable := result{retry: true, retryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return result{}, ErrUnauthorized
	// OpenRouter answers 402 while the account's balance or spending limit is used up,
	// and lifts it once the balance is topped up.
	case resp.StatusCode == http.StatusPaymentRequired:
		return retryable, fmt.Errorf("jev answered 402, the balance is too low: top it up and the reads resume: %s", snippet(data))
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 || resp.StatusCode >= 500:
		return retryable, fmt.Errorf("jev answered %d: %s", resp.StatusCode, snippet(data))
	case resp.StatusCode != http.StatusOK:
		return result{}, fmt.Errorf("jev refused the request with %d: %s", resp.StatusCode, snippet(data))
	}
	var out struct {
		Model   string            `json:"model"`
		Answers map[string]answer `json:"answers"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return result{}, fmt.Errorf("unreadable Jev response: %w", err)
	}
	return result{answers: out.Answers, model: out.Model}, nil
}

// retryAfter reads the seconds form of Retry-After; the date form is rare on APIs.
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, maxRetryAfter)
}

// request asks one independent question per message over one shared state. Keys are
// positional because a platform's message ids need not be valid keys.
func request(model string, b Batch) (map[string]any, map[string]string) {
	questions := make(map[string]any, len(b.Messages))
	messages := make(map[string]any, len(b.Messages))
	ids := make(map[string]string, len(b.Messages))
	for i, m := range b.Messages {
		qid := fmt.Sprintf("m%d", i)
		ids[qid] = m.ID
		messages[qid] = m
		questions[qid] = map[string]any{
			"type":         "noul",
			"instructions": fmt.Sprintf(question, qid),
			"criteria":     criteria,
		}
	}
	return map[string]any{
		"model": model,
		"state": map[string]any{
			"platform": b.Platform,
			"source":   b.Source,
			"business": b.Business,
			"messages": messages,
		},
		"questions": questions,
	}, ids
}

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func (c *Client) ResolvedURL() string {
	if c.URL == "" {
		return DefaultURL
	}
	return c.URL
}

// ResolvedModel is the model asked for: the set one, else the newest Jev under the
// naming of the endpoint.
func (c *Client) ResolvedModel() string {
	if c.Model != "" {
		return c.Model
	}
	return DefaultModelFor(c.ResolvedURL())
}

// DefaultModelFor is the newest Jev as the endpoint at rawURL names it.
func DefaultModelFor(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && (u.Hostname() == "openrouter.ai" || strings.HasSuffix(u.Hostname(), ".openrouter.ai")) {
		return OpenRouterDefaultModel
	}
	return DefaultModel
}

func (c *Client) client() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return c.HTTP
}

func (c *Client) backoff() time.Duration {
	if c.Backoff <= 0 {
		return 2 * time.Second
	}
	return c.Backoff
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
