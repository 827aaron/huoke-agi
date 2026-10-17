package services

import (
	"context"
	"net/http"
	"net/http/httptest"
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
