package connectors

import (
	"encoding/json"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramRef_ParseTelegramLink(t *testing.T) {
	tests := []struct {
		in   string
		want tgLink
	}{
		{"@mobilegamers", tgLink{Username: "mobilegamers"}},
		{"mobilegamers", tgLink{Username: "mobilegamers"}},
		{"t.me/mobilegamers", tgLink{Username: "mobilegamers"}},
		{"https://t.me/mobilegamers/123", tgLink{Username: "mobilegamers"}},
		{"https://www.telegram.me/mobilegamers", tgLink{Username: "mobilegamers"}},
		{"http://telegram.dog/mobilegamers?start=1", tgLink{Username: "mobilegamers"}},
		{"https://t.me/s/mobilegamers", tgLink{Username: "mobilegamers"}},
		{"https://mobilegamers.t.me", tgLink{Username: "mobilegamers"}},
		{"tg://resolve?domain=mobilegamers", tgLink{Username: "mobilegamers"}},
		{"  https://t.me/+AbCdEfGh123  ", tgLink{Invite: "AbCdEfGh123"}},
		{"https://t.me/joinchat/AbCd-Ef_Gh123", tgLink{Invite: "AbCd-Ef_Gh123"}},
		{"tg://join?invite=AbCdEfGh123", tgLink{Invite: "AbCdEfGh123"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseTelegramLink(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTelegramRef_ParseTelegramLink_Rejects(t *testing.T) {
	for _, in := range []string{
		"",
		"https://example.com/mobilegamers",
		"https://t.me/",
		"https://t.me/c/1001/12",
		"https://t.me/addstickers/pack",
		"https://t.me/joinchat",
		"https://t.me/+short",
		"@ab",
		"tg://msg?to=x",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := parseTelegramLink(in)
			assert.Error(t, err)
		})
	}
}

func TestTelegramRef_PeerRoundTrip(t *testing.T) {
	for _, r := range []tgRef{
		{Kind: refChannel, ID: 1001, Hash: -77, Username: "mobilegamers", Invite: "AbCdEfGh123"},
		{Kind: refChat, ID: 55},
		{Kind: refInvite, Invite: "AbCdEfGh123"},
	} {
		got, err := peerRef(r.peer())
		require.NoError(t, err)
		assert.Equal(t, r, got)
	}
}

func TestTelegramRef_PeerRef_Rejects(t *testing.T) {
	for _, in := range []string{`""`, `{"k":"channel"}`, `{"k":"invite"}`, `{"k":"planet","id":1}`, `{"k":"user","id":5}`, `{"k":"channel","id":"x"}`, `{`} {
		_, err := peerRef(json.RawMessage(in))
		assert.Error(t, err, in)
	}
}

func TestTelegramRef_InputUser(t *testing.T) {
	direct, ok := tgRef{Kind: refUser, ID: 5, Hash: 6}.inputUser()
	require.True(t, ok)
	assert.Equal(t, &tg.InputUser{UserID: 5, AccessHash: 6}, direct)

	viaMsg, ok := tgRef{Kind: refUser, ID: 5, FromPeer: &tgRef{Kind: refChat, ID: 9}, FromMsg: 3}.inputUser()
	require.True(t, ok)
	assert.Equal(t, &tg.InputUserFromMessage{Peer: &tg.InputPeerChat{ChatID: 9}, MsgID: 3, UserID: 5}, viaMsg)

	_, ok = tgRef{Kind: refChannel, ID: 5}.inputUser()
	assert.False(t, ok)
}
