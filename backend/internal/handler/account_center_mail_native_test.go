//go:build unit

package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Test-only mailbox, populated exclusively after accepting SMTP DATA. It never
// reads codes from Redis, persists messages, logs credentials, or relays mail.
type accountCenterNativeDelivery struct {
	Code          string `json:"code"`
	DeliveryCount int    `json:"deliveryCount"`
}

type accountCenterNativeMailbox struct {
	listener net.Listener
	password string
	mu       sync.Mutex
	conn     net.Conn
	closed   bool
	messages map[string]accountCenterNativeDelivery
}

func startAccountCenterNativeSMTP(t *testing.T) *accountCenterNativeMailbox {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	password := make([]byte, 32)
	_, err = rand.Read(password)
	require.NoError(t, err)
	m := &accountCenterNativeMailbox{listener: listener, password: hex.EncodeToString(password), messages: make(map[string]accountCenterNativeDelivery)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				_ = conn.Close()
				return
			}
			m.conn = conn
			m.mu.Unlock()
			m.receive(conn)
			_ = conn.Close()
			m.mu.Lock()
			m.conn = nil
			m.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		m.mu.Lock()
		m.closed = true
		if m.conn != nil {
			_ = m.conn.Close()
		}
		m.mu.Unlock()
		_ = listener.Close()
		<-done
	})
	return m
}

func (m *accountCenterNativeMailbox) smtpSettings() map[string]string {
	return map[string]string{
		service.SettingKeySMTPHost:     "127.0.0.1",
		service.SettingKeySMTPPort:     strconv.Itoa(m.listener.Addr().(*net.TCPAddr).Port),
		service.SettingKeySMTPUsername: "native-fixture",
		service.SettingKeySMTPPassword: m.password,
		service.SettingKeySMTPFrom:     "sender@example.test",
		service.SettingKeySMTPFromName: "Native synthetic fixture",
		service.SettingKeySMTPUseTLS:   "false",
	}
}

func accountCenterNativeSyntheticEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && strings.HasSuffix(email, "@example.test") && !strings.ContainsAny(email, "\r\n")
}

var accountCenterNativeMailCode = regexp.MustCompile(`<div class="code">\s*([0-9]{6})\s*</div>`)

// This deliberately accepts only the original EmailService's verification
// template and synthetic recipient envelope, with no fallback code source.
func (m *accountCenterNativeMailbox) capture(recipient string, data []byte) bool {
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil || !accountCenterNativeSyntheticEmail(recipient) {
		return false
	}
	to, err := mail.ParseAddress(message.Header.Get("To"))
	if err != nil || to.Address != recipient {
		return false
	}
	var body io.Reader = message.Body
	switch strings.ToLower(message.Header.Get("Content-Transfer-Encoding")) {
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	case "", "7bit", "8bit":
	default:
		return false
	}
	decoded, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return false
	}
	matches := accountCenterNativeMailCode.FindAllSubmatch(decoded, -1)
	if len(matches) != 1 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delivery := m.messages[recipient]
	delivery.Code = string(matches[0][1])
	delivery.DeliveryCount++
	m.messages[recipient] = delivery
	return true
}

func (m *accountCenterNativeMailbox) receive(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := textproto.NewReader(bufio.NewReader(conn))
	reply := func(text string) { _, _ = fmt.Fprint(conn, text+"\r\n") }
	reply("220 localhost synthetic fixture")
	authenticated, sender := false, false
	recipient := ""
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "):
			reply("250-localhost\r\n250 AUTH PLAIN")
		case strings.HasPrefix(line, "AUTH PLAIN "):
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
			authenticated = err == nil && subtle.ConstantTimeCompare(decoded, []byte("\x00native-fixture\x00"+m.password)) == 1
			if authenticated {
				reply("235 authenticated")
			} else {
				reply("535 authentication failed")
			}
		case strings.HasPrefix(line, "MAIL FROM:"):
			sender, recipient = false, ""
			if authenticated && line == "MAIL FROM:<sender@example.test>" {
				sender = true
				reply("250 ok")
			} else {
				reply("550 synthetic sender required")
			}
		case strings.HasPrefix(line, "RCPT TO:<") && strings.HasSuffix(line, ">"):
			email := strings.TrimSuffix(strings.TrimPrefix(line, "RCPT TO:<"), ">")
			if sender && recipient == "" && accountCenterNativeSyntheticEmail(email) {
				recipient = email
				reply("250 ok")
			} else {
				reply("550 one synthetic recipient required")
			}
		case line == "DATA":
			if !sender || recipient == "" {
				reply("503 envelope required")
				continue
			}
			reply("354 send data")
			data, err := io.ReadAll(io.LimitReader(reader.DotReader(), (1<<20)+1))
			if err != nil || len(data) > 1<<20 {
				return
			}
			if m.capture(recipient, data) {
				reply("250 accepted")
			} else {
				reply("550 verification message required")
			}
			sender, recipient = false, ""
		case line == "RSET":
			sender, recipient = false, ""
			reply("250 reset")
		case line == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unsupported")
		}
	}
}

func (m *accountCenterNativeMailbox) handler(bridgeSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		headers := c.Request.Header.Values("Authorization")
		if bridgeSecret == "" || len(headers) != 1 || subtle.ConstantTimeCompare([]byte(headers[0]), []byte("Bearer "+bridgeSecret)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		emails := c.Request.URL.Query()["email"]
		if len(emails) != 1 || !accountCenterNativeSyntheticEmail(emails[0]) {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		delivery := m.messages[emails[0]]
		m.mu.Unlock()
		c.JSON(http.StatusOK, delivery)
	}
}

// Only configuration and user existence are stubbed for this focused unit test.
// Redis wire protocol (miniredis), repository.NewEmailCache, original handlers,
// EmailQueueService, EmailService, SMTP and verification consumption are exercised.
type accountCenterNativeMailSettings struct {
	service.SettingRepository
	values map[string]string
}

func (s *accountCenterNativeMailSettings) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (s *accountCenterNativeMailSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = s.values[key]
	}
	return values, nil
}

type accountCenterNativeMailUsers struct{ service.UserRepository }

func (accountCenterNativeMailUsers) ExistsByEmail(context.Context, string) (bool, error) {
	return false, nil
}
func (accountCenterNativeMailUsers) ExistsByEmailAlias(context.Context, string) (bool, error) {
	return false, nil
}

func TestAccountCenterNativeMailDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mailbox := startAccountCenterNativeSMTP(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := repository.NewEmailCache(rdb)
	values := mailbox.smtpSettings()
	values[service.SettingKeyRegistrationEnabled] = "true"
	values[service.SettingKeyEmailVerifyEnabled] = "true"
	repo := &accountCenterNativeMailSettings{values: values}
	cfg := &config.Config{}
	settings := service.NewSettingService(repo, cfg)
	emailSvc := service.NewEmailService(repo, cache)
	queue := service.NewEmailQueueService(emailSvc, 1)
	defer queue.Stop()
	auth := service.NewAuthService(nil, accountCenterNativeMailUsers{}, nil, nil, cfg, settings, emailSvc, nil, queue, nil, nil, nil, nil)
	h := NewAuthHandler(cfg, auth, nil, settings, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/v1/auth/send-verify-code", h.SendVerifyCode)
	router.POST("/api/v1/auth/register", h.Register)
	bridgeSecret := mailbox.password // Generated per test, never printed.
	router.GET("/_fixture/mailbox", mailbox.handler(bridgeSecret))
	request := func(method, path, body string, authorization []string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for _, value := range authorization {
			req.Header.Add("Authorization", value)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	valid := []string{"Bearer " + bridgeSecret}
	readMailbox := func(email string) accountCenterNativeDelivery {
		w := request(http.MethodGet, "/_fixture/mailbox?email="+email, "", valid)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var delivery accountCenterNativeDelivery
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &delivery))
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
		require.Len(t, fields, 2)
		return delivery
	}
	const recipient = "new@example.test"
	for _, headers := range [][]string{nil, {"Bearer wrong"}, {valid[0], valid[0]}} {
		w := request(http.MethodGet, "/_fixture/mailbox?email="+recipient, "", headers)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Empty(t, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	for _, query := range []string{"", "email=someone@example.com", "email=new@example.test&email=other@example.test"} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, "/_fixture/mailbox?"+query, "", valid).Code)
	}
	require.Zero(t, readMailbox(recipient).DeliveryCount)
	registerBody := `{"email":"new@example.test","password":"` + mailbox.password + `"}`
	missing := request(http.MethodPost, "/api/v1/auth/register", registerBody, nil)
	require.Equal(t, http.StatusBadRequest, missing.Code)
	require.Contains(t, missing.Body.String(), "EMAIL_VERIFY_REQUIRED")
	w := request(http.MethodPost, "/api/v1/auth/send-verify-code", `{"email":"new@example.test"}`, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Eventually(t, func() bool { return readMailbox(recipient).DeliveryCount == 1 }, 3*time.Second, 10*time.Millisecond)
	delivery := readMailbox(recipient)
	require.True(t, regexp.MustCompile(`^[0-9]{6}$`).MatchString(delivery.Code), "captured a six digit code")
	stored, err := cache.GetVerificationCode(context.Background(), recipient)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.True(t, stored.Code == delivery.Code, "SMTP and original Redis cache agree")
	require.Zero(t, readMailbox("other@example.test").DeliveryCount)
	// A second actual delivery is independent, and its count is recipient-scoped.
	require.NoError(t, emailSvc.SendVerifyCode(context.Background(), "other@example.test", "Synthetic"))
	require.Equal(t, 1, readMailbox("other@example.test").DeliveryCount)
	require.Equal(t, 1, readMailbox(recipient).DeliveryCount)
	// A repeat request remains subject to the original email cooldown.
	require.ErrorIs(t, emailSvc.SendVerifyCode(context.Background(), recipient, "Synthetic"), service.ErrVerifyCodeTooFrequent)
	wrong := "000000"
	if delivery.Code == wrong {
		wrong = "000001"
	}
	wrongBody := strings.TrimSuffix(registerBody, "}") + `,"verify_code":"` + wrong + `"}`
	invalid := request(http.MethodPost, "/api/v1/auth/register", wrongBody, nil)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "INVALID_VERIFY_CODE")
	require.NoError(t, emailSvc.VerifyCode(context.Background(), recipient, delivery.Code))
	require.ErrorIs(t, emailSvc.VerifyCode(context.Background(), recipient, delivery.Code), service.ErrInvalidVerifyCode)
	// Captured delivery survives consumption; mailbox never fabricates a live code.
	require.Equal(t, 1, readMailbox(recipient).DeliveryCount)
	// Once consumed, a fresh original service delivery increments only this inbox.
	require.NoError(t, emailSvc.SendVerifyCode(context.Background(), recipient, "Synthetic"))
	require.Equal(t, 2, readMailbox(recipient).DeliveryCount)
	require.Equal(t, 1, readMailbox("other@example.test").DeliveryCount)
	stored, err = cache.GetVerificationCode(context.Background(), recipient)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.True(t, stored.Code == readMailbox(recipient).Code, "mailbox retains the latest delivered code")
	// Non-synthetic recipients are rejected by the sink before SMTP DATA.
	require.Error(t, emailSvc.SendVerifyCode(context.Background(), "someone@example.com", "Synthetic"))
}
