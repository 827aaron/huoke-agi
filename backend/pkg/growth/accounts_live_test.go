//go:build postgres

package growth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/config"

	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accountsByID(t *testing.T, w *world) map[int64]AccountView {
	t.Helper()
	accounts, err := w.svc.ListAccounts(context.Background(), w.user)
	require.NoError(t, err)
	out := make(map[int64]AccountView, len(accounts))
	for _, a := range accounts {
		out[a.ID] = a
	}
	return out
}

func TestAccounts_SearchSources_MovesToTheNextAccountOnARateLimit(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	first := w.firstAccount(t)
	second, backup := w.addAccount(t, "backup")
	w.tg.Err = &connectors.FloodWaitError{Wait: 10 * time.Minute}
	backup.AddSource(tgGroup("800", "fps_arena", "FPS arena"))

	found, err := w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "fps", 10)
	require.NoError(t, err)
	require.Len(t, found, 1)

	accounts := accountsByID(t, w)
	assert.Equal(t, AccountStatusRateLimited, accounts[first].Status)
	assert.False(t, accounts[first].Available)
	require.NotNil(t, accounts[first].RateLimitUntil)
	assert.True(t, accounts[first].RateLimitUntil.After(time.Now().Add(9*time.Minute)))
	assert.Equal(t, AccountStatusActive, accounts[second].Status)
	assert.True(t, accounts[second].Available)
	assert.NotNil(t, accounts[second].LastSuccessAt)

	w.tg.Err = errors.New("a resting account must not be asked")
	_, err = w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "fps", 10)
	require.NoError(t, err)
}

func TestAccounts_SearchSources_WaitsForTheShortestRestWhenAllAreLimited(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	_, backup := w.addAccount(t, "backup")
	w.tg.Err = &connectors.FloodWaitError{Wait: 30 * time.Minute}
	backup.Err = &connectors.FloodWaitError{Wait: 5 * time.Minute}

	_, err := w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "fps", 10)
	var flood *connectors.FloodWaitError
	require.ErrorAs(t, err, &flood)
	assert.Equal(t, 5*time.Minute, flood.Wait)

	_, err = w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "fps", 10)
	require.ErrorAs(t, err, &flood, "both accounts rest; neither is asked")
	assert.Greater(t, flood.Wait, 4*time.Minute)
	assert.LessOrEqual(t, flood.Wait, 5*time.Minute)
}

func TestAccounts_SyncSource_PrefersTheAccountThatJoinedTheSource(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	id := w.mustAddTelegramGroup(t, scope, tgGroup("810", "fighting_game", "Fighting"), tgMessage(1, "1", "hi"))
	_, err := w.svc.JoinSources(ctx, scope, []int64{id})
	require.NoError(t, err)
	_, other := w.addAccount(t, "other")
	other.Err = errors.New("the account that never joined must not read")

	for range 3 {
		page, err := w.svc.SyncSource(ctx, scope, id, DirectionNewer, 10)
		require.NoError(t, err)
		assert.Equal(t, "joined", page.Source.JoinStatus)
	}
}

func TestAccounts_SyncSource_RotatesAwayFromALoggedOutAccount(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	first := w.firstAccount(t)
	_, backup := w.addAccount(t, "backup")
	group := tgGroup("820", "racing_club", "Racing")
	backup.AddSource(group, tgMessage(1, "1", "hello"))
	id := w.mustAddTelegramGroup(t, scope, group, tgMessage(1, "1", "hello"))
	// One call on the backup leaves the first account as the one used least recently.
	_, err := w.svc.SearchSources(ctx, scope, connectors.PlatformTelegram, "racing", 10)
	require.NoError(t, err)
	w.tg.Err = connectors.ErrUnauthorized

	page, err := w.svc.SyncSource(ctx, scope, id, DirectionOlder, 10)
	require.NoError(t, err)
	assert.Len(t, page.Messages, 1)
	assert.Equal(t, AccountStatusNeedsLogin, accountsByID(t, w)[first].Status)
}

func strPtr(s string) *string { return &s }

func TestAccounts_UpdateAccount_SealsTheNetworkIdentityAndShowsItMasked(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	id := w.firstAccount(t)

	v, err := w.svc.UpdateAccount(ctx, w.user, id, AccountUpdate{
		ProxyURL:          strPtr("socks5://bob:hunter2@10.0.0.9:1080"),
		Fingerprint:       json.RawMessage(`{"ua":"Mozilla/5.0","screen":[1920,1080]}`),
		ExternalBrowserWS: strPtr("ws://127.0.0.1:9222/devtools/browser/tok-5f2e"),
	})
	require.NoError(t, err)
	assert.Equal(t, "socks5://••••@10.0.0.9:1080", v.ProxyURL)
	assert.Equal(t, ProxySourceAccount, v.ProxySource)
	assert.JSONEq(t, `{"ua":"Mozilla/5.0","screen":[1920,1080]}`, string(v.Fingerprint))
	assert.Equal(t, "ws://127.0.0.1:9222/••••", v.ExternalBrowserWS)

	var proxy, ws []byte
	require.NoError(t, w.db.QueryRow(`SELECT proxy, external_browser_ws FROM channel_accounts WHERE id = $1`, id).Scan(&proxy, &ws))
	assert.False(t, bytes.Contains(proxy, []byte("hunter2")))
	assert.False(t, bytes.Contains(ws, []byte("tok-5f2e")))

	v, err = w.svc.UpdateAccount(ctx, w.user, id, AccountUpdate{ProxyURL: strPtr(v.ProxyURL), Label: strPtr("main")})
	require.NoError(t, err)
	assert.Equal(t, "socks5://••••@10.0.0.9:1080", v.ProxyURL, "the masked value sent back keeps the proxy")
	assert.Equal(t, "main", v.Label)

	v, err = w.svc.UpdateAccount(ctx, w.user, id, AccountUpdate{ProxyURL: strPtr(""), Fingerprint: json.RawMessage(`null`)})
	require.NoError(t, err)
	assert.Empty(t, v.ProxyURL)
	assert.Equal(t, ProxySourceDirect, v.ProxySource)
	assert.Nil(t, v.Fingerprint)
	assert.Equal(t, "ws://127.0.0.1:9222/••••", v.ExternalBrowserWS)

	for _, bad := range []AccountUpdate{
		{ProxyURL: strPtr("socks4://10.0.0.9:1080")},
		{ExternalBrowserWS: strPtr("file:///tmp/x")},
		{Fingerprint: json.RawMessage(`[1,2]`)},
		{Status: strPtr(AccountStatusNeedsLogin)},
		{Label: strPtr("  ")},
	} {
		_, err := w.svc.UpdateAccount(ctx, w.user, id, bad)
		assert.ErrorIs(t, err, ErrInvalidSetting)
	}

	other := seedUser(t, w.db, "rival@example.test")
	_, err = w.svc.UpdateAccount(ctx, other, id, AccountUpdate{Label: strPtr("mine")})
	assert.ErrorIs(t, err, ErrAccountNotFound)
}

func TestAccounts_SearchSources_GivesTheConnectorTheResolvedProxy(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	const platform = connectors.Platform("proxy_probe")
	var (
		mu    sync.Mutex
		built []connectors.Account
	)
	connectors.Register(platform, func(_ context.Context, _ *config.Config, a connectors.Account) (connectors.Connector, error) {
		mu.Lock()
		defer mu.Unlock()
		built = append(built, a)
		return NewMemoryConnector(platform), nil
	})
	lastBuilt := func() connectors.Account {
		mu.Lock()
		defer mu.Unlock()
		require.NotEmpty(t, built)
		return built[len(built)-1]
	}

	_, err := w.svc.SaveSetting(ctx, w.user, SettingProxy, "", map[string]string{"url": "http://10.9.9.9:3128"})
	require.NoError(t, err)
	acc, err := w.svc.CreateAccount(ctx, w.user, AccountSpec{Platform: platform}, AccountUpdate{
		Fingerprint:       json.RawMessage(`{"ua":"x"}`),
		ExternalBrowserWS: strPtr("ws://127.0.0.1:9222/devtools/browser/abc"),
	})
	require.NoError(t, err)
	assert.Equal(t, ProxySourceGlobal, acc.ProxySource)

	_, err = w.svc.SearchSources(ctx, scope, platform, "rpg", 5)
	require.NoError(t, err)
	got := lastBuilt()
	assert.Equal(t, "http://10.9.9.9:3128", got.Proxy)
	assert.JSONEq(t, `{"ua":"x"}`, string(got.Fingerprint))
	assert.Equal(t, "ws://127.0.0.1:9222/devtools/browser/abc", got.ExternalBrowserWS)

	_, err = w.svc.UpdateAccount(ctx, w.user, acc.ID, AccountUpdate{ProxyURL: strPtr("socks5://u:p@10.8.8.8:1080")})
	require.NoError(t, err)
	_, err = w.svc.SearchSources(ctx, scope, platform, "rpg", 5)
	require.NoError(t, err)
	assert.Equal(t, "socks5://u:p@10.8.8.8:1080", lastBuilt().Proxy, "the account's own proxy wins")
}

func TestAccounts_TestAccountProxy_ReachesTheInternetThroughTheAccountsProxy(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	id := w.firstAccount(t)
	var proxied bool
	fwd := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		proxied = r.URL.Host == "through-proxy.invalid"
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fwd.Close)
	prev := proxyCheckURL
	proxyCheckURL = "http://through-proxy.invalid/"
	t.Cleanup(func() { proxyCheckURL = prev })

	_, err := w.svc.UpdateAccount(ctx, w.user, id, AccountUpdate{ProxyURL: strPtr(fwd.URL)})
	require.NoError(t, err)
	check, err := w.svc.TestAccountProxy(ctx, w.user, id, "")
	require.NoError(t, err)
	assert.True(t, check.OK, check.Error)
	assert.True(t, proxied)
	assert.Equal(t, ProxySourceAccount, check.Source)

	fwd.Close()
	check, err = w.svc.TestAccountProxy(ctx, w.user, id, "")
	require.NoError(t, err)
	assert.False(t, check.OK)
	assert.Contains(t, check.Error, "cannot reach through-proxy.invalid through the proxy")
}

func TestAccounts_PickAccount_AddsTheKeyedAccountOnFirstUseAndHidesIt(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.pickAccount(ctx, w.user, "youtube", 0, map[int64]bool{}, nil)
	assert.ErrorIs(t, err, ErrNoAccount, "no keys, no account")

	w.svc.cfg.YouTubeAPIKey = "yt-1,yt-2"
	acc, err := w.svc.pickAccount(ctx, w.user, "youtube", 0, map[int64]bool{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "youtube", acc.Platform)
	again, err := w.svc.pickAccount(ctx, w.user, "youtube", 0, map[int64]bool{}, nil)
	require.NoError(t, err)
	assert.Equal(t, acc.ID, again.ID)

	views, err := w.svc.ListAccounts(ctx, w.user)
	require.NoError(t, err)
	for _, v := range views {
		assert.NotEqual(t, "youtube", v.Platform)
	}
}

type countingConnector struct {
	*MemoryConnector
	dialogs int
}

func (c countingConnector) CountDialogs(ctx context.Context) (int, error) {
	return c.dialogs, ctx.Err()
}

func TestAccounts_AccountDetail_ListsTheJoinedGroupsAndCountsTheChats(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	joined := w.mustAddTelegramGroup(t, scope, tgGroup("930", "joined_here", "Joined Here"))
	w.mustAddTelegramGroup(t, scope, tgGroup("931", "only_known", "Only Known"))
	_, err := w.svc.JoinSources(ctx, scope, []int64{joined})
	require.NoError(t, err)
	var accountID int64
	require.NoError(t, w.db.QueryRow(`SELECT id FROM channel_accounts WHERE user_id = $1`, w.user).Scan(&accountID))
	w.svc.SetConnector(accountID, countingConnector{MemoryConnector: w.tg, dialogs: 42})

	d, err := w.svc.AccountDetail(ctx, w.user, accountID)
	require.NoError(t, err)
	assert.Equal(t, 1, d.JoinedGroupsCount)
	require.Len(t, d.Groups, 1)
	assert.Equal(t, joined, d.Groups[0].SourceID)
	assert.Equal(t, "Joined Here", d.Groups[0].Title)
	assert.NotNil(t, d.Groups[0].JoinedAt)
	require.NotNil(t, d.DialogsCount)
	assert.Equal(t, 42, *d.DialogsCount)

	_, err = w.svc.AccountDetail(ctx, seedUser(t, w.db, "b@example.test"), accountID)
	assert.ErrorIs(t, err, ErrAccountNotFound)

	_, err = w.db.Exec(`UPDATE channel_accounts SET status = 'needs_login' WHERE id = $1`, accountID)
	require.NoError(t, err)
	d, err = w.svc.AccountDetail(ctx, w.user, accountID)
	require.NoError(t, err)
	assert.Nil(t, d.DialogsCount, "an account that cannot be asked")
	assert.Equal(t, 1, d.JoinedGroupsCount)
}
