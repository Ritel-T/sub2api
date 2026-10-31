//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Validate the PostgreSQL JSON predicate and its complement against the same
// explicitly provisioned disposable local DB as the existing real-ledger suite.
func TestGelvLatencyPostgresLanePartition(t *testing.T) {
	dsn := os.Getenv("GELV_TEST_DSN")
	if dsn == "" {
		t.Skip("GELV_TEST_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, parsed.Hostname())
	require.Equal(t, "/gelv_test", parsed.Path)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	require.NoError(t, client.Schema.Create(ctx))
	token := uuid.NewString()
	user, err := client.User.Create().SetEmail(token + "@example.invalid").SetPasswordHash("isolated-test").SetUsername("latency-partition").Save(ctx)
	require.NoError(t, err)
	defer func() {
		_, _ = client.PaymentOrder.Delete().Where(paymentorder.UserIDEQ(user.ID)).Exec(ctx)
		_ = client.User.DeleteOneID(user.ID).Exec(ctx)
	}()
	wantActive, wantHistory := map[int64]bool{}, map[int64]bool{}
	for i, tc := range []struct {
		status                       string
		snapshot                     map[string]any
		active, history, bound, live bool
	}{
		{OrderStatusPending, map[string]any{}, true, false, true, false},
		{OrderStatusFailed, map[string]any{"gelv_reconciliation_blocked": "paid_evidence_mismatch"}, false, true, true, false},
		{OrderStatusPaid, map[string]any{"gelv_refund_pending": "true"}, false, true, true, false},
		{OrderStatusPaid, map[string]any{"gelv_refund_pending": true}, false, true, true, false},
		{OrderStatusPending, map[string]any{"gelv_refund_pending": "false"}, true, false, true, false},
		{OrderStatusCompleted, map[string]any{}, false, true, true, false},
		{OrderStatusExpired, map[string]any{}, false, true, true, false},
		{OrderStatusCancelled, map[string]any{}, false, true, true, false},
		{OrderStatusExpired, map[string]any{}, false, false, false, false},
		{OrderStatusRecharging, map[string]any{}, false, false, true, true},
		{OrderStatusRecharging, map[string]any{"gelv_refund_pending": "true"}, false, false, true, true},
		{OrderStatusRecharging, map[string]any{}, true, false, true, false},
	} {
		ref := fmt.Sprintf("sub2_latency_%s_%d", token, i)
		create := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(9.9).SetPayAmount(10).SetFeeRate(0).
			SetRechargeCode("LATENCY-" + ref).SetOutTradeNo(ref).SetPaymentType(payment.TypeGelv).SetProviderKey(payment.TypeGelv).SetProviderSnapshot(tc.snapshot).
			SetOrderType(payment.OrderTypeBalance).SetStatus(tc.status).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.invalid")
		if tc.bound {
			create.SetPaymentTradeNo(ref)
		}
		row, err := create.Save(ctx)
		require.NoError(t, err)
		if !tc.live {
			_, err = client.PaymentOrder.UpdateOneID(row.ID).SetUpdatedAt(time.Now().Add(-paymentFulfillmentLeaseDuration - time.Minute)).Save(ctx)
			require.NoError(t, err)
		}
		if tc.active {
			wantActive[row.ID] = true
		}
		if tc.history {
			wantHistory[row.ID] = true
		}
	}
	for _, active := range []bool{true, false} {
		rows, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(user.ID), gelvLanePredicate(active)).All(ctx)
		require.NoError(t, err)
		got := map[int64]bool{}
		for _, row := range rows {
			got[row.ID] = true
		}
		if active {
			require.Equal(t, wantActive, got)
		} else {
			require.Equal(t, wantHistory, got)
		}
	}
}
