package growth

import (
	"testing"

	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
)

func TestSources_CanonicalLink_OneSpellingPerSource(t *testing.T) {
	tests := []struct {
		in       string
		platform connectors.Platform
		want     string
		wantErr  bool
	}{
		{in: "https://t.me/RpgFans/", platform: connectors.PlatformTelegram, want: "t.me/rpgfans"},
		{in: "@RpgFans", platform: connectors.PlatformTelegram, want: "t.me/rpgfans"},
		{in: "rpgfans", platform: connectors.PlatformTelegram, want: "t.me/rpgfans"},
		{in: "telegram.me/s/RpgFans", platform: connectors.PlatformTelegram, want: "t.me/rpgfans"},
		{in: "https://t.me/+AbCdEf", platform: connectors.PlatformTelegram, want: "t.me/+AbCdEf"},
		{in: "https://t.me/joinchat/AbCdEf", platform: connectors.PlatformTelegram, want: "t.me/+AbCdEf"},
		{in: "t.me/joinchat", platform: connectors.PlatformTelegram, wantErr: true},
		{in: "https://t.me/abc", platform: connectors.PlatformTelegram, wantErr: true},
		{in: "https://www.Forum.example.com/t/42/?utm_source=x&b=2&a=1#reply", platform: connectors.PlatformWeb, want: "forum.example.com/t/42?a=1&b=2"},
		{in: "forum.example.com:8080/t/42", platform: connectors.PlatformWeb, want: "forum.example.com:8080/t/42"},
		{in: "ftp://forum.example.com/t/42", platform: connectors.PlatformWeb, wantErr: true},
		{in: "localhost/x", platform: connectors.PlatformWeb, wantErr: true},
		{in: "https://www.facebook.com/groups/thaigamers/?ref=share", platform: connectors.PlatformFacebook, want: "facebook.com/groups/thaigamers"},
		{in: "https://m.facebook.com/groups/thaigamers/", platform: connectors.PlatformFacebook, want: "facebook.com/groups/thaigamers"},
		{in: "https://www.facebook.com/somepage", platform: connectors.PlatformWeb, want: "facebook.com/somepage"},
		{in: "facebook.com/groups/", platform: connectors.PlatformWeb, want: "facebook.com/groups"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.platform, DetectPlatform(tt.in))
			got, err := CanonicalLink(tt.platform, tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
