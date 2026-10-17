package connectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnector_Joinable_AgreesWithEachConnectorsCapabilities(t *testing.T) {
	for platform, conn := range map[Platform]Connector{
		PlatformTelegram:  &Telegram{},
		PlatformFacebook:  &Facebook{},
		PlatformInstagram: &Instagram{},
		PlatformYouTube:   &YouTube{},
		PlatformTikTok:    &TikTok{},
	} {
		assert.Equal(t, Has(conn, CapJoinSource), Joinable(platform), platform)
	}
	assert.True(t, Joinable(PlatformTelegram))
	assert.False(t, Joinable(PlatformWeb), "a public page is read without joining")
}
