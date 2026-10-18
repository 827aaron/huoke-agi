package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJev_Judge_AsksOneNoulPerMessage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/systemone", r.URL.Path)
		assert.Equal(t, "Bearer ts-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"m0":{"type":"noul","noul":0.91},"m1":{"type":"noul","noul":0.12}},"usage":{}}`))
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL + "/", Key: "ts-key"}
	out, model, err := c.Judge(t.Context(), Batch{
		Platform: "telegram", Source: "Fans", Task: "find 10 guild leaders", Business: "Offer: an MMORPG",
		Excluded: []string{"a stated age under 18"},
		Messages: []Message{{ID: "45", Sender: "@ann", Text: "looking for a new MMO"}, {ID: "44", Sender: "@bob", Text: "lol"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]Judgement{
		"45": {Excluded: 0.91, Unwanted: -1, Signals: map[string]float64{}},
		"44": {Excluded: 0.12, Unwanted: -1, Signals: map[string]float64{}},
	}, out)
	assert.Equal(t, "jev-1.13.0", model)

	assert.Equal(t, "jev-latest", got["model"])
	state := got["state"].(map[string]any)
	assert.Equal(t, "telegram", state["platform"])
	assert.Equal(t, "Fans", state["source"])
	assert.Equal(t, "Offer: an MMORPG", state["business"])
	assert.Equal(t, "find 10 guild leaders", state["task"])
	assert.Equal(t, []any{"a stated age under 18"}, state["excluded"])
	assert.Equal(t, []any{}, state["unwanted"], "an empty list, not null")
	assert.Equal(t, map[string]any{"id": "44", "sender": "@bob", "text": "lol"}, state["messages"].(map[string]any)["m1"])
	questions := got["questions"].(map[string]any)
	require.Len(t, questions, 2, "no unwanted question without an unwanted list")
	q := questions["m1"].(map[string]any)
	assert.Equal(t, "noul", q["type"])
	assert.Contains(t, q["instructions"], "`messages.m1`")
	assert.Contains(t, q["instructions"], "one of the people listed in `excluded`, or that it is spam, advertising or a bot")
	assert.Contains(t, q["instructions"], "apply whatever `task` and `business` say")
	assert.Contains(t, q["criteria"].(map[string]any)["false"], "never infer them from style, age cues, language or platform")
}

func TestJev_Judge_AsksTheUnwantedQuestionWhenTheListIsNotEmpty(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"answers":{"m0":{"type":"noul","noul":0.1},"m0_unwanted":{"type":"noul","noul":0.8},` +
			`"m1":{"type":"noul","noul":0.1}}}`))
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL, Key: "k"}
	out, _, err := c.Judge(t.Context(), Batch{
		Task:     "find 10 agents",
		Unwanted: []string{"agents and resellers", "people in Singapore"},
		Messages: []Message{{ID: "7", Text: "I am an agent"}, {ID: "8", Text: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]Judgement{
		"7": {Excluded: 0.1, Unwanted: 0.8, Signals: map[string]float64{}},
		"8": {Excluded: 0.1, Unwanted: -1, Signals: map[string]float64{}},
	}, out)

	assert.Equal(t, []any{"agents and resellers", "people in Singapore"}, got["state"].(map[string]any)["unwanted"])
	questions := got["questions"].(map[string]any)
	require.Len(t, questions, 4)
	q := questions["m1_unwanted"].(map[string]any)
	assert.Equal(t, "noul", q["type"])
	assert.Contains(t, q["instructions"], "`messages.m1` itself show that its sender is one of the people listed in `unwanted`")
	assert.Contains(t, q["instructions"], "wins over `unwanted`: an entry that contradicts `task` does not apply")
	assert.Contains(t, q["criteria"].(map[string]any)["true"], "explicitly")
}

func TestJev_SignalFor_BuildsAQuestionFromTheProfilesOwnDimension(t *testing.T) {
	tests := []struct {
		name, key, dimName, description string
		want                            Signal
		question                        string
	}{
		{"a built-in key with its own description", "paying", "Paying",
			"The sender mentions paying for this kind of product: purchases, deposits, top-ups, subscriptions.", Signals[2], ""},
		{"a built-in key with no description", "player", "Audience", "", Signals[0], ""},
		{"a built-in key the owner redefined", "player", "VIP", "The sender says they are a VIP or high roller.", Signal{},
			"Does `messages.m3` show that its sender fits \"VIP\", meaning: The sender says they are a VIP or high roller? " +
				"Judge this for the search `task` describes and the business in `business`."},
		{"an owner's dimension", "agent_team", "Runs a team", "The sender says they lead or recruit a team of agents.", Signal{},
			"Does `messages.m3` show that its sender fits \"Runs a team\", meaning: The sender says they lead or recruit a team " +
				"of agents? Judge this for the search `task` describes and the business in `business`."},
		{"a name alone, with a percent sign", "d1", "Wants 100% bonus", "", Signal{},
			"Does `messages.m3` show that its sender fits \"Wants 100% bonus\"? " +
				"Judge this for the search `task` describes and the business in `business`."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SignalFor(tt.key, tt.dimName, tt.description)
			if tt.question == "" {
				assert.Equal(t, tt.want, got)
				return
			}
			assert.Equal(t, tt.key, got.Key)
			assert.Equal(t, tt.question, fmt.Sprintf(got.Question, "m3"))
			assert.Equal(t, "The message shows it, in the sender's own words or as a plain fact", got.Criteria["true"])
			assert.Equal(t, "The message does not show it", got.Criteria["false"])
		})
	}
}

func TestJev_Judge_AsksEverySignalOfEveryMessageOverOneState(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"answers":{"m0":{"type":"noul","noul":0.9},"m0_player":{"type":"noul","noul":0.8},` +
			`"m0_paying":{"type":"noul","noul":0.1},"m1_player":{"type":"noul","noul":0.6}}}`))
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL, Key: "k"}
	out, _, err := c.Judge(t.Context(), Batch{
		Messages: []Message{{ID: "7", Text: "I topped up 50 in my RPG"}, {ID: "8", Text: "lol"}},
		Signals:  Signals,
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]Judgement{
		"7": {Excluded: 0.9, Unwanted: -1, Signals: map[string]float64{"player": 0.8, "paying": 0.1}},
		"8": {Excluded: -1, Unwanted: -1, Signals: map[string]float64{"player": 0.6}},
	}, out)

	questions := got["questions"].(map[string]any)
	require.Len(t, questions, 10)
	assert.Contains(t, questions["m1_pain"].(map[string]any)["instructions"], "`messages.m1`")
	assert.Len(t, got["state"].(map[string]any)["messages"], 2)
}

func TestJev_Judge_RetriesARateLimitAndStopsOnARefusedKey(t *testing.T) {
	tests := []struct {
		name     string
		statuses []int
		calls    int
		err      error
		waits    []time.Duration
	}{
		{"429 then success", []int{429, 529, 200}, 3, nil, []time.Duration{time.Second, 2 * time.Second}},
		{"402 waits as long as the server asks", []int{402, 200}, 2, nil, []time.Duration{7 * time.Second}},
		{"refused key", []int{401}, 1, ErrUnauthorized, nil},
		{"forbidden key", []int{403}, 1, ErrUnauthorized, nil},
		{"overloaded throughout", []int{529, 529, 529}, 3, ErrUnavailable, nil},
		{"out of balance throughout", []int{402, 402, 402}, 3, ErrUnavailable, nil},
		{"bad request", []int{422}, 1, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := tt.statuses[min(calls, len(tt.statuses)-1)]
				calls++
				if status == http.StatusPaymentRequired {
					w.Header().Set("Retry-After", "7")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"answers":{"m0":{"type":"noul","noul":0.7}}}`))
			}))
			defer srv.Close()

			var waits []time.Duration
			c := &Client{URL: srv.URL, Key: "k", Retries: 2, Backoff: time.Second,
				Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}
			out, _, err := c.Judge(t.Context(), Batch{Messages: []Message{{ID: "1", Text: "hi"}}})
			assert.Equal(t, tt.calls, calls)
			switch {
			case tt.statuses[len(tt.statuses)-1] == 200:
				require.NoError(t, err)
				assert.Equal(t, map[string]Judgement{"1": {Excluded: 0.7, Unwanted: -1, Signals: map[string]float64{}}}, out)
				assert.Equal(t, tt.waits, waits)
			case tt.err != nil:
				assert.ErrorIs(t, err, tt.err)
				if tt.statuses[0] == http.StatusPaymentRequired {
					assert.ErrorContains(t, err, "balance is too low")
				}
			default:
				assert.ErrorContains(t, err, "422")
				assert.NotErrorIs(t, err, ErrUnavailable)
			}
		})
	}
}

func TestJev_Judge_AsksForTheNewestJevUnderTheEndpointsNaming(t *testing.T) {
	tests := []struct {
		name, url, model, want string
	}{
		{"typesafe", "", "", "jev-latest"},
		{"openrouter", "https://openrouter.ai/api", "", "~typesafe/jev-latest"},
		{"a saved untilded latest on openrouter", "https://openrouter.ai/api", "typesafe/jev-latest", "~typesafe/jev-latest"},
		{"another endpoint", "https://jev.example.test", "", "jev-latest"},
		{"a pinned version", "https://openrouter.ai/api", "typesafe/jev-1.13", "typesafe/jev-1.13"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, (&Client{URL: tt.url, Model: tt.model}).ResolvedModel())
		})
	}
}
