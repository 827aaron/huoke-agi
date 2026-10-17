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
	out, err := c.Judge(t.Context(), Batch{
		Platform: "telegram", Source: "Fans", Business: "Offer: an MMORPG",
		Messages: []Message{{ID: "45", Sender: "@ann", Text: "looking for a new MMO"}, {ID: "44", Sender: "@bob", Text: "lol"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"45": 0.91, "44": 0.12}, out)

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

func TestJev_Judge_RetriesARateLimitAndStopsOnARefusedKey(t *testing.T) {
	tests := []struct {
		name     string
		statuses []int
		calls    int
		err      error
	}{
		{"429 then success", []int{429, 529, 200}, 3, nil},
		{"refused key", []int{401}, 1, ErrUnauthorized},
		{"overloaded throughout", []int{529, 529, 529}, 3, ErrUnavailable},
		{"bad request", []int{422}, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := tt.statuses[min(calls, len(tt.statuses)-1)]
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"answers":{"m0":{"type":"noul","noul":0.7}}}`))
			}))
			defer srv.Close()

			var waits []time.Duration
			c := &Client{URL: srv.URL, Key: "k", Retries: 2, Backoff: time.Second,
				Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}
			out, err := c.Judge(t.Context(), Batch{Messages: []Message{{ID: "1", Text: "hi"}}})
			assert.Equal(t, tt.calls, calls)
			switch {
			case tt.statuses[len(tt.statuses)-1] == 200:
				require.NoError(t, err)
				assert.Equal(t, map[string]float64{"1": 0.7}, out)
				assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, waits)
			case tt.err != nil:
				assert.ErrorIs(t, err, tt.err)
			default:
				assert.ErrorContains(t, err, "422")
				assert.NotErrorIs(t, err, ErrUnavailable)
			}
		})
	}
}
