//go:build unit

package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Synthetic-only server around real PostgreSQL repositories, Redis and original
// auth handlers. Never included in a release binary; optional loopback-only mail,
// no model/payment worker.
func TestAccountCenterNativeServe(t *testing.T) {
	root := os.Getenv("RITEL_NATIVE_ACCOUNT_ROOT")
	if root == "" {
		t.Skip("owned native laboratory not requested")
	}
	require.True(t, strings.HasPrefix(root, "/home/ritel/.local/state/ritel-account-subcore-"))
	require.Equal(t, filepath.Clean(root), root)
	st, err := os.Lstat(root)
	require.NoError(t, err)
	require.True(t, st.IsDir())
	require.Zero(t, st.Mode()&os.ModeSymlink)
	var input struct {
		SyntheticOnly bool   `json:"syntheticOnly"`
		DSN           string `json:"dsn"`
		Redis         string `json:"redis"`
		RedisPassword string `json:"redisPassword"`
		BridgeSecret  string `json:"bridgeSecret"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "input.private.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &input))
	require.True(t, input.SyntheticOnly)
	require.True(t, strings.Contains(input.DSN, "host=127.0.0.1 ") && strings.Contains(input.DSN, "dbname=ritel_account_lab "))
	require.True(t, strings.HasPrefix(input.Redis, "127.0.0.1:"))
	ctx := context.Background()
	db, err := sql.Open("postgres", input.DSN)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.PingContext(ctx))
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	ent := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer ent.Close()
	rdb := redis.NewClient(&redis.Options{Addr: input.Redis, Password: input.RedisPassword})
	defer rdb.Close()
	require.NoError(t, rdb.Ping(ctx).Err())
	random := func() string {
		b := make([]byte, 32)
		_, err := rand.Read(b)
		require.NoError(t, err)
		return hex.EncodeToString(b)
	}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: random(), ExpireHour: 1, RefreshTokenExpireDays: 31}, AccountCenter: config.AccountCenterConfig{Enabled: true, BridgeSecret: input.BridgeSecret}}
	cfg.Totp.EncryptionKey = random()
	cfg.Totp.EncryptionKeyConfigured = true
	settingsRepo := repository.NewSettingRepository(ent)
	emailVerificationEnabled := os.Getenv("RITEL_NATIVE_EMAIL_VERIFY") == "1"
	settingValues := map[string]string{service.SettingKeyRegistrationEnabled: "true", service.SettingKeyEmailVerifyEnabled: strconv.FormatBool(emailVerificationEnabled), service.SettingKeyTotpEnabled: "true", service.SettingKeyInvitationCodeEnabled: "true", service.SettingKeyDefaultBalance: "2", service.SettingKeyDefaultConcurrency: "5"}
	var mailbox *accountCenterNativeMailbox
	if emailVerificationEnabled {
		require.NotEmpty(t, input.BridgeSecret)
		mailbox = startAccountCenterNativeSMTP(t)
		for key, value := range mailbox.smtpSettings() {
			settingValues[key] = value
		}
	}
	require.NoError(t, settingsRepo.SetMultiple(ctx, settingValues))
	settings := service.NewSettingService(settingsRepo, cfg)
	users := repository.NewUserRepository(ent, db)
	userSvc := service.NewUserService(users, settingsRepo, nil, nil)
	redeem := repository.NewRedeemCodeRepository(ent)
	var emailSvc *service.EmailService
	var emailQueue *service.EmailQueueService
	if emailVerificationEnabled {
		emailSvc = service.NewEmailService(settingsRepo, repository.NewEmailCache(rdb))
		emailQueue = service.NewEmailQueueService(emailSvc, 1)
		defer emailQueue.Stop()
	}
	auth := service.NewAuthService(ent, users, redeem, repository.NewRefreshTokenCache(rdb), cfg, settings, emailSvc, nil, emailQueue, nil, nil, nil, nil)
	encryptor, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	totpSvc := service.NewTotpService(users, encryptor, repository.NewTotpCache(rdb), settings, nil, nil)
	password := random()
	secretBytes := make([]byte, 20)
	_, err = rand.Read(secretBytes)
	require.NoError(t, err)
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secretBytes)
	encrypted, err := encryptor.Encrypt(secret)
	require.NoError(t, err)
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	enabledAt := time.Now().Add(-time.Hour)
	user := &service.User{Email: "existing@example.test", Username: "Existing synthetic user", PasswordHash: hash, Role: service.RoleUser, Status: service.StatusActive, Balance: 17.25, Concurrency: 5, TotpEnabled: true, TotpSecretEncrypted: &encrypted, TotpEnabledAt: &enabledAt}
	require.NoError(t, users.Create(ctx, user))
	require.NoError(t, users.UpdateTotpSecret(ctx, user.ID, &encrypted))
	require.NoError(t, users.EnableTotp(ctx, user.ID))
	keys := repository.NewAPIKeyRepository(ent, db)
	existingKey := &service.APIKey{UserID: user.ID, Key: "sk-" + random(), Name: "existing synthetic API key", Status: service.StatusActive}
	require.NoError(t, keys.Create(ctx, existingKey))
	invitation, err := service.GenerateRedeemCode()
	require.NoError(t, err)
	require.NoError(t, redeem.Create(ctx, &service.RedeemCode{Code: invitation, Type: service.RedeemTypeInvitation, Status: service.StatusUnused}))
	h := NewAuthHandler(cfg, auth, userSvc, settings, nil, nil, totpSvc, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/v1/settings/public", NewSettingHandler(settings, "synthetic-only").GetPublicSettings)
	r.POST("/api/v1/auth/refresh", accountCenterNativeShortAccess(h.RefreshToken, cfg))
	r.POST("/api/v1/auth/login", accountCenterNativeShortAccess(h.Login, cfg))
	r.POST("/api/v1/auth/login/2fa", accountCenterNativeShortAccess(h.Login2FA, cfg))
	r.POST("/api/v1/auth/register", accountCenterNativeShortAccess(h.Register, cfg))
	r.POST("/api/v1/auth/send-verify-code", h.SendVerifyCode)
	if emailVerificationEnabled {
		r.GET("/_fixture/mailbox", mailbox.handler(input.BridgeSecret))
	}
	r.GET("/api/v1/auth/me", gin.HandlerFunc(middleware2.NewJWTAuthMiddleware(auth, userSvc, settings, nil)), h.GetCurrentUser)
	r.GET("/api/v1/internal/account-center/identities/:subject", h.AccountCenterIdentity)
	management := NewUserHandler(userSvc, auth, emailSvc, repository.NewEmailCache(rdb), nil, nil)
	authenticated := r.Group("/api/v1/user", gin.HandlerFunc(middleware2.NewJWTAuthMiddleware(auth, userSvc, settings, nil)))
	authenticated.GET("/profile", management.GetProfile)
	authenticated.PUT("", management.UpdateProfile)
	authenticated.PUT("/password", management.ChangePassword)
	authenticated.POST("/account-bindings/email/send-code", management.SendEmailBindingCode)
	authenticated.POST("/account-bindings/email", management.BindEmailIdentity)

	r.POST("/_fixture/change-password", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer "+input.BridgeSecret {
			c.AbortWithStatus(401)
			return
		}
		var body struct {
			ID      int64  `json:"id"`
			Current string `json:"current"`
			Next    string `json:"next"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.AbortWithStatus(400)
			return
		}
		if err := userSvc.ChangePassword(c.Request.Context(), body.ID, service.ChangePasswordRequest{CurrentPassword: body.Current, NewPassword: body.Next}); err != nil {
			c.JSON(400, gin.H{"ok": false})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	ready := map[string]any{"syntheticOnly": true, "emailVerificationEnabled": emailVerificationEnabled, "baseUrl": srv.URL, "bridgeSecret": input.BridgeSecret, "existing": map[string]any{"userId": user.ID, "subject": "sub2api:" + strconv.FormatInt(user.ID, 10), "email": user.Email, "password": password, "totpSecret": secret, "name": user.Username}, "invitationCode": invitation}
	raw, err = json.Marshal(ready)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "ready.private.json"), raw, 0600))
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("native lab timed out")
		case <-poll.C:
			if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
				saved, err := users.GetByID(ctx, user.ID)
				require.NoError(t, err)
				require.Equal(t, user.ID, saved.ID)
				require.Equal(t, user.Email, saved.Email)
				require.Equal(t, 17.25, saved.Balance)
				require.True(t, saved.TotpEnabled)
				key, err := keys.GetByID(ctx, existingKey.ID)
				require.NoError(t, err)
				require.Equal(t, existingKey.Key, key.Key)
				require.Equal(t, user.ID, key.UserID)
				count, err := ent.User.Query().Count(ctx)
				require.NoError(t, err)
				require.Equal(t, 2, count)
				all, err := ent.User.Query().All(ctx)
				require.NoError(t, err)
				var newID int64
				for _, u := range all {
					if u.Email == "new@example.test" || (os.Getenv("RITEL_NATIVE_EMAIL_CHANGE") == "1" && u.Email == "changed-new@example.test") {
						newID = u.ID
						require.Equal(t, 2.0, u.Balance)
						require.Equal(t, 5, u.Concurrency)
					}
				}
				require.NotZero(t, newID)
				if os.Getenv("RITEL_NATIVE_EMAIL_CHANGE") == "1" {
					updated, err := users.GetByID(ctx, newID)
					require.NoError(t, err)
					require.Equal(t, "changed-new@example.test", updated.Email)
				}
				usedInvite, err := redeem.GetByCode(ctx, invitation)
				require.NoError(t, err)
				require.Equal(t, service.StatusUsed, usedInvite.Status)
				require.NotNil(t, usedInvite.UsedBy)
				require.Equal(t, newID, *usedInvite.UsedBy)

				var migrations int
				require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrations))
				verification, _ := json.Marshal(map[string]any{"syntheticOnly": true, "productionChanged": false, "userCount": count, "migrations": migrations, "originalUIDPreserved": true, "originalEmailPreserved": true, "balancePreserved": true, "apiKeyPreserved": true, "totpPreserved": true, "signupDefaultsGrantedOnce": true, "invitationClaimedByNewUID": true})
				require.NoError(t, os.WriteFile(filepath.Join(root, "verified.json"), verification, 0600))
				return
			}
		}
	}
}
