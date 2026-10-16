//go:build postgres

package growth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

	to, err := w.svc.SendTestEmail(ctx, w.user, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"owner@example.test"}, to)

	_, err = w.svc.SendTestEmail(ctx, w.user, "qa@example.test")
	require.NoError(t, err)

	msgs := sink.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, []string{"owner@example.test"}, msgs[0].To)
	assert.Equal(t, []string{"qa@example.test"}, msgs[1].To)

	v, err := w.svc.TestSetting(ctx, w.user, SettingEmail)
	require.NoError(t, err)
	assert.Equal(t, SettingStatusConnected, v.Status)
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
