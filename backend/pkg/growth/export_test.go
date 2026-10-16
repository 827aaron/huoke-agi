package growth

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport_LeadsCSV_OneRowPerLeadWithItsFirstEvidence(t *testing.T) {
	delivered := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	said := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	data := LeadsCSV([]LeadView{
		{
			ID: 1, Platform: "telegram", PlatformUserID: "42", IdentityKind: "platform_id", Username: "gamer",
			DisplayName: "张三", Score: 9, Tier: "qualified", MatchReason: "player+3, quitting+3",
			DeliveredAt: &delivered,
			Evidence: []EvidenceView{
				{MessageLink: "https://t.me/c/1/5", MessageTime: &said, Excerpt: "想换个游戏, \"有推荐吗\""},
				{Excerpt: "second"},
			},
		},
		{ID: 2, Platform: "web", PlatformUserID: "forum.example.com/u/7", IdentityKind: "profile_url", Score: 7, Tier: "qualified"},
	})

	require.True(t, strings.HasPrefix(string(data), "\xEF\xBB\xBF"))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{
		"1", "telegram", "42", "gamer", "张三", "https://t.me/gamer", "9", "qualified", "player+3, quitting+3",
		"想换个游戏, \"有推荐吗\"", "https://t.me/c/1/5", "2026-10-15T08:30:00Z", "2", "2026-10-16T09:00:00Z",
	}, rows[1])
	assert.Equal(t, "https://forum.example.com/u/7", rows[2][5])
	assert.Equal(t, "0", rows[2][12])
}
