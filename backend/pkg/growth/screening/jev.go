// Package screening asks the Jev model, in yes/no judgments per message, whether a
// message shows its sender is someone the business cannot serve and which buying signals it shows. It knows
// nothing about platforms: the caller passes the platform and source as plain text.
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
// OpenRouter serves that as the tilde alias "~typesafe/jev-latest", which its model list
// does not carry; "typesafe/jev-latest" is answered with "model not found".
const (
	DefaultURL             = "https://api.typesafe.ai"
	DefaultModel           = "jev-latest"
	OpenRouterURL          = "https://openrouter.ai/api"
	OpenRouterDefaultModel = "~typesafe/jev-latest"
	OpenRouterModelPrefix  = "typesafe/"

	openRouterUntildedLatest = "typesafe/jev-latest"
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

// Batch is a set of messages from one source judged against one business. Signals are
// asked of every message beside the exclusion question.
type Batch struct {
	Platform string    `json:"platform"`
	Source   string    `json:"source"`
	Business string    `json:"business"`
	Messages []Message `json:"messages"`
	Signals  []Signal  `json:"-"`
}

// Signal is one yes/no question about a message; Question has one %s, the message's key.
type Signal struct {
	Key      string
	Question string
	Criteria map[string]string
}

// Judgement is the probability that a message shows its sender is excluded, and that
// it shows each signal asked, by key. A question missing from the answer is missing
// from Signals; Excluded is -1 when its own question went unanswered.
type Judgement struct {
	Excluded float64
	Signals  map[string]float64
}

// The buying signals of a likely customer. The keys are stable weight names; the
// questions name no industry, so the business text decides who the audience is.
const (
	SignalPlayer = "player"
	SignalPain   = "pain"
	SignalPaying = "paying"
	SignalMatch  = "match"
)

var Signals = []Signal{
	{
		Key: SignalPlayer,
		Question: "Is the sender of `messages.%s` part of the audience the business in `business` serves: do they use " +
			"or follow its kind of product or service themselves (talk about using it, ask for tips, share their results)?",
		Criteria: map[string]string{
			"true":  "The sender shows they use or follow this kind of product or service themselves",
			"false": "No sign the sender belongs to this audience",
		},
	},
	{
		Key: SignalPain,
		Question: "Is the sender of `messages.%s` unhappy with what they use now, bored with it, leaving it, " +
			"or asking for an alternative like the offer in `business`? Wanting to stop using this kind of product " +
			"altogether is not this signal.",
		Criteria: map[string]string{
			"true":  "Complaint, boredom, quitting, or a request for something similar or better",
			"false": "No complaint and no search for an alternative",
		},
	},
	{
		Key: SignalPaying,
		Question: "Does the sender of `messages.%s` mention paying for this kind of product or service: purchases, " +
			"deposits, top-ups, subscriptions, plans or memberships?",
		Criteria: map[string]string{
			"true":  "The sender mentions spending money on this kind of product",
			"false": "No mention of spending",
		},
	},
	{
		Key: SignalMatch,
		Question: "Does what the sender of `messages.%s` wants or uses fit the offer in `business`: " +
			"its category, platform, region and style?",
		Criteria: map[string]string{
			"true":  "Same or close category, platform and style",
			"false": "Unrelated or opposite to the offer",
		},
	},
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

// question is asked of each message in turn; %s is its key under `messages`. A yes
// drops the message whatever its signals score, so it asks for explicit evidence only.
const question = "Does `messages.%s` (posted on `platform`, in `source`) itself show that its sender is someone the " +
	"business in `business` cannot serve: spam, advertising, a bot, a seller or booster, a competitor, someone " +
	"`business` says is usually not a customer, or a person outside its regions or with no use for its offer?"

var criteria = map[string]string{
	"true": "The message states it explicitly, in the sender's own words or as a plain fact",
	"false": "The message does not state it. Exclusions apply only on explicit evidence in the message; never " +
		"infer them from style, age cues, language or platform",
}

// Judge returns the judgement of each message by id, and the model version that
// answered. A message with no answer at all is missing from the map.
func (c *Client) Judge(ctx context.Context, b Batch) (map[string]Judgement, string, error) {
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
			out := make(map[string]Judgement, len(b.Messages))
			for qid, q := range ids {
				a, ok := res.answers[qid]
				if !ok || a.Noul == nil {
					continue
				}
				j, seen := out[q.id]
				if !seen {
					j = Judgement{Excluded: -1, Signals: map[string]float64{}}
				}
				if q.signal == "" {
					j.Excluded = *a.Noul
				} else {
					j.Signals[q.signal] = *a.Noul
				}
				out[q.id] = j
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

type questionOf struct {
	id     string
	signal string
}

// request asks independent questions per message over one shared state, so the
// messages are sent once however many questions there are. Keys are positional because
// a platform's message ids need not be valid keys.
func request(model string, b Batch) (map[string]any, map[string]questionOf) {
	questions := make(map[string]any, len(b.Messages)*(1+len(b.Signals)))
	messages := make(map[string]any, len(b.Messages))
	ids := make(map[string]questionOf, len(questions))
	for i, m := range b.Messages {
		key := fmt.Sprintf("m%d", i)
		messages[key] = m
		ids[key] = questionOf{id: m.ID}
		questions[key] = map[string]any{
			"type":         "noul",
			"instructions": fmt.Sprintf(question, key),
			"criteria":     criteria,
		}
		for _, sig := range b.Signals {
			qid := key + "_" + sig.Key
			ids[qid] = questionOf{id: m.ID, signal: sig.Key}
			questions[qid] = map[string]any{
				"type":         "noul",
				"instructions": fmt.Sprintf(sig.Question, key),
				"criteria":     sig.Criteria,
			}
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
// naming of the endpoint. A saved "typesafe/jev-latest" on OpenRouter means its alias.
func (c *Client) ResolvedModel() string {
	if c.Model == "" || (c.Model == openRouterUntildedLatest && isOpenRouter(c.ResolvedURL())) {
		return DefaultModelFor(c.ResolvedURL())
	}
	return c.Model
}

// DefaultModelFor is the newest Jev as the endpoint at rawURL names it.
func DefaultModelFor(rawURL string) string {
	if isOpenRouter(rawURL) {
		return OpenRouterDefaultModel
	}
	return DefaultModel
}

// IsOpenRouterModel tells a Jev model as OpenRouter lists or aliases it.
func IsOpenRouterModel(id string) bool {
	return strings.HasPrefix(strings.TrimPrefix(id, "~"), OpenRouterModelPrefix)
}

func isOpenRouter(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && (u.Hostname() == "openrouter.ai" || strings.HasSuffix(u.Hostname(), ".openrouter.ai"))
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
