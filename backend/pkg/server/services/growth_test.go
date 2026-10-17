package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type growthSettingsQuerier struct {
	database.Querier
}

func (growthSettingsQuerier) DeleteUserGrowthSetting(ctx context.Context, _ database.DeleteUserGrowthSettingParams) (int64, error) {
	return 1, ctx.Err()
}

func TestGrowth_DeleteSetting_RepointsTheFlowsOnlyWhenTheLLMKeyChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewGrowthService(growth.NewService(growthSettingsQuerier{}, &config.Config{}))
	var repointed []int64
	svc.RepointProvidersWith(func(_ context.Context, userID int64) error {
		repointed = append(repointed, userID)
		return nil
	})
	remove := func(kind string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("uid", uint64(7))
		c.Params = gin.Params{{Key: "kind", Value: kind}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/growth/settings/"+kind, nil)
		svc.DeleteSetting(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	remove(growth.SettingYouTube)
	assert.Empty(t, repointed)
	remove(growth.SettingLLM)
	assert.Equal(t, []int64{7}, repointed)
}

func TestGrowth_CreateSchedule_TellsThePageWhyItIsRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewGrowthService(growth.NewService(growthSettingsQuerier{}, &config.Config{}))
	for _, tc := range []struct {
		name, body, reason string
	}{
		{"same start and end", `{"flow_id":3,"days":"daily","start_time":"09:00","end_time":"09:00","timezone":"Asia/Bangkok"}`,
			"end_time must differ from start_time"},
		{"unreadable body", `{"flow_id":"3"}`, "flow_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("uid", uint64(7))
			c.Request = httptest.NewRequest(http.MethodPost, "/growth/schedules", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")

			svc.CreateSchedule(c)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var body struct{ Code, Msg string }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "Growth.InvalidRequest", body.Code)
			assert.Contains(t, body.Msg, tc.reason)
		})
	}
}
