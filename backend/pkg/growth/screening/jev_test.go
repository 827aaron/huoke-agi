package screening

import (
	"context"
	"encoding/json"
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
		Platform: "telegram", Source: "Fans", Business: "Offer: an MMORPG",
		Messages: []Message{{ID: "45", Sender: "@ann", Text: "looking for a new MMO"}, {ID: "44", Sender: "@bob", Text: "lol"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]Judgement{
		"45": {Customer: 0.91, Signals: map[string]float64{}},
		"44": {Customer: 0.12, Signals: map[string]float64{}},
	}, out)
	assert.Equal(t, "jev-1.13.0", model)

	assert.Equal(t, "jev-latest", got["model"])
	state := got["state"].(map[string]any)
	assert.Equal(t, "telegram", state["platform"])
	assert.Equal(t, "Fans", state["source"])
	assert.Equal(t, "Offer: an MMORPG", state["business"])
	assert.Equal(t, map[string]any{"id": "44", "sender": "@bob", "text": "lol"}, state["messages"].(map[string]any)["m1"])
	questions := got["questions"].(map[string]any)
	require.Len(t, questions, 2)
	q := questions["m1"].(map[string]any)
	assert.Equal(t, "noul", q["type"])
	assert.Contains(t, q["instructions"], "`messages.m1`")
	assert.Contains(t, q["criteria"], "true")
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
		"7": {Customer: 0.9, Signals: map[string]float64{"player": 0.8, "paying": 0.1}},
		"8": {Customer: -1, Signals: map[string]float64{"player": 0.6}},
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
				assert.Equal(t, map[string]Judgement{"1": {Customer: 0.7, Signals: map[string]float64{}}}, out)
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
