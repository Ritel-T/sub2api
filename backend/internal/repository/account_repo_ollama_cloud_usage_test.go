package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newOllamaCloudUsageRepositoryTestClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func ollamaCloudUsageRepositoryAccount() *service.Account {
	return &service.Account{
		ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "key", "base_url": "https://ollama.com"},
		Extra: map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "cipher:wos-session=secret",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
		},
	}
}

func TestUpdateOllamaCloudUsageSnapshotRowsAffectedZeroIsIdentityConflict(t *testing.T) {
	client, mock := newOllamaCloudUsageRepositoryTestClient(t)
	mock.ExpectBegin()
	expectOllamaCloudUsageGroupLock(mock, ollamaCloudUsageRepositoryAccount(), true,
		`"cipher:wos-session=secret"`, `true`, `null`)
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")).
		WithArgs(sqlmock.AnyArg(), "key", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	repo := newAccountRepositoryWithSQL(client, nil, nil)

	err := repo.UpdateOllamaCloudUsageSnapshot(context.Background(), ollamaCloudUsageRepositoryAccount(), &service.OllamaCloudUsageSnapshot{
		Status:        service.OllamaCloudUsageStatusOK,
		LastAttemptAt: time.Now(),
		NextRefreshAt: time.Now().Add(time.Hour),
	})

	require.ErrorIs(t, err, service.ErrOllamaCloudUsageIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectOllamaCloudUsageGroupLock(
	mock sqlmock.Sqlmock,
	account *service.Account,
	anchorMatches bool,
	sessionJSON, autoJSON, snapshotJSON string,
) {
	apiKey, _ := account.Credentials["api_key"].(string)
	credentials, _ := json.Marshal(normalizeJSONMap(account.Credentials))
	var proxyID any
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT")+`.*`+regexp.QuoteMeta("FOR NO KEY UPDATE")).
		WithArgs(apiKey, account.ID, account.Platform, account.Type, string(credentials), proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "anchor_matches", "session", "auto_refresh", "snapshot"}).
			AddRow(account.ID, anchorMatches, sessionJSON, autoJSON, snapshotJSON))
}

func TestOllamaCloudUsageManagedWriteRejectsChangedProxyIdentity(t *testing.T) {
	client, mock := newOllamaCloudUsageRepositoryTestClient(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)` + regexp.QuoteMeta("SELECT protocol, host, port") + `.*` + regexp.QuoteMeta("FOR SHARE")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
			AddRow("http", "new.example", 3128, "user", "pass", service.StatusActive))
	mock.ExpectRollback()

	account := ollamaCloudUsageRepositoryAccount()
	proxyID := int64(9)
	account.ProxyID = &proxyID
	account.Proxy = &service.Proxy{
		ID: proxyID, Protocol: "http", Host: "old.example", Port: 3128,
		Username: "user", Password: "pass", Status: service.StatusActive,
	}
	repo := newAccountRepositoryWithSQL(client, nil, nil)

	err := repo.SaveOllamaCloudUsageSession(context.Background(), account, "cipher:wos-session=replacement", true)

	require.ErrorIs(t, err, service.ErrOllamaCloudUsageIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSaveAndDeleteOllamaCloudUsageSessionKeepCiphertextOutOfSQL(t *testing.T) {
	var capturedSQL []string
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		capturedSQL = append(capturedSQL, actualSQL)
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	account := ollamaCloudUsageRepositoryAccount()
	const replacement = "cipher:wos-session=browser-cookie-secret"

	mock.ExpectBegin()
	expectOllamaCloudUsageGroupLock(mock, account, true, `"cipher:wos-session=secret"`, `true`, `null`)
	mock.ExpectExec(`(?s)UPDATE accounts.*ollama_cloud_usage_session.*ollama_cloud_usage_auto_refresh.*ollama_cloud_usage_snapshot`).
		WithArgs(`{"ollama_cloud_usage_auto_refresh":true,"ollama_cloud_usage_session":"cipher:wos-session=browser-cookie-secret"}`, "key", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.SaveOllamaCloudUsageSession(context.Background(), account, replacement, true))

	account.Extra[service.OllamaCloudUsageSessionExtraKey] = replacement
	mock.ExpectBegin()
	expectOllamaCloudUsageGroupLock(mock, account, true, `"cipher:wos-session=browser-cookie-secret"`, `true`, `null`)
	mock.ExpectExec(`(?s)UPDATE accounts.*ollama_cloud_usage_session.*ollama_cloud_usage_auto_refresh.*ollama_cloud_usage_snapshot`).
		WithArgs(`{}`, "key", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.DeleteOllamaCloudUsageSession(context.Background(), account))

	require.NotEmpty(t, capturedSQL)
	for _, query := range capturedSQL {
		require.NotContains(t, query, "browser-cookie-secret")
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOllamaCloudBaseURLSQLRegexMatchesServiceSemantics(t *testing.T) {
	for _, baseURL := range []string{
		"https://ollama.com",
		"HTTPS://WWW.OLLAMA.COM:443/v1",
		"https://ollama.com/V1",
		"https://ollama.com/v1/",
		"https://ollama.com.evil.test/v1",
	} {
		t.Run(baseURL, func(t *testing.T) {
			matched, err := regexp.MatchString(ollamaCloudBaseURLRegexSQL, baseURL)
			require.NoError(t, err)
			account := ollamaCloudUsageRepositoryAccount()
			account.Credentials["base_url"] = baseURL
			require.Equal(t, service.IsOllamaCloudUsageAccount(account), matched)
		})
	}
}

func TestUpdateCredentialsIdentityChangeClearsAllOllamaManagedExtra(t *testing.T) {
	client, mock := newOllamaCloudUsageRepositoryTestClient(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*credentials -> 'api_key' IS DISTINCT FROM.*ollama_cloud_usage_session.*ollama_cloud_usage_auto_refresh.*ollama_cloud_usage_snapshot`).
		WithArgs(`{"api_key":"new-key","base_url":"https://ollama.com"}`, int64(17)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(17), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	repo := newAccountRepositoryWithSQL(client, nil, nil)

	err := repo.UpdateCredentials(context.Background(), 17, map[string]any{
		"api_key": "new-key", "base_url": "https://ollama.com",
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDisableOllamaCloudUsageAutoRefreshUsesGroupIdentityCAS(t *testing.T) {
	client, mock := newOllamaCloudUsageRepositoryTestClient(t)
	account := ollamaCloudUsageRepositoryAccount()
	mock.ExpectBegin()
	expectOllamaCloudUsageGroupLock(mock, account, true, `"cipher:wos-session=secret"`, `true`, `null`)
	mock.ExpectExec(`(?s)UPDATE accounts.*ollama_cloud_usage_auto_refresh`).
		WithArgs(`{"ollama_cloud_usage_auto_refresh":false,"ollama_cloud_usage_session":"cipher:wos-session=secret"}`, "key", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	repo := newAccountRepositoryWithSQL(client, nil, nil)

	err := repo.DisableOllamaCloudUsageAutoRefresh(context.Background(), account)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Ollama 清理分支必须带顶层 credentials DISTINCT 守卫：没有它，非 Ollama 的
// openai/anthropic apikey 账号在凭证未变化的持久化上也会误清探测快照。
func TestUpdateCredentialsCleanupBranchRequiresChangedCredentials(t *testing.T) {
	client, mock := newOllamaCloudUsageRepositoryTestClient(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*CASE.*AND credentials IS DISTINCT FROM \$1::jsonb\s+AND \(\s+credentials -> 'api_key' IS DISTINCT FROM`).
		WithArgs(`{"api_key":"same-key","base_url":"https://relay.example.com/v1"}`, int64(17)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(17), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	repo := newAccountRepositoryWithSQL(client, nil, nil)

	err := repo.UpdateCredentials(context.Background(), 17, map[string]any{
		"api_key": "same-key", "base_url": "https://relay.example.com/v1",
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SQL 平台白名单常量与 service 判定必须互为镜像：对每个已知平台，常量里的
// 成员关系都要与 IsOllamaCloudUsageAccount（apikey + 官方 ollama.com）一致，
// 防止两侧平台列表各自漂移。
func TestOllamaCloudUsagePlatformWhitelistMatchesServicePredicate(t *testing.T) {
	matches := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(ollamaCloudUsagePlatformsSQL, -1)
	sqlPlatforms := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		sqlPlatforms[match[1]] = struct{}{}
	}
	require.Len(t, sqlPlatforms, 6)
	for _, platform := range []string{
		service.PlatformOpenAI, service.PlatformAnthropic,
		service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek, service.PlatformMiniMax,
		service.PlatformGemini, service.PlatformGrok, service.PlatformAntigravity,
		service.PlatformComposite, "kiro",
	} {
		account := ollamaCloudUsageRepositoryAccount()
		account.Platform = platform
		_, inSQL := sqlPlatforms[platform]
		require.Equal(t, inSQL, service.IsOllamaCloudUsageAccount(account), platform)
	}
}
