//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type middlewareEpochSettingsRepo struct {
	service.SettingRepository
	epoch string
	err   error
}

func (r *middlewareEpochSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.AuthSessionEpochSettingPrefix+"37" {
		return "", service.ErrSettingNotFound
	}
	if r.err != nil {
		return "", r.err
	}
	if r.epoch == "" {
		return "", service.ErrSettingNotFound
	}
	return r.epoch, nil
}

func TestSessionEpochMiddleware_RevocationAndStorageFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name   string
		err    error
		epoch  string
		status int
		code   string
	}{
		{"database outage", errors.New("database unavailable"), "", http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
		{"revoked session", nil, "0123456789abcdef0123456789abcdef", http.StatusUnauthorized, "TOKEN_REVOKED"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-epoch-middleware-signing-key", ExpireHour: 1}}
			settingsRepo := &middlewareEpochSettingsRepo{}
			settings := service.NewSettingService(settingsRepo, cfg)
			auth := service.NewAuthService(nil, nil, nil, nil, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
			token, err := auth.GenerateToken(context.Background(), &service.User{ID: 37, Role: service.RoleAdmin})
			require.NoError(t, err)
			settingsRepo.err = scenario.err
			settingsRepo.epoch = scenario.epoch
			for _, entry := range []struct {
				name             string
				admin, websocket bool
			}{
				{"user", false, false}, {"admin", true, false}, {"admin websocket", true, true},
			} {
				t.Run(entry.name, func(t *testing.T) {
					// Any user lookup after the epoch failed would panic: the request
					// must stop at the revocation/store check, before business work.
					called := false
					router := gin.New()
					if entry.admin {
						router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(auth, nil, nil, nil)))
					} else {
						router.Use(gin.HandlerFunc(NewJWTAuthMiddleware(auth, nil, nil, nil)))
					}
					router.GET("/protected", func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
					req := httptest.NewRequest(http.MethodGet, "/protected", nil)
					if entry.websocket {
						req.Header.Set("Upgrade", "websocket")
						req.Header.Set("Connection", "Upgrade")
						req.Header.Set("Sec-WebSocket-Protocol", "sub2api-admin, jwt."+token)
					} else {
						req.Header.Set("Authorization", "Bearer "+token)
					}
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					require.Equal(t, scenario.status, w.Code, w.Body.String())
					var body ErrorResponse
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					require.Equal(t, scenario.code, body.Code)
					require.False(t, called)
				})
			}
		})
	}
}
