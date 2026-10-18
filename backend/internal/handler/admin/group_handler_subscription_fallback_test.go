package admin

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionFallbackAdminRequestPresence(t *testing.T) {
	for _, body := range []string{`{}`, `{"subscription_fallback_group_id":null}`, `{"subscription_fallback_group_id":0}`, `{"subscription_fallback_group_id":19}`} {
		var create CreateGroupRequest
		var update UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(body), &create))
		require.NoError(t, json.Unmarshal([]byte(body), &update))
		require.Equal(t, create.SubscriptionFallbackGroupID, update.SubscriptionFallbackGroupID)
		if body == `{}` || body == `{"subscription_fallback_group_id":null}` {
			require.Nil(t, update.SubscriptionFallbackGroupID, "omission/null preserves in partial updates")
		} else {
			require.NotNil(t, update.SubscriptionFallbackGroupID, "zero must remain distinguishable from omission")
			if body == `{"subscription_fallback_group_id":0}` {
				require.Zero(t, *update.SubscriptionFallbackGroupID)
			} else {
				require.EqualValues(t, 19, *update.SubscriptionFallbackGroupID)
			}
		}
	}
}

func TestSubscriptionFallbackHandlerPassesCreateAndPartialUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	h := NewGroupHandlerWithConfig(svc, nil, nil, &config.Config{})
	r := gin.New()
	r.POST("/groups", h.Create)
	r.PUT("/groups/:id", h.Update)
	res := serveGroupRequest(r, http.MethodPost, "/groups", `{"name":"subscription","platform":"openai","subscription_type":"subscription","rate_multiplier":1,"subscription_fallback_group_id":19}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.EqualValues(t, 19, *svc.createdGroups[0].SubscriptionFallbackGroupID)
	res = serveGroupRequest(r, http.MethodPut, "/groups/2", `{"subscription_fallback_group_id":0}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.NotNil(t, svc.updatedGroups[0].SubscriptionFallbackGroupID)
	require.Zero(t, *svc.updatedGroups[0].SubscriptionFallbackGroupID)
	res = serveGroupRequest(r, http.MethodPut, "/groups/2", `{"name":"renamed"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Nil(t, svc.updatedGroups[1].SubscriptionFallbackGroupID)
}

func TestSubscriptionFallbackSimpleModeDropsSubscriptionRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	r := newSimpleModeGroupRouter(svc)
	res := serveGroupRequest(r, http.MethodPost, "/groups", `{"name":"simple","platform":"openai","subscription_type":"subscription","subscription_fallback_group_id":19}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Nil(t, svc.createdGroups[0].SubscriptionFallbackGroupID)
	res = serveGroupRequest(r, http.MethodPut, "/groups/2", `{"subscription_fallback_group_id":19}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Nil(t, svc.updatedGroups[0].SubscriptionFallbackGroupID)
}
