package setup

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	maxSetupBodyBytes = 64 * 1024
	maxSetupRequests  = 4
)

// BootstrapToken authorizes this installation only. The generated credential is
// returned to the local launcher, never through a public HTTP endpoint.
func BootstrapToken() (token string, generated bool, err error) {
	if configured, ok := os.LookupEnv("SETUP_TOKEN"); ok {
		if len(configured) < 32 || len(configured) > 256 || strings.IndexFunc(configured, func(r rune) bool { return r < '!' || r > '~' }) != -1 {
			return "", false, fmt.Errorf("SETUP_TOKEN must contain 32 to 256 printable ASCII characters without spaces")
		}
		return configured, false, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generate setup token: %w", err)
	}
	return hex.EncodeToString(buf), true, nil
}

func bootstrapGuard(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Setup-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			response.Error(c, http.StatusUnauthorized, "A valid setup token is required")
			c.Abort()
			return
		}
		c.Next()
	}
}

// Setup JSON is small. Buffer it with a strict bound before the decoder or
// installation mutex; this also rejects oversized chunked bodies and trailing
// junk that a single JSON decode would otherwise leave unread.
func setupRequestLimits() gin.HandlerFunc {
	slots := make(chan struct{}, maxSetupRequests)
	return func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			response.Error(c, http.StatusTooManyRequests, "Too many setup requests")
			c.Abort()
			return
		}
		body := http.MaxBytesReader(c.Writer, c.Request.Body, maxSetupBodyBytes)
		defer func() { _ = body.Close() }()
		data, err := io.ReadAll(body)
		if err != nil {
			var limitErr *http.MaxBytesError
			if errors.As(err, &limitErr) {
				response.Error(c, http.StatusRequestEntityTooLarge, "Setup request exceeds 64 KiB")
			} else {
				response.Error(c, http.StatusBadRequest, "Could not read setup request")
			}
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(data))
		c.Next()
	}
}
