//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountSecurityCredentialHandlerRequiresNativeSubjectAndStrictInput(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/user/account-security/password", `{"old_password":"old-password","new_password":"new-password"}`},
		{http.MethodPost, "/api/v1/user/account-security/email", `{"email":"new@example.test","verify_code":"123456","password":"old-password"}`},
	} {
		for _, mode := range []string{"no_subject", "missing_bearer", "duplicate_bearer", "foreign_uid", "forged", "client_proof", "trailing_json", "empty_body"} {
			t.Run(endpoint.method+"_"+mode, func(t *testing.T) {
				f := newResetHandlerFixture(t)
				r := gin.New()
				r.Use(func(c *gin.Context) {
					if mode != "no_subject" {
						c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: f.operator.ID})
					}
				})
				r.PUT("/api/v1/user/account-security/password", f.h.AccountSecurityPassword)
				r.POST("/api/v1/user/account-security/email", f.h.AccountSecurityEmail)
				body := endpoint.body
				status := http.StatusUnauthorized
				token := f.token(t, f.operator, []string{"pwd"}, 0)
				switch mode {
				case "foreign_uid":
					token = f.token(t, f.target, []string{"pwd"}, 0)
				case "forged":
					token += "invalid"
				case "client_proof":
					body = strings.TrimSuffix(body, "}") + `,"user_id":77,"generation":"forged","auth_time":9999999999,"amr":["pwd","otp"]}`
					status = http.StatusBadRequest
				case "trailing_json":
					body += " {}"
					status = http.StatusBadRequest
				case "empty_body":
					body = ""
					status = http.StatusBadRequest
				}
				req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if mode != "missing_bearer" {
					req.Header.Add("Authorization", "Bearer "+token)
				}
				if mode == "duplicate_bearer" {
					req.Header.Add("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, status, w.Code, w.Body.String())
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), "access_token")
				require.NotContains(t, w.Body.String(), "old-password")
			})
		}
	}
}
