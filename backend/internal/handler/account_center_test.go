//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountCenterHandlerUserRepo struct{ *userHandlerRepoStub }

func (r *accountCenterHandlerUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestAccountCenterAuthorityEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{AccountCenter: config.AccountCenterConfig{Enabled: true, BridgeSecret: strings.Repeat("s", 43), PublicOrigin: "https://account.example.test"}}
	require.NoError(t, cfg.AccountCenter.Validate())
	repo := &accountCenterHandlerUserRepo{userHandlerRepoStub: &userHandlerRepoStub{user: &service.User{ID: 42, Email: "old@example.test", PasswordHash: "private-hash", Username: "Name", Status: service.StatusActive}}}
	settings := &userHandlerEpochRepo{values: map[string]string{}}
	auth := service.NewAuthService(nil, repo, nil, nil, cfg, service.NewSettingService(settings, cfg), nil, nil, nil, nil, nil, nil, nil)
	h := &AuthHandler{cfg: cfg, authService: auth}
	request := func(subject string, headers []string) *httptest.ResponseRecorder {
		r := gin.New()
		r.GET("/identity/:subject", h.AccountCenterIdentity)
		req := httptest.NewRequest(http.MethodGet, "/identity/"+subject, nil)
		for _, v := range headers {
			req.Header.Add("Authorization", v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, headers := range [][]string{nil, {"Bearer browser-jwt"}, {"Bearer " + cfg.AccountCenter.BridgeSecret, "Bearer " + cfg.AccountCenter.BridgeSecret}} {
		w := request("sub2api:42", headers)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.NotContains(t, w.Body.String(), "old@example.test")
	}
	valid := []string{"Bearer " + cfg.AccountCenter.BridgeSecret}
	for _, subject := range []string{"email@example.test", "sub2api:042", "sub2api:0", "sub2api:-1", "42", "sub2api:9223372036854775808"} {
		require.Equal(t, http.StatusBadRequest, request(subject, valid).Code)
	}
	w := request("sub2api:42", valid)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Code int                           `json:"code"`
		Data service.AccountCenterIdentity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "sub2api:42", body.Data.Subject)
	require.True(t, body.Data.Enabled)
	require.Len(t, body.Data.SecurityGeneration, 64)
	require.NotContains(t, w.Body.String(), "private-hash")
	for _, mode := range []string{"disabled", "short_secret", "missing_public_origin", "invalid_public_origin"} {
		t.Run(mode, func(t *testing.T) {
			cfg.AccountCenter.Enabled = true
			cfg.AccountCenter.BridgeSecret = strings.Repeat("s", 43)
			cfg.AccountCenter.PublicOrigin = "https://account.example.test"
			switch mode {
			case "disabled":
				cfg.AccountCenter.Enabled = false
			case "short_secret":
				cfg.AccountCenter.BridgeSecret = "short"
			case "missing_public_origin":
				cfg.AccountCenter.PublicOrigin = ""
			case "invalid_public_origin":
				cfg.AccountCenter.PublicOrigin = "http://account.example.test/path"
			}
			for _, headers := range [][]string{nil, valid} {
				w := request("sub2api:42", headers)
				require.Equal(t, http.StatusNotFound, w.Code)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), "old@example.test")
			}
		})
	}
}
