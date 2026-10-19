package growth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type cursorFakeQuerier struct {
	database.Querier

	sources []database.GrowthSource
	states  []database.AccountSource
}

func (q cursorFakeQuerier) GetUserGrowthSources(ctx context.Context, _ int64) ([]database.GrowthSource, error) {
	return q.sources, ctx.Err()
}

func (q cursorFakeQuerier) GetUserAccountSources(ctx context.Context, _ int64) ([]database.AccountSource, error) {
	return q.states, ctx.Err()
}

func TestSources_SourceCursors_ReportsTheLastReadingAccountsCursor(t *testing.T) {
	at := func(hour int) sql.NullTime {
		return sql.NullTime{Time: time.Date(2026, 10, 18, hour, 0, 0, 0, time.UTC), Valid: true}
	}
	q := cursorFakeQuerier{
		sources: []database.GrowthSource{
			{ID: 12, Title: "RPG Fans"},
			{ID: 15, Link: "t.me/gachatalk"},
			{ID: 20, Title: "Never read"},
		},
		states: []database.AccountSource{
			{AccountID: 1, SourceID: 12, NewestMessageID: 900, OldestMessageID: 100, MessagesRead: 800, SyncedAt: at(9)},
			{AccountID: 2, SourceID: 12, NewestMessageID: 48213, OldestMessageID: 47001, MessagesRead: 1212, SyncedAt: at(11)},
			{AccountID: 1, SourceID: 15, NewestMessageID: 7300, OldestMessageID: 6100, HistoryExhausted: true,
				MessagesRead: 1200, SyncedAt: at(10)},
			{AccountID: 1, SourceID: 20},
		},
	}
	svc := NewService(q, nil)
	source12 := SourceCursor{SourceID: 12, Title: "RPG Fans", NewestMessageID: 48213, OldestMessageID: 47001,
		MessagesRead: 1212, SyncedAt: at(11).Time}
	source15 := SourceCursor{SourceID: 15, Title: "t.me/gachatalk", NewestMessageID: 7300, OldestMessageID: 6100,
		HistoryExhausted: true, MessagesRead: 1200, SyncedAt: at(10).Time}

	t.Run("named ids keep their order and skip ids outside the pool", func(t *testing.T) {
		got, err := svc.SourceCursors(context.Background(), 3, []int64{15, 99, 12, 20, 15})

		require.NoError(t, err)
		assert.Equal(t, []SourceCursor{source15, source12, {SourceID: 20, Title: "Never read"}}, got)
	})

	t.Run("no ids lists every read source, latest first", func(t *testing.T) {
		got, err := svc.SourceCursors(context.Background(), 3, nil)

		require.NoError(t, err)
		assert.Equal(t, []SourceCursor{source12, source15}, got)
	})
}
