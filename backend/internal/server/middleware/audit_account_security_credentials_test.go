package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountSecurityCredentialAuditOmitsRequestAndTokenResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &auditCaptureRepository{}
	audit := service.NewAuditLogService(repository, nil)
	audit.Start()
	defer audit.Stop()
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(ContextKeyUser), AuthSubject{UserID: 77}); c.Next() })
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
	body := `{"old_password":"old-canary","new_password":"new-canary","password":"pwd-canary","verify_code":"code-canary"}`
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/user/account-security/password"},
		{http.MethodPost, "/api/v1/user/account-security/email"},
	} {
		require.Contains(t, auditBodyOmittedRoutes, endpoint.method+" "+endpoint.path)
		router.Handle(endpoint.method, endpoint.path, func(c *gin.Context) {
			raw, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(raw))
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"access_token": "access-canary", "refresh_token": "refresh-canary"}})
		})
		req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code)
	}
	audit.Stop()
	repository.mu.Lock()
	logs := append([]*service.AuditLog(nil), repository.logs...)
	repository.mu.Unlock()
	require.Len(t, logs, 2)
	for _, entry := range logs {
		require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
		serialized, err := json.Marshal(entry)
		require.NoError(t, err)
		for _, secret := range []string{"old-canary", "new-canary", "pwd-canary", "code-canary", "access-canary", "refresh-canary"} {
			require.False(t, strings.Contains(string(serialized), secret), "audit must not persist credential request or token response values")
		}
	}
}
