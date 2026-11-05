package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMultiGroupModelsUnionAndRetrieve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groups := []*service.APIKey{
		{Group: &service.Group{ID: 1, Platform: service.PlatformAnthropic, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"shared", "a"}}}, User: &service.User{UserGroupDeniedModels: []string{"shared"}}},
		{Group: &service.Group{ID: 2, Platform: service.PlatformGrok}, User: &service.User{}},
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		1: {{Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"shared": "shared", "a": "a", "hidden": "hidden"}}}},
		2: {{Platform: service.PlatformGrok, Credentials: map[string]any{"model_mapping": map[string]any{"shared": "shared", "b": "b"}}}},
	}})
	for _, tc := range []struct {
		name, model string
		codex       bool
		status      int
	}{
		{name: "ordinary", status: 200}, {name: "codex", codex: true, status: 200},
		{name: "retrieve through second group", model: "shared", status: 200},
		{name: "missing model", model: "hidden", status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			c.Request.Header.Set("If-None-Match", "group-specific-etag")
			c.Set(string(middleware.ContextKeyAPIKey), groups[0])
			if tc.model != "" {
				c.Params = gin.Params{{Key: "model", Value: tc.model}}
			}
			middleware.SetAPIKeyModelDiscoveryGroups(c, groups)
			if tc.codex {
				h.CodexModels(c)
			} else {
				h.Models(c)
			}
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Empty(t, w.Header().Get("ETag"))
			if tc.status != 200 {
				return
			}
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if tc.model != "" {
				require.JSONEq(t, `"shared"`, string(body["id"]))
				return
			}
			field, idField := "data", "id"
			if tc.codex {
				field, idField = "models", "slug"
			}
			var entries []map[string]any
			require.NoError(t, json.Unmarshal(body[field], &entries))
			ids := make([]string, 0, len(entries))
			for _, entry := range entries {
				ids = append(ids, entry[idField].(string))
			}
			require.Equal(t, []string{"a", "b", "shared"}, ids)
			// Shared is retained from the second group, including its provider metadata.
			if !tc.codex {
				require.Equal(t, "xai", entries[2]["owned_by"])
			}
		})
	}
}

type failedDiscoveryModelsRepo struct{ gatewayModelsAccountRepoStub }

func (r *failedDiscoveryModelsRepo) ListSchedulableByGroupID(ctx context.Context, id int64) ([]service.Account, error) {
	if id == 2 {
		return nil, errors.New("storage unavailable")
	}
	return r.gatewayModelsAccountRepoStub.ListSchedulableByGroupID(ctx, id)
}

func TestMultiGroupModelsDoesNotReturnPartialSuccess(t *testing.T) {
	h := newGatewayModelsHandlerForTest(&failedDiscoveryModelsRepo{gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		1: {{Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"a": "a"}}}},
	}}})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	h.multiGroupModels(c, []*service.APIKey{
		{Group: &service.Group{ID: 1, Platform: service.PlatformAnthropic}},
		{Group: &service.Group{ID: 2, Platform: service.PlatformGrok}},
	}, false)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), `"data"`)
}
