//go:build postgres

package growth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelsStub struct {
	*httptest.Server
	mu     sync.Mutex
	paths  []string
	header []http.Header
	status int
}

func startModelsStub(t *testing.T) *modelsStub {
	t.Helper()
	m := &modelsStub{status: http.StatusOK}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.paths, m.header = append(m.paths, r.URL.Path), append(m.header, r.Header.Clone())
		status := m.status
		m.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":[{"id":"typesafe/jev-latest","name":"Jev"},{"id":"openai/gpt-5"},` +
			`{"id":"claude-x","display_name":"Claude X"},{"id":"typesafe/jev-1.13","name":"Jev 1.13"}]}`))
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *modelsStub) last() (string, http.Header) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paths[len(m.paths)-1], m.header[len(m.header)-1]
}

func TestModels_ListModels_KeepsOnlyJevOnOpenRouterAndReusesTheSavedKey(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	stub := startModelsStub(t)

	got, err := w.svc.ListModels(ctx, w.user, SettingScreening, screeningOpenRouter, map[string]string{"api_key": "or-typed", "api_url": stub.URL})
	require.NoError(t, err)
	assert.Equal(t, []ModelOption{{ID: "typesafe/jev-1.13", Name: "Jev 1.13"}, {ID: "typesafe/jev-latest", Name: "Jev"}}, got)
	path, header := stub.last()
	assert.Equal(t, "/v1/models", path)
	assert.Equal(t, "Bearer or-typed", header.Get("Authorization"))

	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningOpenRouter, map[string]string{"api_key": "or-saved", "api_url": stub.URL + "/api"})
	require.NoError(t, err)
	_, err = w.svc.ListModels(ctx, w.user, SettingScreening, screeningOpenRouter, map[string]string{"api_key": ""})
	require.NoError(t, err)
	path, header = stub.last()
	assert.Equal(t, "/api/v1/models", path)
	assert.Equal(t, "Bearer or-saved", header.Get("Authorization"))

	w.svc.cfg.TypeSafeAPIKey = ""
	_, err = w.svc.ListModels(ctx, w.user, SettingScreening, screeningCustom, map[string]string{"api_url": stub.URL})
	assert.ErrorIs(t, err, ErrInvalidSetting, "a saved key of another provider is not reused")
}

func TestModels_ListModels_ListsAGatewaysModelsForTheLLM(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	stub := startModelsStub(t)

	tests := []struct {
		name, suffix, protocol, path string
		anthropic                    bool
	}{
		{"an OpenAI base with its version", "/api/v1/", "", "/api/v1/models", false},
		{"a base with another version", "/api/paas/v4", "", "/api/paas/v4/models", false},
		{"a base without a version", "/vision", "", "/vision/v1/models", false},
		{"an Anthropic endpoint", "/v1", llmProtocolAnthropic, "/v1/models", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := w.svc.ListModels(ctx, w.user, SettingLLM, "custom", map[string]string{
				"api_key": "gw-key", "server_url": stub.URL + tt.suffix, "protocol": tt.protocol,
			})
			require.NoError(t, err)
			require.Len(t, got, 4)
			assert.Equal(t, ModelOption{ID: "claude-x", Name: "Claude X"}, got[0])
			path, header := stub.last()
			assert.Equal(t, tt.path, path)
			if tt.anthropic {
				assert.Equal(t, "gw-key", header.Get("x-api-key"))
				assert.Empty(t, header.Get("Authorization"))
			} else {
				assert.Equal(t, "Bearer gw-key", header.Get("Authorization"))
			}
		})
	}
}

func TestModels_ListModels_SaysWhyTheListIsMissing(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	stub := startModelsStub(t)
	stub.status = http.StatusUnauthorized

	_, err := w.svc.ListModels(ctx, w.user, SettingLLM, "custom", map[string]string{"api_key": "bad", "server_url": stub.URL})
	assert.ErrorIs(t, err, ErrModelList)
	assert.ErrorContains(t, err, "answered 401")

	_, err = w.svc.ListModels(ctx, w.user, SettingLLM, "gemini", map[string]string{"api_key": "k"})
	assert.ErrorIs(t, err, ErrInvalidSetting)
	_, err = w.svc.ListModels(ctx, w.user, SettingTelegram, "", nil)
	assert.ErrorIs(t, err, ErrInvalidSetting)
}
