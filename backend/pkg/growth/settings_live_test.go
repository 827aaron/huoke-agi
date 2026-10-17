//go:build postgres

package growth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"testing"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settingOf(t *testing.T, w *world, kind string) SettingView {
	t.Helper()
	return settingOfService(t, w.svc, w.user, kind)
}

func TestSettings_SaveSetting_StoresEncryptedAndListsMasked(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.SaveSetting(ctx, w.user, SettingWebSearch, "Tavily", map[string]string{"api_keys": "tvly-secret-1234"})
	require.NoError(t, err)

	v := settingOf(t, w, SettingWebSearch)
	assert.Equal(t, SettingSourceDatabase, v.Source)
	assert.Equal(t, "tavily", v.Provider)
	assert.Equal(t, "••••1234", v.Fields["api_keys"])
	assert.Equal(t, SettingStatusUntested, v.Status)

	var secret []byte
	require.NoError(t, w.db.QueryRow(`SELECT secret FROM growth_settings WHERE user_id = $1`, w.user).Scan(&secret))
	assert.False(t, bytes.Contains(secret, []byte("tvly-secret-1234")))

	assert.Equal(t, SettingSourceNone, settingOf(t, w, SettingYouTube).Source)
}

func TestSettings_SaveSetting_KeepsASecretTheFormLeftEmpty(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_, err := w.svc.SaveSetting(ctx, w.user, SettingLLM, "custom", map[string]string{
		"api_key": "sk-first-key-9999", "server_url": "https://llm.example.test/v1", "model": "m1",
	})
	require.NoError(t, err)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingLLM, "custom", map[string]string{"api_key": "", "model": "m2"})
	require.NoError(t, err)

	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "sk-first-key-9999", cfg.LLMServerKey)
	assert.Equal(t, "https://llm.example.test/v1", cfg.LLMServerURL)
	assert.Equal(t, "m2", cfg.LLMServerModel)
}

func TestSettings_SaveSetting_RejectsWhatCannotWork(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	cases := []struct {
		kind, provider string
		fields         map[string]string
	}{
		{"llm", "nobody", map[string]string{"api_key": "k"}},
		{"llm", "openai", map[string]string{}},
		{"llm", "openai", map[string]string{"api_key": "k", "server_url": "ftp://x"}},
		{"llm", "openai", map[string]string{"api_key": "k", "protocol": "openai_chat"}},
		{"llm", "custom", map[string]string{"api_key": "k"}},
		{"llm", "custom", map[string]string{"api_key": "k", "server_url": "https://gw.example.test/v1", "protocol": "grpc"}},
		{"telegram", "", map[string]string{"api_id": "abc", "api_hash": "0123456789abcdef0123456789abcdef"}},
		{"telegram", "", map[string]string{"api_id": "123", "api_hash": "short"}},
		{"web_search", "google", map[string]string{"api_keys": "k"}},
		{"email", "", map[string]string{"host": "smtp.example.test", "from": "not an address"}},
		{"email", "", map[string]string{"host": "smtp.example.test", "from": "a@b.test", "tls": "ssl3"}},
		{"youtube", "", map[string]string{"api_keys": "k", "extra": "x"}},
		{"proxy", "", map[string]string{"url": "ftp://proxy.example.test:21"}},
		{"proxy", "", map[string]string{"url": "socks5://proxy.example.test"}},
		{"payments", "", map[string]string{"api_key": "k"}},
	}
	for _, tc := range cases {
		_, err := w.svc.SaveSetting(ctx, w.user, tc.kind, tc.provider, tc.fields)
		assert.ErrorIs(t, err, ErrInvalidSetting, "%s %v", tc.kind, tc.fields)
	}
}

func TestSettings_SaveSetting_NeedsTheMasterKey(t *testing.T) {
	w := newWorld(t)
	bare := NewService(database.New(w.db), &config.Config{})

	_, err := bare.SaveSetting(context.Background(), w.user, SettingYouTube, "", map[string]string{"api_keys": "yt-key-123456"})
	assert.ErrorIs(t, err, ErrNoSecretKey)
}

func TestSettings_ConfigFor_AppliesOnlyTheOwnersCredentials(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	other := seedUser(t, w.db, "other@example.test")
	_, err := w.svc.SaveSetting(ctx, w.user, SettingTelegram, "", map[string]string{
		"api_id": "12345", "api_hash": "0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)

	mine, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, 12345, mine.TGAPIID)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", mine.TGAPIHash)

	theirs, err := w.svc.ConfigFor(ctx, other)
	require.NoError(t, err)
	assert.Zero(t, theirs.TGAPIID)
	assert.Empty(t, theirs.TGAPIHash)

	rekeyed := NewService(database.New(w.db), &config.Config{GrowthSecretKey: "another-key"})
	cfg, err := rekeyed.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Empty(t, cfg.TGAPIHash, "a credential sealed with another key is ignored")
	assert.Equal(t, SettingStatusInvalid, settingOfService(t, rekeyed, w.user, SettingTelegram).Status)
}

func settingOfService(t *testing.T, svc *Service, userID int64, kind string) SettingView {
	t.Helper()
	views, err := svc.ListSettings(context.Background(), userID)
	require.NoError(t, err)
	for _, v := range views {
		if v.Kind == kind {
			return v
		}
	}
	t.Fatalf("no %s setting listed", kind)
	return SettingView{}
}

func TestSettings_TestSetting_RecordsWhatTheServiceAnswers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" || gotAuth != "Bearer good-key-0001" {
			rw.WriteHeader(http.StatusUnauthorized)
			_, _ = rw.Write([]byte(`{"error":"invalid api key"}`))
			return
		}
		_, _ = rw.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(api.Close)

	_, err := w.svc.SaveSetting(ctx, w.user, SettingLLM, "openai", map[string]string{
		"api_key": "good-key-0001", "server_url": api.URL + "/v1",
	})
	require.NoError(t, err)
	v, err := w.svc.TestSetting(ctx, w.user, SettingLLM)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)
	assert.Equal(t, "Bearer good-key-0001", gotAuth)
	assert.NotNil(t, v.TestedAt)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingLLM, "openai", map[string]string{"api_key": "bad-key-0002"})
	require.NoError(t, err)
	assert.Equal(t, SettingStatusUntested, settingOf(t, w, SettingLLM).Status, "a new key resets the verdict")
	v, err = w.svc.TestSetting(ctx, w.user, SettingLLM)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusInvalid, v.Status)
	assert.Contains(t, v.StatusReason, "401")
	assert.Equal(t, SettingStatusInvalid, settingOf(t, w, SettingLLM).Status)
}

func TestSettings_TestSetting_AsksTheTelegramCheckWithTheSavedCredentials(t *testing.T) {
	if real, ok := connectors.LookupCredentialCheck(connectors.PlatformTelegram); ok {
		t.Cleanup(func() { connectors.RegisterCredentialCheck(connectors.PlatformTelegram, real) })
	}
	var gotHash string
	verdict := errors.New("API_ID_INVALID")
	connectors.RegisterCredentialCheck(connectors.PlatformTelegram, func(_ context.Context, cfg *config.Config) error {
		gotHash = cfg.TGAPIHash
		return verdict
	})

	w := newWorld(t)
	ctx := context.Background()
	_, err := w.svc.SaveSetting(ctx, w.user, SettingTelegram, "", map[string]string{
		"api_id": "12345", "api_hash": "0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)

	v, err := w.svc.TestSetting(ctx, w.user, SettingTelegram)
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", gotHash)
	assert.Equal(t, SettingStatusInvalid, v.Status)

	verdict = nil
	v, err = w.svc.TestSetting(ctx, w.user, SettingTelegram)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)
}

func TestSettings_SendTestEmail_GoesToTheLoginAddressByDefault(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	sink := startSMTPSink(t)
	w.saveSink(t, sink, "")

	to, err := w.svc.SendTestEmail(ctx, w.user, "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"owner@example.test"}, to)

	_, err = w.svc.SendTestEmail(ctx, w.user, "qa@example.test", "")
	require.NoError(t, err)

	msgs := sink.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, []string{"owner@example.test"}, msgs[0].To)
	assert.Equal(t, []string{"qa@example.test"}, msgs[1].To)

	v, err := w.svc.TestSetting(ctx, w.user, SettingEmail)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)
}

func TestSettings_SendTestEmail_UsesThePlatformServerWhenOnlyRecipientsAreSaved(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	assert.False(t, *settingOf(t, w, SettingEmail).PlatformSMTP)

	sink := startSMTPSink(t)
	port, err := strconv.Atoi(sink.Port)
	require.NoError(t, err)
	w.svc.cfg.SMTPHost, w.svc.cfg.SMTPPort, w.svc.cfg.SMTPFrom, w.svc.cfg.SMTPTLS = sink.Host, port, "platform@example.test", smtpNoTLS

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmail, "", map[string]string{"from": "me@example.test"})
	require.ErrorContains(t, err, "from applies only to your own server")

	v, err := w.svc.SaveSetting(ctx, w.user, SettingEmail, "", map[string]string{"notify_emails": "qa@example.test"})
	require.NoError(t, err)
	assert.True(t, v.Configured)
	assert.True(t, *v.PlatformSMTP)
	assert.True(t, *settingOf(t, w, SettingEmail).PlatformSMTP)

	to, err := w.svc.SendTestEmail(ctx, w.user, "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"qa@example.test"}, to)
	msgs := sink.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "platform@example.test", msgs[0].From)

	v, err = w.svc.TestSetting(ctx, w.user, SettingEmail)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmail, "", map[string]string{
		"host": "smtp.own.example.test", "from": "me@example.test", "password": "own-secret",
	})
	require.NoError(t, err)
	v, err = w.svc.SaveSetting(ctx, w.user, SettingEmail, "", map[string]string{"host": "", "password": ""})
	require.NoError(t, err)
	assert.Empty(t, v.Fields["host"], "an empty host goes back to the platform server")
	assert.Empty(t, v.Fields["password"])
	assert.Equal(t, "qa@example.test", v.Fields["notify_emails"])
	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, sink.Host, cfg.SMTPHost)
}

func TestSettings_SaveSetting_StoresAKeyListMaskedKeyByKey(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.SaveSetting(ctx, w.user, SettingYouTube, "", map[string]string{"api_keys": "yt-key-0001\n yt-key-0002, "})
	require.NoError(t, err)
	_, err = w.svc.SaveSetting(ctx, w.user, SettingWebSearch, "tavily", map[string]string{"api_keys": "tvly-0003,tvly-0004"})
	require.NoError(t, err)

	assert.Equal(t, "••••0001,••••0002", settingOf(t, w, SettingYouTube).Fields["api_keys"])
	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, []string{"yt-key-0001", "yt-key-0002"}, cfg.YouTubeAPIKeys())
	assert.Equal(t, "tvly-0003,tvly-0004", cfg.TavilyAPIKey)
}

func TestSettings_SaveSetting_StoresTheDefaultProxyMasked(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.SaveSetting(ctx, w.user, SettingProxy, "", map[string]string{"url": "socks5://alice:hunter2@10.1.2.3:1080"})
	require.NoError(t, err)

	assert.Equal(t, "socks5://••••@10.1.2.3:1080", settingOf(t, w, SettingProxy).Fields["url"])
	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "socks5://alice:hunter2@10.1.2.3:1080", cfg.GrowthProxyURL)
}

func TestSettings_TestSetting_ChecksEveryKeyOfAList(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	api := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("x-goog-api-key") {
		case "ok-key-0001":
			_, _ = rw.Write([]byte(`{"items":[]}`))
		case "spent-key-0002":
			rw.WriteHeader(http.StatusForbidden)
			_, _ = rw.Write([]byte(`{"error":{"code":403,"message":"` + strings.Repeat("x", 400) +
				`","errors":[{"reason":"quotaExceeded"}]}}`))
		default:
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"API key not valid"}}`))
		}
	}))
	t.Cleanup(api.Close)
	prev := youTubeAPIURL
	youTubeAPIURL = api.URL
	t.Cleanup(func() { youTubeAPIURL = prev })

	_, err := w.svc.SaveSetting(ctx, w.user, SettingYouTube, "", map[string]string{"api_keys": "ok-key-0001,spent-key-0002"})
	require.NoError(t, err)
	v, err := w.svc.TestSetting(ctx, w.user, SettingYouTube)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)
	assert.Equal(t, "quota spent for today: ••••0002", v.StatusReason)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingYouTube, "", map[string]string{"api_keys": "ok-key-0001,bad-key-0003"})
	require.NoError(t, err)
	v, err = w.svc.TestSetting(ctx, w.user, SettingYouTube)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusInvalid, v.Status)
	assert.Contains(t, v.StatusReason, "••••0003: ")
	assert.Contains(t, v.StatusReason, "API key not valid")
}

func TestSettings_ConfigFor_SendsAnAnthropicProtocolEndpointToTheAnthropicProvider(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_, err := w.svc.SaveSetting(ctx, w.user, SettingLLM, "custom", map[string]string{
		"api_key": "gw-key-0001", "server_url": "https://gw.example.test/anthropic", "protocol": "anthropic_messages",
	})
	require.NoError(t, err)

	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "gw-key-0001", cfg.AnthropicAPIKey)
	assert.Equal(t, "https://gw.example.test/anthropic", cfg.AnthropicServerURL)
	assert.Empty(t, cfg.LLMServerKey)
}

func TestSettings_ConfigFor_PicksTheEmbeddingKeyExplicitThenOpenAIThenEnv(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.cfg.EmbeddingProvider, w.svc.cfg.EmbeddingKey, w.svc.cfg.EmbeddingModel = "openai", "env-embed-key", "text-embedding-3-small"

	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "env-embed-key", cfg.EmbeddingKey, "nothing saved: .env")

	_, err = w.svc.SaveSetting(ctx, w.user, SettingLLM, "openai", map[string]string{"api_key": "sk-user"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.EmbeddingProvider)
	assert.Empty(t, cfg.EmbeddingKey, "the OpenAI embedder falls back to the customer's OpenAI key")
	assert.Equal(t, "sk-user", cfg.OpenAIKey)
	assert.Equal(t, "text-embedding-3-small", cfg.EmbeddingModel)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, "voyageai", map[string]string{"api_key": "voy-key"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "voyageai", cfg.EmbeddingProvider, "an explicit embedding setting wins")
	assert.Equal(t, "voy-key", cfg.EmbeddingKey)
	assert.Empty(t, cfg.EmbeddingModel, "another provider does not inherit the .env model")

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, "nowhere", map[string]string{})
	assert.ErrorIs(t, err, ErrInvalidSetting)
}

func TestSettings_TestSetting_EmbedsOnceToTestTheEmbeddingModel(t *testing.T) {
	var auth string
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if auth != "Bearer good-key" {
			rw.WriteHeader(http.StatusUnauthorized)
			_, _ = rw.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`))
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"object":"list","model":"m","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}]}`))
	}))
	t.Cleanup(stub.Close)
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.SaveSetting(ctx, w.user, SettingEmbedding, "openai", map[string]string{"api_key": "bad-key", "server_url": stub.URL})
	require.NoError(t, err)
	v, err := w.svc.TestSetting(ctx, w.user, SettingEmbedding)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusInvalid, v.Status)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, "openai", map[string]string{"api_key": "good-key", "server_url": stub.URL})
	require.NoError(t, err)
	v, err = w.svc.TestSetting(ctx, w.user, SettingEmbedding)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status, v.StatusReason)
	assert.Equal(t, "openai returned a 3-dimension vector", v.StatusReason)
}

func TestSettings_ListSettings_CountsAnEmbedderConfiguredOnlyWithWhatItNeeds(t *testing.T) {
	w := newWorld(t)
	for _, tc := range []struct {
		name string
		cfg  func(*config.Config)
		want bool
	}{
		{"the default provider without a key", func(c *config.Config) { c.EmbeddingProvider = "openai" }, false},
		{"openai with the chat key", func(c *config.Config) { c.EmbeddingProvider, c.OpenAIKey = "openai", "sk-1" }, true},
		{"ollama without a server", func(c *config.Config) { c.EmbeddingProvider = "ollama" }, false},
		{"ollama with a server", func(c *config.Config) { c.EmbeddingProvider, c.OllamaServerURL = "ollama", "http://ollama:11434" }, true},
		{"jina with its key", func(c *config.Config) { c.EmbeddingProvider, c.EmbeddingKey = "jina", "jina-1" }, true},
		{"none", func(c *config.Config) { c.EmbeddingProvider, c.EmbeddingKey = "none", "k" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.svc.cfg.EmbeddingProvider, w.svc.cfg.EmbeddingKey, w.svc.cfg.OpenAIKey, w.svc.cfg.OllamaServerURL = "", "", "", ""
			tc.cfg(w.svc.cfg)
			assert.Equal(t, tc.want, settingOf(t, w, SettingEmbedding).Configured)
		})
	}
}

func TestSettings_ListSettings_SaysWhetherTheJudgeKeyIsConfigured(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.TypeSafeAPIKey = ""
	ctx := context.Background()
	scope := w.seedTask(t)

	v := settingOf(t, w, SettingScreening)
	assert.False(t, v.Configured)
	assert.False(t, settingOf(t, w, SettingTelegram).Configured)
	assert.Equal(t, "0.6", v.Fields["threshold"])
	p, err := w.svc.Progress(ctx, scope)
	require.NoError(t, err)
	assert.ErrorContains(t, ErrNoJudge, p.Warning)
	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, "", map[string]string{"threshold": "0.5"})
	assert.ErrorIs(t, err, ErrInvalidSetting, "the key is required")

	saved, err := w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningCustom, map[string]string{
		"api_key": "ts-secret-5678", "api_url": w.svc.cfg.TypeSafeAPIURL,
	})
	require.NoError(t, err)
	assert.True(t, saved.Configured, "the saved view says so too")
	tested, err := w.svc.TestSetting(ctx, w.user, SettingScreening)
	require.NoError(t, err)
	assert.True(t, tested.Configured, "and so does the tested one")
	v = settingOf(t, w, SettingScreening)
	assert.True(t, v.Configured)
	assert.Equal(t, "••••5678", v.Fields["api_key"])
	p, err = w.svc.Progress(ctx, scope)
	require.NoError(t, err)
	assert.Empty(t, p.Warning)
}

func TestSettings_ConfigFor_PointsJevAtTheChosenProvider(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	secret, err := w.svc.seal(w.user, SettingScreening, storedSetting{Values: map[string]string{"api_key": "ts-legacy"}})
	require.NoError(t, err)
	_, err = w.svc.db.UpsertGrowthSetting(ctx, database.UpsertGrowthSettingParams{UserID: w.user, Kind: SettingScreening, Secret: secret})
	require.NoError(t, err)
	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "ts-legacy", cfg.TypeSafeAPIKey, "a row saved before providers existed is TypeSafe")
	assert.Equal(t, "https://api.typesafe.ai", newJevClient(cfg).ResolvedURL())
	assert.Equal(t, "jev-latest", newJevClient(cfg).ResolvedModel())
	assert.Equal(t, screeningTypeSafe, settingOf(t, w, SettingScreening).Provider)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningOpenRouter, map[string]string{"api_key": "sk-or-1234"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "https://openrouter.ai/api", newJevClient(cfg).ResolvedURL())
	assert.Equal(t, "~typesafe/jev-latest", newJevClient(cfg).ResolvedModel())

	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningOpenRouter, map[string]string{"model": "typesafe/jev-1.13"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "sk-or-1234", cfg.TypeSafeAPIKey)
	assert.Equal(t, "typesafe/jev-1.13", newJevClient(cfg).ResolvedModel())

	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningCustom, map[string]string{"api_key": "k"})
	assert.ErrorContains(t, err, "needs api_url")
	_, err = w.svc.SaveSetting(ctx, w.user, SettingScreening, screeningCustom, map[string]string{"api_key": "k", "api_url": "ftp://jev.example.test"})
	assert.ErrorContains(t, err, "api_url must be an http(s) URL")
}

func TestSettings_ConfigFor_SendsEmbeddingsThroughOpenRouter(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.SaveSetting(ctx, w.user, SettingLLM, "custom", map[string]string{
		"api_key": "sk-or-llm", "server_url": "https://openrouter.ai/api/v1", "model": "deepseek/deepseek-chat",
	})
	require.NoError(t, err)
	cfg, err := w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, []string{"openai", "https://openrouter.ai/api/v1", "openai/text-embedding-3-small", "sk-or-llm"},
		[]string{cfg.EmbeddingProvider, cfg.EmbeddingURL, cfg.EmbeddingModel, cfg.EmbeddingKey},
		"an LLM on OpenRouter lends its key to the embedder")

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, embeddingOpenRouter, map[string]string{"model": "openai/text-embedding-3-large"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, []string{"openai", "https://openrouter.ai/api/v1", "openai/text-embedding-3-large", "sk-or-llm"},
		[]string{cfg.EmbeddingProvider, cfg.EmbeddingURL, cfg.EmbeddingModel, cfg.EmbeddingKey})
	assert.Equal(t, embeddingOpenRouter, settingOf(t, w, SettingEmbedding).Provider)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, embeddingOpenRouter, map[string]string{"api_key": "sk-or-embed"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "sk-or-embed", cfg.EmbeddingKey)

	_, err = w.svc.SaveSetting(ctx, w.user, SettingEmbedding, "ollama", map[string]string{"server_url": "http://ollama:11434"})
	require.NoError(t, err)
	cfg, err = w.svc.ConfigFor(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, []string{"ollama", "http://ollama:11434", "", ""},
		[]string{cfg.EmbeddingProvider, cfg.EmbeddingURL, cfg.EmbeddingModel, cfg.EmbeddingKey},
		"another provider drops what OpenRouter set")
}

func TestSettings_ListSettings_GivesEveryKindAProvidersArray(t *testing.T) {
	w := newWorld(t)
	w.savePace(t, nil)

	views, err := w.svc.ListSettings(context.Background(), w.user)
	require.NoError(t, err)
	for _, v := range views {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"providers":[`, v.Kind)
	}
	saved, err := json.Marshal(settingOf(t, w, SettingTelegram))
	require.NoError(t, err)
	assert.Contains(t, string(saved), `"providers":[]`)
}

func TestSettings_SendTestEmail_WritesInTheLanguageOfTheNewestTask(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	sink := startSMTPSink(t)
	w.saveSink(t, sink, "")
	scope := w.seedTask(t)
	_, err := w.db.Exec(`UPDATE flows SET language = 'Chinese' WHERE id = $1`, scope.FlowID)
	require.NoError(t, err)

	_, err = w.svc.SendTestEmail(ctx, w.user, "", "")
	require.NoError(t, err)
	_, err = w.svc.SendTestEmail(ctx, w.user, "", "English")
	require.NoError(t, err)

	msgs := sink.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "线索交付邮件测试", mailSubject(t, msgs[0].Data))
	assert.Equal(t, "Lead delivery email test", mailSubject(t, msgs[1].Data))
}

func mailSubject(t *testing.T, data string) string {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(data))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	return subject
}
