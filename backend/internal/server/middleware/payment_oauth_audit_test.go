//go:build unit

package middleware

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSquarespaceOAuthImportOmitsEntireCredentialBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditCaptureRepository{}
	audit := service.NewAuditLogService(repo, nil)
	audit.Start()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 77})
		c.Set(string(ContextKeyUserRole), "admin")
		c.Next()
	})
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
	route := "/api/v1/admin/payment/squarespace/oauth/import"
	router.POST(route, func(c *gin.Context) {
		var body map[string]any
		require.NoError(t, c.ShouldBindJSON(&body))
		require.Equal(t, "audit-canary-client", body["client_secret"])
		c.JSON(http.StatusOK, gin.H{"configured": true})
	})
	req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(`{"client_secret":"audit-canary-client","clientSecret":"audit-canary-camel","tokens":{"access_token":"audit-canary-access","refreshToken":"audit-canary-refresh"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	audit.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 1)
	require.Equal(t, "<credential-bearing body omitted>", repo.logs[0].RequestBody)
	serialized, err := json.Marshal(repo.logs[0])
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "audit-canary")
}
