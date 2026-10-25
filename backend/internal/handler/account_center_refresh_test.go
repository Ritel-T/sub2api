//go:build unit

package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Persist JSON just like the production cache, so the response is compared with
// the serialized authoritative record rather than a separately computed TTL.
type accountCenterResponseRefreshCache struct {
	service.RefreshTokenCache
	records map[string][]byte
}

func (c *accountCenterResponseRefreshCache) StoreRefreshToken(_ context.Context, hash string, data *service.RefreshTokenData, _ time.Duration) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.records[hash] = encoded
	return nil
}
func (c *accountCenterResponseRefreshCache) GetRefreshToken(_ context.Context, hash string) (*service.RefreshTokenData, error) {
	encoded, ok := c.records[hash]
	if !ok {
		return nil, service.ErrRefreshTokenNotFound
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal(encoded, &data); err != nil {
		return nil, err
	}
	return &data, nil
}
func (c *accountCenterResponseRefreshCache) ConsumeRefreshToken(_ context.Context, hash string) (bool, error) {
	if _, ok := c.records[hash]; !ok {
		return false, nil
	}
	delete(c.records, hash)
	return true, nil
}
func (*accountCenterResponseRefreshCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}
func (*accountCenterResponseRefreshCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func accountCenterResponseStored(t *testing.T, c *accountCenterResponseRefreshCache, token string) *service.RefreshTokenData {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	stored, err := c.GetRefreshToken(context.Background(), hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	return stored
}

func TestAccountCenterRefreshResponse_MatchesStoredDeadlineAndRotatesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "synthetic-refresh-response-key", ExpireHour: 1, RefreshTokenExpireDays: 11}, AccountCenter: config.AccountCenterConfig{Enabled: enabled}}
			user := &service.User{ID: 42, Email: "user@example.test", PasswordHash: "synthetic-hash", Role: service.RoleAdmin, Status: service.StatusActive}
			settings := service.NewSettingService(&userHandlerEpochRepo{values: map[string]string{}}, cfg)
			cache := &accountCenterResponseRefreshCache{records: make(map[string][]byte)}
			auth := service.NewAuthService(nil, &userHandlerRepoStub{user: user}, nil, cache, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
			h := &AuthHandler{authService: auth, settingSvc: settings, cfg: cfg}
			router := gin.New()
			router.POST("/login", func(c *gin.Context) { respondWithTokenPair(c, auth, user) })
			router.POST("/api/v1/auth/refresh", h.RefreshToken)
			request := func(path string, body []byte) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				return recorder
			}
			loginResponse := request("/login", nil)
			require.Equal(t, http.StatusOK, loginResponse.Code, loginResponse.Body.String())
			var login struct {
				Data AuthResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(loginResponse.Body.Bytes(), &login))
			stored := accountCenterResponseStored(t, cache, login.Data.RefreshToken)
			require.Equal(t, 11*24*time.Hour, stored.ExpiresAt.Sub(stored.CreatedAt))
			if enabled {
				require.Equal(t, stored.ExpiresAt.Unix(), login.Data.RefreshExpiresAt)
				require.NotEmpty(t, stored.AccountGeneration)
			} else {
				require.NotContains(t, loginResponse.Body.String(), `"refresh_expires_at"`)
				require.Empty(t, stored.AccountGeneration)
			}
			original := *stored
			body, err := json.Marshal(RefreshTokenRequest{RefreshToken: login.Data.RefreshToken})
			require.NoError(t, err)
			refreshedResponse := request("/api/v1/auth/refresh", body)
			require.Equal(t, http.StatusOK, refreshedResponse.Code, refreshedResponse.Body.String())
			var refreshed struct {
				Data RefreshTokenResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(refreshedResponse.Body.Bytes(), &refreshed))
			require.NotEqual(t, login.Data.RefreshToken, refreshed.Data.RefreshToken)
			stored = accountCenterResponseStored(t, cache, refreshed.Data.RefreshToken)
			require.Equal(t, original.FamilyID, stored.FamilyID)
			require.Equal(t, original.AccountGeneration, stored.AccountGeneration)
			if enabled {
				require.Equal(t, stored.ExpiresAt.Unix(), refreshed.Data.RefreshExpiresAt)
			} else {
				require.NotContains(t, refreshedResponse.Body.String(), `"refresh_expires_at"`)
			}
			replay := request("/api/v1/auth/refresh", body)
			require.Equal(t, http.StatusUnauthorized, replay.Code, replay.Body.String())
			require.Len(t, cache.records, 1)
			if enabled {
				user.TotpEnabled = true
				body, err = json.Marshal(RefreshTokenRequest{RefreshToken: refreshed.Data.RefreshToken})
				require.NoError(t, err)
				rejected := request("/api/v1/auth/refresh", body)
				require.Equal(t, http.StatusUnauthorized, rejected.Code, rejected.Body.String())
				require.Contains(t, rejected.Body.String(), "TOKEN_REVOKED")
				require.Len(t, cache.records, 1, "stale generation must not consume its refresh record")
			}
		})
	}
}
