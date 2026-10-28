package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/lib/pq"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// Literal expectations also prohibit a synthetic token_version column or writes
// to other user/credential fields. Ordered mocks require row lock before epoch.
const factorTestUserSQL = "SELECT email, password_hash, status, totp_enabled, totp_secret_encrypted, deleted_at FROM users WHERE id = $1 FOR UPDATE"
const factorTestEpochSQL = "SELECT value FROM settings WHERE key = $1"
const factorTestTotpSQL = "UPDATE users SET totp_secret_encrypted = $2, totp_enabled = TRUE, totp_enabled_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL"
const factorTestPasskeySQL = "INSERT INTO passkey_credentials (user_id, credential_id, name, credential_data) VALUES ($1, $2, $3, $4::jsonb) RETURNING id, name, created_at, updated_at"

func newFactorSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	return db, mock
}

func factorTestUser() *service.User {
	return &service.User{ID: 4294967338, Email: " User@Example.COM ", PasswordHash: "original-password-hash", Status: service.StatusActive}
}

func factorTestProof(user *service.User, epoch string) service.NativeAuthenticationProof {
	return service.NativeAuthenticationProof{Methods: []string{"pwd"}, AuthTime: 123, Generation: service.AccountFactorGeneration(user, epoch), Epoch: epoch}
}

func factorTestUserRows(user *service.User) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"email", "password_hash", "status", "totp_enabled", "totp_secret_encrypted", "deleted_at"}).
		AddRow(user.Email, user.PasswordHash, user.Status, user.TotpEnabled, user.TotpSecretEncrypted, user.DeletedAt)
}

func expectFactorSnapshot(mock sqlmock.Sqlmock, user *service.User, epoch string) {
	mock.ExpectQuery(factorTestUserSQL).WithArgs(user.ID).WillReturnRows(factorTestUserRows(user))
	rows := sqlmock.NewRows([]string{"value"})
	if epoch != "" {
		rows.AddRow(epoch)
	}
	mock.ExpectQuery(factorTestEpochSQL).WithArgs(fmt.Sprintf("auth_session_epoch:%d", user.ID)).WillReturnRows(rows)
}

func factorTestPasskey(userID int64) *service.PasskeyCredentialRecord {
	return &service.PasskeyCredentialRecord{
		UserID: userID, UserHandle: []byte("stable-user-handle"), Name: "  Security key  ",
		Credential: webauthn.Credential{ID: []byte("credential-id"), PublicKey: []byte("public-key")},
	}
}

func expectFactorPasskeyInsert(mock sqlmock.Sqlmock, record *service.PasskeyCredentialRecord) *sqlmock.ExpectedQuery {
	encoded, _ := json.Marshal(record.Credential)
	name := strings.TrimSpace(record.Name)
	if name == "" {
		name = "Passkey"
	}
	return mock.ExpectQuery(factorTestPasskeySQL).WithArgs(record.UserID, record.Credential.ID, name, string(encoded))
}

// Exercise both public repository entrypoints against the same locked snapshot.
func writeFactor(t *testing.T, db *sql.DB, kind string, userID int64, proof service.NativeAuthenticationProof) error {
	t.Helper()
	ctx := service.WithNativeAuthenticationProof(context.Background(), proof)
	if kind == "passkey" {
		_, err := NewPasskeyRepository(db).Create(ctx, factorTestPasskey(userID))
		return err
	}
	enabler, ok := NewUserRepository(nil, db).(service.AccountTotpAtomicEnabler)
	require.True(t, ok)
	return enabler.EnableTotpWithSecret(ctx, userID, "new-encrypted-secret", proof)
}

func TestAccountFactorStaleGenerationAfterResetPreventsWrites(t *testing.T) {
	for _, kind := range []string{"totp", "passkey"} {
		for _, resetFactors := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/factors=%t", kind, resetFactors), func(t *testing.T) {
				db, mock := newFactorSQLMock(t)
				user := factorTestUser()
				if resetFactors {
					secret := "old-encrypted-secret"
					user.TotpEnabled, user.TotpSecretEncrypted = true, &secret
				}
				proof := factorTestProof(user, strings.Repeat("a1", 16))
				// The admin reset has committed; neither factor write is expected.
				user.TotpEnabled, user.TotpSecretEncrypted = false, nil
				mock.ExpectBegin()
				expectFactorSnapshot(mock, user, strings.Repeat("b2", 16))
				mock.ExpectRollback()
				require.ErrorIs(t, writeFactor(t, db, kind, user.ID, proof), service.ErrTokenRevoked)
			})
		}
	}
}

func TestAccountFactorMatchingProofAtomicWrites(t *testing.T) {
	for _, kind := range []string{"totp", "passkey"} {
		for _, epoch := range []string{"", strings.Repeat("a1", 16)} {
			t.Run(kind+"/epoch="+epoch, func(t *testing.T) {
				db, mock := newFactorSQLMock(t)
				user := factorTestUser()
				mock.ExpectBegin()
				expectFactorSnapshot(mock, user, epoch)
				if kind == "totp" {
					mock.ExpectExec(factorTestTotpSQL).WithArgs(user.ID, "new-encrypted-secret").WillReturnResult(sqlmock.NewResult(0, 1))
				} else {
					now := time.Now().UTC()
					expectFactorPasskeyInsert(mock, factorTestPasskey(user.ID)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).AddRow(10, "Security key", now, now))
				}
				mock.ExpectCommit()
				require.NoError(t, writeFactor(t, db, kind, user.ID, factorTestProof(user, epoch)))
			})
		}
	}
}

func TestAccountFactorSnapshotChangesRejectProof(t *testing.T) {
	for _, kind := range []string{"totp", "passkey"} {
		for _, change := range []string{"email", "password", "enabled", "secret", "inactive", "deleted"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				db, mock := newFactorSQLMock(t)
				user := factorTestUser()
				epoch := strings.Repeat("a1", 16)
				proof := factorTestProof(user, epoch)
				want := service.ErrTokenRevoked
				switch change {
				case "email":
					user.Email = "changed@example.com"
				case "password":
					user.PasswordHash = "new-password-hash"
				case "enabled":
					user.TotpEnabled = true
				case "secret":
					secret := "changed-encrypted-secret"
					user.TotpSecretEncrypted = &secret
				case "inactive":
					user.Status, want = "disabled", service.ErrUserNotActive
				case "deleted":
					now := time.Now()
					user.DeletedAt, want = &now, service.ErrUserNotActive
				}
				mock.ExpectBegin()
				if change == "inactive" || change == "deleted" {
					mock.ExpectQuery(factorTestUserSQL).WithArgs(user.ID).WillReturnRows(factorTestUserRows(user))
				} else {
					expectFactorSnapshot(mock, user, epoch)
				}
				mock.ExpectRollback()
				require.ErrorIs(t, writeFactor(t, db, kind, user.ID, proof), want)
			})
		}
	}
}

func TestAccountFactorStorageFailuresRollback(t *testing.T) {
	failure := errors.New("injected storage failure")
	for _, kind := range []string{"totp", "passkey"} {
		for _, stage := range []string{"begin", "snapshot", "missing-user", "epoch", "bad-epoch", "write", "commit"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				db, mock := newFactorSQLMock(t)
				user := factorTestUser()
				epoch := strings.Repeat("a1", 16)
				proof := factorTestProof(user, epoch)
				want := failure
				begin := mock.ExpectBegin()
				if stage == "begin" {
					begin.WillReturnError(failure)
				} else {
					q := mock.ExpectQuery(factorTestUserSQL).WithArgs(user.ID)
					switch stage {
					case "snapshot":
						q.WillReturnError(failure)
					case "missing-user":
						q.WillReturnError(sql.ErrNoRows)
						want = service.ErrUserNotFound
					default:
						q.WillReturnRows(factorTestUserRows(user))
						q = mock.ExpectQuery(factorTestEpochSQL).WithArgs(fmt.Sprintf("auth_session_epoch:%d", user.ID))
						switch stage {
						case "epoch":
							q.WillReturnError(failure)
						case "bad-epoch":
							q.WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("invalid"))
							want = service.ErrServiceUnavailable
						default:
							q.WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(epoch))
							if kind == "totp" {
								e := mock.ExpectExec(factorTestTotpSQL).WithArgs(user.ID, "new-encrypted-secret")
								if stage == "write" {
									e.WillReturnError(failure)
								} else {
									e.WillReturnResult(sqlmock.NewResult(0, 1))
								}
							} else {
								q = expectFactorPasskeyInsert(mock, factorTestPasskey(user.ID))
								if stage == "write" {
									q.WillReturnError(failure)
								} else {
									now := time.Now()
									q.WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).AddRow(10, "Security key", now, now))
								}
							}
						}
					}
					if stage == "commit" {
						mock.ExpectCommit().WillReturnError(failure)
					} else {
						mock.ExpectRollback()
					}
				}
				require.ErrorIs(t, writeFactor(t, db, kind, user.ID, proof), want)
			})
		}
	}
}

func TestAccountFactorTotpRowsAffectedRollback(t *testing.T) {
	for _, rows := range []int64{0, 2, -1} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) {
			db, mock := newFactorSQLMock(t)
			user := factorTestUser()
			mock.ExpectBegin()
			expectFactorSnapshot(mock, user, "")
			result := sqlmock.NewResult(0, rows)
			var want error = service.ErrServiceUnavailable
			switch rows {
			case 0:
				want = service.ErrUserNotFound
			case -1:
				want = errors.New("rows affected failed")
				result = sqlmock.NewErrorResult(want)
			}
			mock.ExpectExec(factorTestTotpSQL).WithArgs(user.ID, "new-encrypted-secret").WillReturnResult(result)
			mock.ExpectRollback()
			require.ErrorIs(t, writeFactor(t, db, "totp", user.ID, factorTestProof(user, "")), want)
		})
	}
}

func TestAccountFactorTotpRequiresDBAndProof(t *testing.T) {
	ctx := context.Background()
	proof := factorTestProof(factorTestUser(), "")
	var absent *userRepository
	require.ErrorIs(t, absent.EnableTotpWithSecret(ctx, 42, "secret", proof), service.ErrServiceUnavailable)
	require.ErrorIs(t, (&userRepository{}).EnableTotpWithSecret(ctx, 42, "secret", proof), service.ErrServiceUnavailable)
	var nilDB *sql.DB
	require.ErrorIs(t, newUserRepositoryWithSQL(nil, nilDB).EnableTotpWithSecret(ctx, 42, "secret", proof), service.ErrServiceUnavailable)
	db, mock := newFactorSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, newUserRepositoryWithSQL(nil, tx).EnableTotpWithSecret(ctx, 42, "secret", proof), service.ErrServiceUnavailable)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	mock.ExpectBegin()
	mock.ExpectRollback()
	require.ErrorIs(t, writeFactor(t, db, "totp", 42, service.NativeAuthenticationProof{}), service.ErrTokenRevoked)
}

func TestAccountFactorPasskeyLegacyAndErrorTranslations(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		for _, outcome := range []string{"success", "duplicate", "error"} {
			t.Run(fmt.Sprintf("fenced=%t/%s", fenced, outcome), func(t *testing.T) {
				db, mock := newFactorSQLMock(t)
				user := factorTestUser()
				ctx := context.Background()
				if fenced {
					ctx = service.WithNativeAuthenticationProof(ctx, factorTestProof(user, ""))
					mock.ExpectBegin()
					expectFactorSnapshot(mock, user, "")
				} else {
					// A proof without generation still follows the exact legacy path.
					ctx = service.WithNativeAuthenticationProof(ctx, service.NativeAuthenticationProof{Methods: []string{"pwd"}})
				}
				record := factorTestPasskey(user.ID)
				record.Name = " "
				q := expectFactorPasskeyInsert(mock, record)
				now := time.Now().UTC()
				var want error
				switch outcome {
				case "success":
					q.WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).AddRow(99, "Passkey", now, now))
				case "duplicate":
					q.WillReturnError(&pq.Error{Code: "23505"})
					want = service.ErrPasskeyExists
				case "error":
					want = errors.New("insert failed")
					q.WillReturnError(want)
				}
				if fenced {
					if want == nil {
						mock.ExpectCommit()
					} else {
						mock.ExpectRollback()
					}
				}
				got, err := NewPasskeyRepository(db).Create(ctx, record)
				if want != nil {
					require.ErrorIs(t, err, want)
					require.Nil(t, got)
					if outcome == "error" {
						require.EqualError(t, err, "create passkey credential: insert failed")
					}
					return
				}
				require.NoError(t, err)
				require.Equal(t, int64(99), got.ID)
				require.Equal(t, "Passkey", got.Name)
				require.Equal(t, record.Credential, got.Credential)
				require.Equal(t, record.UserHandle, got.UserHandle)
				require.Equal(t, now, got.CreatedAt)
				require.Equal(t, now, got.UpdatedAt)
				require.Equal(t, " ", record.Name)
			})
		}
	}
}

func TestAccountFactorProofGetterPreservesOriginal(t *testing.T) {
	require.Empty(t, service.AccountFactorProof(context.Background()).Generation)
	proof := factorTestProof(factorTestUser(), "")
	ctx := service.WithNativeAuthenticationProof(context.Background(), proof)
	got := service.AccountFactorProof(ctx)
	require.Equal(t, proof, got)
	got.Methods[0] = "changed"
	require.Equal(t, proof, service.AccountFactorProof(ctx))
}

// These stubs keep service dispatch tests in the assigned repository test file.
// Unimplemented methods panic, so an unexpected fallback cannot pass silently.
type factorSetupSettings struct{ service.SettingRepository }

func (*factorSetupSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyTotpEnabled {
		return "true", nil
	}
	return "", service.ErrSettingNotFound
}

type factorSetupCache struct {
	service.TotpCache
	session *service.TotpSetupSession
	deleted bool
}

func (c *factorSetupCache) GetSetupSession(context.Context, int64) (*service.TotpSetupSession, error) {
	return c.session, nil
}
func (c *factorSetupCache) DeleteSetupSession(context.Context, int64) error {
	c.deleted = true
	return nil
}

type factorSetupEncryptor struct{ service.SecretEncryptor }

func (*factorSetupEncryptor) Encrypt(string) (string, error) { return "new-encrypted-secret", nil }
func (*factorSetupEncryptor) Decrypt(string) (string, error) { return "JBSWY3DPEHPK3PXP", nil }

type factorSetupLegacyRepo struct {
	service.UserRepository
	calls  []string
	secret *string
	failAt string
}

func (r *factorSetupLegacyRepo) UpdateTotpSecret(_ context.Context, _ int64, secret *string) error {
	r.calls = append(r.calls, "secret")
	r.secret = secret
	if r.failAt == "secret" {
		return service.ErrUserNotFound
	}
	return nil
}
func (r *factorSetupLegacyRepo) EnableTotp(context.Context, int64) error {
	r.calls = append(r.calls, "enable")
	if r.failAt == "enable" {
		return service.ErrUserNotFound
	}
	return nil
}

func TestAccountFactorCompleteSetupDispatch(t *testing.T) {
	for _, mode := range []string{"legacy", "legacy-secret-error", "legacy-enable-error", "fenced", "stale", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			user := factorTestUser()
			ctx := context.Background()
			cache := &factorSetupCache{session: &service.TotpSetupSession{Secret: "JBSWY3DPEHPK3PXP", SetupToken: "setup-token"}}
			legacy := &factorSetupLegacyRepo{}
			var repo service.UserRepository = legacy
			var want error
			switch mode {
			case "legacy-secret-error":
				legacy.failAt, want = "secret", service.ErrUserNotFound
			case "legacy-enable-error":
				legacy.failAt, want = "enable", service.ErrUserNotFound
			}
			if mode == "fenced" || mode == "stale" || mode == "unsupported" {
				ctx = service.WithNativeAuthenticationProof(ctx, factorTestProof(user, ""))
				if mode == "unsupported" {
					want = service.ErrServiceUnavailable
				} else {
					db, mock := newFactorSQLMock(t)
					repo = NewUserRepository(nil, db)
					mock.ExpectBegin()
					if mode == "stale" {
						expectFactorSnapshot(mock, user, strings.Repeat("b2", 16))
						mock.ExpectRollback()
						want = service.ErrTokenRevoked
					} else {
						expectFactorSnapshot(mock, user, "")
						mock.ExpectExec(factorTestTotpSQL).WithArgs(user.ID, "new-encrypted-secret").WillReturnResult(sqlmock.NewResult(0, 1))
						mock.ExpectCommit()
					}
				}
			}
			settings := service.NewSettingService(&factorSetupSettings{}, nil)
			svc := service.NewTotpService(repo, &factorSetupEncryptor{}, cache, settings, nil, nil)
			code, err := totp.GenerateCode(cache.session.Secret, time.Now())
			require.NoError(t, err)
			err = svc.CompleteSetup(ctx, user.ID, code, cache.session.SetupToken)
			if want != nil {
				require.ErrorIs(t, err, want)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, want == nil, cache.deleted)
			if strings.HasPrefix(mode, "legacy") {
				calls := []string{"secret", "enable"}
				if mode == "legacy-secret-error" {
					calls = []string{"secret"}
				}
				require.Equal(t, calls, legacy.calls)
				require.Equal(t, "new-encrypted-secret", *legacy.secret)
			} else {
				require.Empty(t, legacy.calls)
			}
		})
	}
}
