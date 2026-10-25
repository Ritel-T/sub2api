//go:build unit

package handler

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"os"
	"time"
)

// Synthetic-only timing instrument. Original handlers issue and persist their
// actual grants; only the returned lab AT's exp is shortened with the same lab
// signing key. The unmodified original JWT verifier enforces that expiry.
func accountCenterNativeShortAccess(next gin.HandlerFunc, cfg *config.Config) gin.HandlerFunc {
	if os.Getenv("RITEL_NATIVE_SHORT_AT") != "1" {
		return next
	}
	return func(c *gin.Context) {
		recorder := httptest.NewRecorder()
		inner, _ := gin.CreateTestContext(recorder)
		inner.Request = c.Request
		next(inner)
		raw := recorder.Body.Bytes()
		var body map[string]any
		if recorder.Code == 200 && json.Unmarshal(raw, &body) == nil {
			if data, ok := body["data"].(map[string]any); ok {
				if access, ok := data["access_token"].(string); ok {
					claims := &service.JWTClaims{}
					parsed, err := jwt.ParseWithClaims(access, claims, func(token *jwt.Token) (any, error) { return []byte(cfg.JWT.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}))
					if err != nil || !parsed.Valid {
						c.AbortWithStatus(500)
						return
					}
					claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(8 * time.Second))
					token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWT.Secret))
					if err != nil {
						c.AbortWithStatus(500)
						return
					}
					data["access_token"] = token
					data["expires_in"] = 8
					raw, err = json.Marshal(body)
					if err != nil {
						c.AbortWithStatus(500)
						return
					}
				}
			}
		}
		for k, values := range recorder.Header() {
			for _, v := range values {
				c.Writer.Header().Add(k, v)
			}
		}
		c.Data(recorder.Code, recorder.Header().Get("Content-Type"), raw)
	}
}
