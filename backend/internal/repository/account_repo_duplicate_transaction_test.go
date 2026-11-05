//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCreateWithAccountGroupsHonorsOuterTransactionRollback(t *testing.T) {
	db := scopedAdminKeyTestDB(t)
	_, err := db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY,deleted_at TIMESTAMPTZ);
 INSERT INTO groups(id) VALUES(1);
 CREATE TABLE accounts(
 id BIGSERIAL PRIMARY KEY,created_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ,
 name TEXT,notes TEXT,platform TEXT,type TEXT,credentials JSONB,extra JSONB,
 concurrency INT,priority INT,rate_multiplier DOUBLE PRECISION,group_rate_multiplier DOUBLE PRECISION,
 status TEXT,error_message TEXT,schedulable BOOLEAN,auto_pause_on_expired BOOLEAN,quota_dimension TEXT);
 CREATE TABLE account_groups(account_id BIGINT REFERENCES accounts(id),group_id BIGINT REFERENCES groups(id),priority INT,allowed_models JSONB,created_at TIMESTAMPTZ);
 CREATE TABLE scheduled_test_plans(id BIGSERIAL PRIMARY KEY,account_id BIGINT REFERENCES accounts(id),model_id TEXT,cron_expression TEXT,enabled BOOLEAN,max_results INT,auto_recover BOOLEAN,next_run_at TIMESTAMPTZ,pelican_config JSONB,created_at TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT);
 CREATE UNIQUE INDEX outer_create_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL;`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newAccountRepositoryWithSQL(client, db, nil)
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(context.Background(), tx)
	policy := service.DefaultOpenAITwoFAAdmissionConfig()
	policy.Enabled, policy.IsolationGroupID = true, 1
	now := time.Now().UTC()
	a := &service.Account{Name: "outer-transaction-create", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Concurrency: 1, Schedulable: false,
		Extra:              map[string]any{service.OpenAITwoFAAdmissionExtraKey: service.OpenAITwoFAAdmissionState{Policy: policy, Phase: "pending"}},
		InitialQualityPlan: &service.ScheduledTestPlan{ModelID: "gpt-6-astra", CronExpression: "* * * * *", Enabled: true, MaxResults: 4, NextRunAt: &now},
		InitialAdmissionPlan: &service.ScheduledTestPlan{ModelID: policy.ModelID, CronExpression: "* * * * *", Enabled: true, MaxResults: 100, NextRunAt: &now,
			PelicanConfig: &service.PelicanTestConfig{QuestionKind: "candy", Prompt: service.CandyPrompt, ModelID: policy.ModelID, ParallelCount: 1,
				Quality: &service.QualityPolicy{Action: service.QualityActionOpenAITwoFAAdmission, ExpectedAnswer: "21", Admission: &policy}}}}
	require.NoError(t, repo.CreateWithAccountGroups(ctx, a, []service.AccountGroup{{GroupID: 1, Priority: 37}}))
	require.Positive(t, a.ID)
	for table, expected := range map[string]int{"accounts": 1, "account_groups": 1, "scheduled_test_plans": 2, "scheduler_outbox": 1} {
		rows, err := tx.Client().QueryContext(ctx, "SELECT COUNT(*) FROM "+table)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var inside int
		require.NoError(t, rows.Scan(&inside))
		require.NoError(t, rows.Close())
		require.Equal(t, expected, inside, table+" must exist in the caller transaction")
		var outside int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&outside))
		require.Zero(t, outside, table+" must not commit independently")
	}
	require.NoError(t, tx.Rollback())
	for _, table := range []string{"accounts", "account_groups", "scheduled_test_plans", "scheduler_outbox"} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table+" must roll back with the caller")
	}
}
