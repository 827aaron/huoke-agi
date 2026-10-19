package growth

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readSheet(t *testing.T, data []byte) [][]string {
	t.Helper()
	require.True(t, strings.HasPrefix(string(data), "\xEF\xBB\xBF"), "the sheet starts with the UTF-8 byte order mark")
	require.Contains(t, string(data), "\r\n", "rows end in CRLF")
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	return rows
}

func TestExport_LeadsCSV_WritesTheWebExportsElevenColumns(t *testing.T) {
	older := time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	captured := time.Date(2026, 10, 16, 9, 5, 0, 0, time.UTC)
	sheet := LeadsCSVContext{
		Lang:        "Chinese",
		SignalNames: map[string]string{"vip": "VIP 玩家"},
		Sources: map[int64]SourceRef{
			7: {Title: "Gacha 交流群", Link: "https://t.me/gacha"},
			8: {Title: "论坛", Link: "https://forum.example.com/board"},
		},
	}
	data := LeadsCSV([]LeadView{
		{
			Platform: "telegram", PlatformUserID: "42", Username: "@gamer", DisplayName: "张三",
			Score: 18, Tier: "qualified", MatchReason: "想换平台, \"提现慢\"",
			Signals: []string{"player", "vip", "unknown"},
			Evidence: []EvidenceView{
				{SourceID: 8, MessageLink: "https://t.me/c/1/4", MessageTime: &older, Excerpt: "old"},
				{SourceID: 7, MessageLink: "https://t.me/c/1/5", MessageTime: &newer, Excerpt: "想换个游戏, \"有推荐吗\""},
			},
		},
		{
			Platform: "web", PlatformUserID: "forum.example.com/u/7", IdentityKind: "profile_url",
			Score: 5, Tier: "watchlist",
			Contacts: []Contact{{Type: "email", Value: "ann@example.test"}, {Type: "whatsapp", Value: "+66 81 234 5678"}},
			Evidence: []EvidenceView{{SourceID: 8, CapturedAt: captured, Excerpt: "quote"}},
		},
		{Platform: "facebook", PlatformUserID: "fb-1", Score: 0, Tier: "watchlist"},
	}, sheet)

	rows := readSheet(t, data)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"昵称", "用户名", "平台", "联系方式", "评分", "信号", "画像", "来源", "原话", "原文链接", "发言时间"}, rows[0])
	assert.Equal(t, []string{
		"张三", "@gamer", "Telegram", "https://t.me/gamer", "18 · 合格", "属于目标人群、VIP 玩家、unknown", "想换平台, \"提现慢\"",
		"Gacha 交流群", "想换个游戏, \"有推荐吗\"", "https://t.me/c/1/5", "2026-10-15 08:30",
	}, rows[1], "the newest evidence is the provenance")
	assert.Equal(t, []string{
		"forum.example.com/u/7", "forum.example.com/u/7", "网页 / 论坛", "邮箱：ann@example.test; WhatsApp：+66 81 234 5678",
		"5 · 待观察", "", "", "论坛", "quote", "https://forum.example.com/board", "2026-10-16 09:05",
	}, rows[2], "the source link stands in for a missing message link, the capture time for a missing message time")
	assert.Equal(t, []string{"fb-1", "fb-1", "Facebook", "", "0 · 待观察", "", "", "", "", "", ""}, rows[3])
}

func TestExport_LeadsCSV_WritesEnglishForAnyOtherLanguage(t *testing.T) {
	said := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	for _, lang := range []string{"en", "Thai", ""} {
		data := LeadsCSV([]LeadView{{
			Platform: "telegram", PlatformUserID: "42", Username: "gamer", Score: 9, Tier: "qualified",
			Signals:  []string{"paying", "custom"},
			Contacts: []Contact{{Type: "phone", Value: "+1 555 0100"}},
			Evidence: []EvidenceView{{SourceID: 3, MessageTime: &said, Excerpt: "q"}},
		}}, LeadsCSVContext{Lang: lang, Sources: map[int64]SourceRef{3: {Title: "Board", Link: "https://b.example"}}})

		rows := readSheet(t, data)
		assert.Equal(t, []string{"Name", "Username", "Platform", "Contact", "Score", "Signals", "Profile", "Source", "Quote", "Link", "Posted at"}, rows[0], lang)
		assert.Equal(t, []string{
			"gamer", "@gamer", "Telegram", "Phone: +1 555 0100", "9 · Qualified", "Shows deal intent、custom", "",
			"Board", "q", "https://b.example", "2026-10-15 08:30",
		}, rows[1], lang)
	}
}

func TestExport_LeadsCSV_WritesTimesInTheContextsZone(t *testing.T) {
	said := time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC)
	riyadh, err := time.LoadLocation("Asia/Riyadh")
	require.NoError(t, err)
	data := LeadsCSV([]LeadView{{
		Platform: "telegram", PlatformUserID: "1", Evidence: []EvidenceView{{MessageTime: &said}},
	}}, LeadsCSVContext{Location: riyadh})

	assert.Equal(t, "2026-10-16 02:30", readSheet(t, data)[1][10])
}

func TestExport_LeadsCSV_ReachesALeadWithoutContactsThroughItsProfile(t *testing.T) {
	cases := []struct {
		name string
		lead LeadView
		want string
	}{
		{"telegram handle", LeadView{Platform: "telegram", PlatformUserID: "1", Username: "ann"}, "https://t.me/ann"},
		{"telegram id only", LeadView{Platform: "telegram", PlatformUserID: "1"}, ""},
		{"full url id", LeadView{Platform: "web", PlatformUserID: "https://x.example/u/1"}, "https://x.example/u/1"},
		{"web path id", LeadView{Platform: "web", PlatformUserID: "x.example/u/1"}, "https://x.example/u/1"},
		{"web bare id", LeadView{Platform: "web", PlatformUserID: "user1"}, ""},
		{"contacts win", LeadView{Platform: "telegram", Username: "ann", Contacts: []Contact{{Type: "line", Value: "ann"}}}, "Line: ann"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, readSheet(t, LeadsCSV([]LeadView{tc.lead}, LeadsCSVContext{}))[1][3])
		})
	}
}

func TestExport_LeadsCSVFileName_NamesTheSheetAfterBusinessScopeAndTime(t *testing.T) {
	at := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
	zh := LeadsCSVContext{Lang: "Chinese"}
	en := LeadsCSVContext{Lang: "English"}

	assert.Equal(t, "潜客_星火工作室_第3轮_20261019-0930.csv", LeadsCSVFileName("星火工作室", zh.RoundScope(3), zh, at))
	assert.Equal(t, "潜客_Star_Fall_A_B_当前筛选结果_20261019-0930.csv", LeadsCSVFileName(" Star Fall A/B ", zh.FilteredScope(), zh, at))
	assert.Equal(t, "潜客_未命名业务_全部已交付_20261019-0930.csv", LeadsCSVFileName("", zh.DeliveredScope(), zh, at))
	assert.Equal(t, "Prospects_Unnamed_business_round-3_20261019-0930.csv", LeadsCSVFileName(" ", en.RoundScope(3), en, at))

	bangkok, err := time.LoadLocation("Asia/Bangkok")
	require.NoError(t, err)
	assert.Equal(t, "Prospects_X_Current_filter_results_20261019-1630.csv",
		LeadsCSVFileName("X", en.FilteredScope(), LeadsCSVContext{Location: bangkok}, at), "the stamp is in the sheet's zone")
}

// sheetStore is the part of the store a sheet's context reads; anything else panics.
type sheetStore struct {
	database.Querier
	flow      database.Flow
	profiles  []database.GrowthProfile
	sources   []database.GrowthSource
	schedules []database.GrowthSchedule
}

func (s sheetStore) GetFlow(ctx context.Context, _ int64) (database.Flow, error) {
	return s.flow, ctx.Err()
}

func (s sheetStore) GetUserGrowthProfiles(ctx context.Context, _ int64) ([]database.GrowthProfile, error) {
	return s.profiles, ctx.Err()
}

func (s sheetStore) GetUserGrowthSources(ctx context.Context, _ int64) ([]database.GrowthSource, error) {
	return s.sources, ctx.Err()
}

func (s sheetStore) GetUserGrowthSchedules(ctx context.Context, p database.GetUserGrowthSchedulesParams) ([]database.GrowthSchedule, error) {
	var out []database.GrowthSchedule
	for _, row := range s.schedules {
		if !p.FlowID.Valid || row.FlowID == p.FlowID {
			out = append(out, row)
		}
	}
	return out, ctx.Err()
}

func TestExport_LeadsCSVContext_GathersLanguageDimensionNamesSourcesAndZone(t *testing.T) {
	stored := Profile{Scoring: &Scoring{Dimensions: []ScoringDimension{
		{Key: "player", Name: "renamed built-in", Points: 3},
		{Key: "vip", Name: "VIP 玩家", Points: 2},
		{Key: "nameless", Points: 1},
	}, Qualified: 5, Watchlist: 2}}
	stored.normalize()
	profile, err := json.Marshal(stored)
	require.NoError(t, err)
	flowSchedule := sql.NullInt64{Int64: 10, Valid: true}
	svc := NewService(sheetStore{
		flow:     database.Flow{ID: 10, UserID: 1, Language: "Simplified Chinese"},
		profiles: []database.GrowthProfile{{ID: 1, UserID: 1, Profile: profile}},
		sources: []database.GrowthSource{
			{ID: 7, Title: "Gacha", Link: "https://t.me/gacha"},
			{ID: 8, Title: "Board", Link: "https://forum.example.com"},
		},
		schedules: []database.GrowthSchedule{
			{ID: 1, Timezone: "Asia/Tokyo"},
			{ID: 2, FlowID: flowSchedule, Timezone: "Asia/Bangkok"},
		},
	}, nil)

	sheet := svc.LeadsCSVContext(context.Background(), 1, 10)

	assert.Equal(t, "Simplified Chinese", sheet.Lang)
	assert.Equal(t, map[string]string{"vip": "VIP 玩家"}, sheet.SignalNames, "built-in signals keep the sheet's own label")
	assert.Equal(t, map[int64]SourceRef{7: {Title: "Gacha", Link: "https://t.me/gacha"}, 8: {Title: "Board", Link: "https://forum.example.com"}}, sheet.Sources)
	assert.Equal(t, "Asia/Bangkok", sheet.Location.String(), "the flow's own schedule sets the zone")

	noFlowSchedule := svc.LeadsCSVContext(context.Background(), 1, 11)
	assert.Equal(t, "Asia/Tokyo", noFlowSchedule.Location.String(), "else any schedule of the customer")

	bare := NewService(sheetStore{flow: database.Flow{ID: 10, UserID: 1}}, nil).LeadsCSVContext(context.Background(), 1, 10)
	assert.Equal(t, "UTC", bare.Location.String())
	assert.Empty(t, bare.SignalNames)
	assert.Empty(t, bare.Sources)
}
