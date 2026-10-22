package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModerationRecoveryStaleTaskCannotReban(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &contentModerationRepository{db: db}
	m.ExpectBegin()
	m.ExpectQuery("SELECT status, role, moderation_status_revision FROM users.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"status", "role", "revision"}).AddRow("active", "user", 9))
	m.ExpectExec("INSERT INTO content_moderation_user_state").WithArgs(int64(306)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT version, generation, auto_ban_exempt, owned_status_revision FROM content_moderation_user_state.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"version", "generation", "exempt", "owned"}).AddRow(4, 2, false, nil))
	m.ExpectRollback()
	result, err := r.ApplyModerationHit(context.Background(), service.ModerationHit{UserID: 306, Version: 3, RequestID: "old-task", Since: time.Now(), AutoBan: true, Threshold: 1})
	require.NoError(t, err)
	require.False(t, result.Accepted)
	require.False(t, result.BanApplied)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestModerationRecoveryUnbanDoesNotClaimOtherDisable(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &contentModerationRepository{db: db}
	m.ExpectBegin()
	m.ExpectQuery("SELECT status, role, moderation_status_revision FROM users.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"status", "role", "revision"}).AddRow("disabled", "user", 12))
	m.ExpectExec("INSERT INTO content_moderation_user_state").WithArgs(int64(306)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT version, generation, auto_ban_exempt, owned_status_revision FROM content_moderation_user_state.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"version", "generation", "exempt", "owned"}).AddRow(4, 2, false, 11))
	m.ExpectRollback()
	err = r.ManageModerationUser(context.Background(), 306, service.ModerationUserAction{Action: "unban", Version: 4, ActorID: 1, Reason: "reviewed"})
	require.ErrorContains(t, err, "MODERATION_DISABLE_NOT_OWNED")
	require.NoError(t, m.ExpectationsWereMet())
}

func expectModerationLock(m sqlmock.Sqlmock, status, role string, revision, version, generation int64, exempt bool, owned any) {
	m.ExpectBegin()
	m.ExpectQuery("SELECT status, role, moderation_status_revision FROM users.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"status", "role", "revision"}).AddRow(status, role, revision))
	m.ExpectExec("INSERT INTO content_moderation_user_state").WithArgs(int64(306)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT version, generation, auto_ban_exempt, owned_status_revision FROM content_moderation_user_state.*FOR UPDATE").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"version", "generation", "exempt", "owned"}).AddRow(version, generation, exempt, owned))
}

func TestModerationRecoveryThresholdOwnershipAndExemption(t *testing.T) {
	for _, tc := range []struct {
		name, status, role string
		exempt, wantBan    bool
	}{
		{"threshold", "active", "user", false, true},
		{"allowlisted still counts", "active", "user", true, false},
		{"administrator exempt", "active", "admin", false, false},
		{"other disable retained", "disabled", "user", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			r := &contentModerationRepository{db: db}
			expectModerationLock(m, tc.status, tc.role, 10, 2, 1, tc.exempt, nil)
			m.ExpectExec("INSERT INTO content_moderation_user_hits").WithArgs(int64(306), int64(1), "request-1", "keyword_block").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectQuery("SELECT COUNT.*FROM content_moderation_user_hits").WithArgs(int64(306), int64(1), sqlmock.AnyArg(), false).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
			if tc.wantBan {
				m.ExpectQuery("UPDATE users SET status='disabled'.*RETURNING moderation_status_revision").WithArgs(int64(306)).WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(11))
				m.ExpectExec("UPDATE content_moderation_user_state SET owned_status_revision").WithArgs(int64(306), int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			m.ExpectCommit()
			result, err := r.ApplyModerationHit(context.Background(), service.ModerationHit{UserID: 306, Version: 2, RequestID: "request-1", Action: "keyword_block", Since: time.Now(), AutoBan: true, Threshold: 10})
			require.NoError(t, err)
			require.True(t, result.Accepted)
			require.Equal(t, 10, result.Count)
			require.Equal(t, tc.wantBan, result.BanApplied)
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}

func TestModerationRecoveryDuplicateHitDoesNotNotifyOrBan(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &contentModerationRepository{db: db}
	expectModerationLock(m, "active", "user", 10, 2, 1, false, nil)
	m.ExpectExec("INSERT INTO content_moderation_user_hits").WithArgs(int64(306), int64(1), "duplicate", "keyword_block").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectRollback()
	result, err := r.ApplyModerationHit(context.Background(), service.ModerationHit{UserID: 306, Version: 2, RequestID: "duplicate", Action: "keyword_block"})
	require.NoError(t, err)
	require.False(t, result.Accepted)
	require.False(t, result.BanApplied)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestModerationRecoveryActionsRemainIndependent(t *testing.T) {
	for _, action := range []string{"unban", "reset", "exemption"} {
		t.Run(action, func(t *testing.T) {
			db, m, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			r := &contentModerationRepository{db: db}
			expectModerationLock(m, "disabled", "user", 11, 2, 1, false, 11)
			generation := int64(1)
			var owned any = int64(11)
			exempt := false
			in := service.ModerationUserAction{Action: action, Version: 2, ActorID: 7, Reason: "reviewed", Confirm: true}
			switch action {
			case "unban":
				m.ExpectQuery("UPDATE users SET status='active'.*RETURNING status").WithArgs(int64(306), int64(11)).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
				owned = nil
			case "reset":
				generation = 2
			case "exemption":
				exempt = true
				in.Exempt = &exempt
			}
			m.ExpectExec("UPDATE content_moderation_user_state SET version=version\\+1").WithArgs(int64(306), generation, exempt, owned, "reviewed", int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("INSERT INTO content_moderation_user_actions").WithArgs(int64(306), int64(7), action, "reviewed", int64(3), exempt).WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectCommit()
			require.NoError(t, r.ManageModerationUser(context.Background(), 306, in))
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}

func TestModerationRecoveryConcurrentAdminConflict(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &contentModerationRepository{db: db}
	expectModerationLock(m, "disabled", "user", 11, 3, 1, false, 11)
	m.ExpectRollback()
	err = r.ManageModerationUser(context.Background(), 306, service.ModerationUserAction{Action: "reset", Version: 2, ActorID: 7, Reason: "reviewed", Confirm: true})
	require.Error(t, err)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestModerationRecoveryTransactionFailureCannotSucceed(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &contentModerationRepository{db: db}
	expectModerationLock(m, "active", "user", 10, 2, 1, false, nil)
	m.ExpectExec("INSERT INTO content_moderation_user_hits").WillReturnError(context.DeadlineExceeded)
	m.ExpectRollback()
	result, err := r.ApplyModerationHit(context.Background(), service.ModerationHit{UserID: 306, Version: 2, RequestID: "error"})
	require.Error(t, err)
	require.False(t, result.Accepted)
	require.False(t, result.BanApplied)
	require.NoError(t, m.ExpectationsWereMet())
}
