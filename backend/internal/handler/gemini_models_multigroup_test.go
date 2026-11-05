package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiMultigroupUpstream struct {
	service.HTTPUpstream
	bodies     map[int64]string
	pages      map[string]string
	tokens     []string
	pageStatus int
	status     int
	err        error
}

func (u *geminiMultigroupUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	if id == 2 && u.err != nil {
		return nil, u.err
	}
	status := http.StatusOK
	if id == 2 && u.status != 0 {
		status = u.status
	}
	body := u.bodies[id]
	if token := req.URL.Query().Get("pageToken"); token != "" {
		u.tokens = append(u.tokens, token)
		body = u.pages[token]
		if u.pageStatus != 0 {
			status = u.pageStatus
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"ETag": {"group-only"}, "Last-Modified": {"group-date"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func geminiMultigroupKey(id int64, platform string, allowed, denied []string) *service.APIKey {
	return &service.APIKey{ID: 19, GroupID: &id, Group: &service.Group{ID: id, Platform: platform,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: allowed != nil, Models: allowed}},
		User: &service.User{UserGroupDeniedModels: denied}}
}

func TestGeminiMultigroupNativeCatalogAndRetrieve(t *testing.T) {
	groups := []*service.APIKey{
		geminiMultigroupKey(1, service.PlatformGemini, []string{"gemini-z", "gemini-a", "gemini-hidden", "gemini-mixed"}, []string{"gemini-a", "gemini-hidden"}),
		geminiMultigroupKey(2, service.PlatformGemini, []string{"gemini-z", "gemini-a", "gemini-hidden"}, []string{"gemini-hidden"}),
		geminiMultigroupKey(3, service.PlatformComposite, nil, []string{"gemini-declared-denied"}),
		geminiMultigroupKey(9, service.PlatformOpenAI, nil, nil),
	}
	repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		1: {
			{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "one"}},
			{ID: 11, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-mixed": "gemini-3.8-flash"}}},
			{ID: 12, Platform: service.PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-unmixed": "gemini-3.8-flash"}}},
		},
		2: {{ID: 2, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "two"}}},
		3: {{ID: 3, Platform: service.PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-declared-denied": "gemini-3.8-flash", "gemini-declared": "gemini-3.8-flash"}}}},
	}}}
	u := &geminiMultigroupUpstream{bodies: map[int64]string{
		1: `{"models":[{"name":"models/gemini-z","custom":{"first":true},"inputTokenLimit":123},{"name":"models/gemini-a"},{"name":"models/gemini-hidden"}]}`,
		2: `{"models":[{"name":"gemini-z","custom":{"second":true}},{"name":"models/gemini-a","version":"second-group"},{"name":"models/gemini-hidden"}]}`,
	}}
	h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, u, nil, &config.Config{})}
	request := func(model, query string, aggregate bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models"+query, nil)
		c.Request.Header.Set("If-None-Match", "group-only")
		c.Set(string(middleware.ContextKeyAPIKey), groups[0])
		if !aggregate && groups[0].Group.Platform == service.PlatformComposite {
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGemini))
		}
		if aggregate {
			middleware.SetAPIKeyModelDiscoveryGroups(c, groups)
		}
		if model == "" {
			h.GeminiV1BetaListModels(c)
		} else {
			c.Params = gin.Params{{Key: "model", Value: model}}
			h.GeminiV1BetaGetModel(c)
		}
		return rec
	}
	rec := request("", "", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"models":[{"name":"models/gemini-a","version":"second-group"},{"name":"models/gemini-declared","supportedGenerationMethods":["generateContent","streamGenerateContent"]},{"name":"models/gemini-mixed","supportedGenerationMethods":["generateContent","streamGenerateContent"]},{"name":"models/gemini-z","custom":{"first":true},"inputTokenLimit":123}]}`, rec.Body.String())
	require.Empty(t, rec.Header().Get("ETag"))
	require.Empty(t, rec.Header().Get("Last-Modified"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	for _, model := range []string{"gemini-z", "gemini-a", "gemini-declared", "gemini-mixed", "gemini-hidden", "gemini-unmixed"} {
		t.Run(model, func(t *testing.T) {
			got := request(model, "", true)
			if model == "gemini-hidden" || model == "gemini-unmixed" {
				require.Equal(t, http.StatusNotFound, got.Code, got.Body.String())
			} else {
				require.Equal(t, http.StatusOK, got.Code, got.Body.String())
				if model == "gemini-z" {
					require.Contains(t, got.Body.String(), `"first":true`)
					require.NotContains(t, got.Body.String(), `"second":true`)
				}
			}
		})
	}
	// A later group's failure must fail collection AND retrieve even if an
	// earlier group already supplied the requested model.
	u.status = http.StatusTooManyRequests
	for _, model := range []string{"", "gemini-z"} {
		require.Equal(t, http.StatusTooManyRequests, request(model, "", true).Code)
	}
	u.status = 0
	u.err = errors.New("catalog fetch failed")
	require.Equal(t, http.StatusBadGateway, request("", "", true).Code)
	u.err = nil
	const token = "opaque+/token?next=& value"
	u.bodies[2] = `{"models":[{"name":"models/gemini-a"}],"nextPageToken":"` + token + `"}`
	u.pages = map[string]string{token: `{"models":[{"name":"models/gemini-page-two","version":"page-two"}]}`}
	groups[1].Group.ModelAllowlist.Models = append(groups[1].Group.ModelAllowlist.Models, "gemini-page-two")
	rec = request("", "?pageSize=1&pageToken=legacy-ignored", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"version":"page-two"`)
	require.NotContains(t, rec.Body.String(), "nextPageToken")
	require.Equal(t, []string{token}, u.tokens, "opaque continuation must round-trip through URL encoding")
	u.pageStatus = http.StatusTooManyRequests
	require.Equal(t, http.StatusTooManyRequests, request("gemini-z", "", true).Code)
	u.pageStatus = 0
	u.pages[token] = `{"models":[],"nextPageToken":"` + token + `"}`
	u.tokens = nil
	require.Equal(t, http.StatusBadGateway, request("", "", true).Code)
	require.Equal(t, []string{token}, u.tokens, "stop before refetching a repeated token")

	// Single-group upstream admission still occurs only when an upstream is needed.
	groups[0].ConcurrencyLimit = 1
	require.Equal(t, http.StatusServiceUnavailable, request("", "", false).Code)
	groups[0] = groups[2]
	groups[0].ConcurrencyLimit = 1
	rec = request("", "", false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "gemini-declared-denied")
	groups = []*service.APIKey{geminiMultigroupKey(9, service.PlatformOpenAI, nil, nil)}
	require.Equal(t, http.StatusBadRequest, request("", "", true).Code)
}

func TestGeminiMultigroupForcedAntigravityBoundaries(t *testing.T) {
	groups := []*service.APIKey{
		geminiMultigroupKey(1, service.PlatformGemini, nil, nil),
		geminiMultigroupKey(2, service.PlatformAntigravity, []string{"gemini-z", "gemini-a"}, []string{"gemini-z"}),
		geminiMultigroupKey(3, service.PlatformAntigravity, []string{"gemini-z"}, nil),
	}
	repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		1: {{ID: 1, Platform: service.PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-excluded": "gemini-3.8-flash"}}}},
		2: {{ID: 2, Platform: service.PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-z": "gemini-3.8-flash", "gemini-a": "gemini-3.8-flash"}}}},
		3: {{ID: 3, Platform: service.PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-z": "gemini-3.8-flash"}}}},
	}}}
	h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, nil)}
	request := func(model string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
		c.Set(string(middleware.ContextKeyAPIKey), groups[0])
		middleware.SetAPIKeyModelDiscoveryGroups(c, groups)
		c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)
		if model == "" {
			h.GeminiV1BetaListModels(c)
		} else {
			c.Params = gin.Params{{Key: "model", Value: model}}
			h.GeminiV1BetaGetModel(c)
		}
		return rec
	}
	rec := request("")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"models":[{"name":"models/gemini-a","supportedGenerationMethods":["generateContent","streamGenerateContent"]},{"name":"models/gemini-z","supportedGenerationMethods":["generateContent","streamGenerateContent"]}]}`, rec.Body.String())
	require.Equal(t, http.StatusOK, request("gemini-z").Code)
	require.Equal(t, http.StatusNotFound, request("gemini-excluded").Code)
	// The separate AG endpoint uses its established static catalog, with each
	// group's restrictions applied before the sorted union (no account load).
	defaults := antigravity.DefaultModels()
	a, z := defaults[0].ID, defaults[len(defaults)-1].ID
	if a > z {
		a, z = z, a
	}
	groups[1].Group.ModelAllowlist.Models = []string{z, a}
	groups[1].User.UserGroupDeniedModels = []string{z}
	groups[2].Group.ModelAllowlist.Models = []string{z}
	rec = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1/models", nil)
	middleware.SetAPIKeyModelDiscoveryGroups(c, groups)
	h.AntigravityModels(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Less(t, strings.Index(rec.Body.String(), a), strings.Index(rec.Body.String(), z))
	require.Contains(t, rec.Body.String(), a)
	require.Contains(t, rec.Body.String(), z)
}
