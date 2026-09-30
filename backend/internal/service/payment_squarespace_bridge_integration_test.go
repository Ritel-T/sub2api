//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalpayment"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalrefundjournal"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// This test deliberately owns an ephemeral testcontainer. It never reads a
// database URL from the environment, and never connects to an existing database.
func TestSquarespaceBridgePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("squarespace_bridge_test"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only-password"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		require.NoError(t, pg.Terminate(cleanupCtx))
	})
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(ctx))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, client.Schema.Create(ctx))
	// Recreate the four external tables using the checked-in migration, so the
	// assertions exercise migration 262 rather than ent-generated indexes.
	_, err = db.ExecContext(ctx, "DROP TABLE payment_external_refund_journals, payment_external_payments, payment_sync_states, payment_external_orders")
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "262_squarespace_external_ledger.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	deductSQL := squarespacePGSourceSQL(t, "user_repo.go", "DeductAvailableBalance", "FOR UPDATE")
	billingSQL := squarespacePGSourceSQL(t, "usage_billing_repo.go", "deductUsageBillingBalance", "balance >= $1")
	var sequence atomic.Int64

	t.Run("migration_unique_keys_and_checks", func(t *testing.T) {
		f := squarespacePGFixture(t, ctx, client, deductSQL, sequence.Add(1), OrderStatusCompleted, 15)
		ledger := f.bind(t, ctx)
		_, err := client.PaymentExternalOrder.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").SetExternalOrderID(f.remote.ID).Save(ctx)
		require.True(t, dbent.IsConstraintError(err), "external order must be unique within its website")
		other, err := client.PaymentExternalOrder.Create().SetProviderKey("squarespace").SetWebsiteID("other_site").SetExternalOrderID(f.remote.ID).Save(ctx)
		require.NoError(t, err, "the same identifier on another website is a different payment")
		_, err = client.PaymentExternalOrder.UpdateOneID(other.ID).SetLocalOrderID(f.local.ID).Save(ctx)
		require.True(t, dbent.IsConstraintError(err), "one local order cannot have two external bindings")
		_, err = client.PaymentExternalPayment.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").SetPaymentID(f.proof.PaymentID).SetExternalOrderLedgerID(other.ID).SetCurrency("GBP").SetAmountMinor(1000).SetPaidAt(f.proof.PaidAt).Save(ctx)
		require.True(t, dbent.IsConstraintError(err), "one payment cannot be rebound to another order")
		_, err = client.PaymentExternalPayment.Create().SetProviderKey("squarespace").SetWebsiteID("other_site").SetPaymentID(f.proof.PaymentID).SetExternalOrderLedgerID(other.ID).SetCurrency("GBP").SetAmountMinor(1000).SetPaidAt(f.proof.PaidAt).Save(ctx)
		require.NoError(t, err)
		createJournal := func() error {
			return client.PaymentExternalRefundJournal.Create().SetExternalOrderLedgerID(ledger.ID).SetCumulativeRefundMinor(200).SetDeltaRefundMinor(200).SetTargetUsdUnits(400000000).SetDeltaTargetUsdUnits(400000000).Exec(ctx)
		}
		require.NoError(t, createJournal())
		require.True(t, dbent.IsConstraintError(createJournal()), "duplicate cumulative observations must be rejected")
		_, err = client.PaymentSyncState.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").Save(ctx)
		require.NoError(t, err)
		_, err = client.PaymentSyncState.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").Save(ctx)
		require.True(t, dbent.IsConstraintError(err))
		_, err = db.ExecContext(ctx, "UPDATE payment_external_orders SET total_minor = -1 WHERE id = $1", ledger.ID)
		require.Error(t, err, "migration monetary CHECK must reject negative totals")
		_, err = db.ExecContext(ctx, "DELETE FROM payment_orders WHERE id = $1", f.local.ID)
		require.Error(t, err, "migration binding FK must preserve the local order")
		var constraint *pq.Error
		require.ErrorAs(t, err, &constraint)
		require.Contains(t, []string{"23001", "23503"}, string(constraint.Code))
		require.Equal(t, "payment_external_orders_local_order_id_fkey", constraint.Constraint)
	})

	t.Run("concurrent_binding_and_fulfillment_credit_once", func(t *testing.T) {
		f := squarespacePGFixture(t, ctx, client, deductSQL, sequence.Add(1), OrderStatusPending, 0)
		ledger, err := f.bridge.observeOrder(ctx, "site_1", f.remote)
		require.NoError(t, err)
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				results <- f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, ledger)
			}()
		}
		close(start)
		for range 2 {
			err := <-results
			if err != nil {
				require.Equal(t, "CONFLICT", infraerrors.Reason(err), "only the fulfillment lease may reject a concurrent worker")
			}
		}
		ledger, err = client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, ledger), "a retry must converge without another credit")
		updatedUser, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 20.0, updatedUser.Balance)
		require.Equal(t, 20.0, updatedUser.TotalRecharged)
		local, err := client.PaymentOrder.Get(ctx, f.local.ID)
		require.NoError(t, err)
		require.Equal(t, OrderStatusCompleted, local.Status)
		used, err := client.RedeemCode.Query().Where(redeemcode.CodeEQ(f.local.RechargeCode)).Only(ctx)
		require.NoError(t, err)
		require.Equal(t, StatusUsed, used.Status)
		require.Equal(t, f.local.UserID, *used.UsedBy)
		count, err := client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		count, err = client.PaymentExternalOrder.Query().Where(paymentexternalorder.LocalOrderIDEQ(local.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("competing_external_bindings_have_one_winner", func(t *testing.T) {
		f := squarespacePGFixture(t, ctx, client, deductSQL, sequence.Add(1), OrderStatusPending, 0)
		otherRemote := *f.remote
		otherRemote.ID += "-other"
		otherDocs := append([]provider.SquarespaceTransactionDocument(nil), f.docs...)
		otherDocs[0].ID += "-other"
		otherDocs[0].SalesOrderID = otherRemote.ID
		otherDocs[0].Payments = append([]provider.SquarespacePayment(nil), f.docs[0].Payments...)
		otherDocs[0].Payments[0].ID += "-other"
		otherDocs[0].Payments[0].ExternalTransactionID += "-other"
		otherProof, code := verifySquarespaceProof(&otherRemote, otherDocs)
		require.Empty(t, code)
		ledgers := make([]*dbent.PaymentExternalOrder, 2)
		remotes := []*provider.SquarespaceOrder{f.remote, &otherRemote}
		proofs := []*verifiedSquarespaceProof{f.proof, otherProof}
		documents := [][]provider.SquarespaceTransactionDocument{f.docs, otherDocs}
		for i := range ledgers {
			var err error
			ledgers[i], err = f.bridge.observeOrder(ctx, "site_1", remotes[i])
			require.NoError(t, err)
		}
		type outcome struct {
			index int
			row   *dbent.PaymentExternalOrder
			err   error
		}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		for i := range ledgers {
			go func(index int) {
				<-start
				row, err := f.bridge.bindExternalOrder(ctx, ledgers[index], f.local, remotes[index], proofs[index], "reference")
				results <- outcome{index, row, err}
			}(i)
		}
		close(start)
		winner := -1
		for range 2 {
			result := <-results
			if result.err == nil {
				require.Equal(t, -1, winner, "only one external payment may win the local binding")
				winner = result.index
				ledgers[result.index] = result.row
			} else {
				require.True(t, dbent.IsConstraintError(result.err))
				loser, err := client.PaymentExternalOrder.Get(ctx, ledgers[result.index].ID)
				require.NoError(t, err)
				require.Nil(t, loser.LocalOrderID, "the rejected binding must roll back")
			}
		}
		require.NotEqual(t, -1, winner)
		f.bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
			return &squarespacePGSource{order: remotes[winner], documents: documents[winner]}, nil
		}
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", remotes[winner], documents[winner], ledgers[winner]))
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", remotes[winner], documents[winner], ledgers[winner]))
		updatedUser, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 20.0, updatedUser.Balance)
		count, err := client.PaymentExternalOrder.Query().Where(paymentexternalorder.LocalOrderIDEQ(f.local.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("refund_waits_for_billing_and_duplicate_recovers_once", func(t *testing.T) {
		f := squarespacePGFixture(t, ctx, client, deductSQL, sequence.Add(1), OrderStatusCompleted, 15)
		ledger := f.bind(t, ctx)
		proof := squarespacePGRefundProof(f.proof, 1000)
		billingTx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = billingTx.Rollback() }()
		var balance float64
		require.NoError(t, billingTx.QueryRowContext(ctx, billingSQL, 5, f.local.UserID).Scan(&balance))
		require.Equal(t, 10.0, balance)
		started := make(chan struct{}, 2)
		f.users.beforeDeduct = func() { started <- struct{}{} }
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() { <-start; results <- f.bridge.applyObservedRefund(ctx, ledger, proof, "REFUNDED") }()
		}
		close(start)
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("refund did not reach the available-balance deduction")
		}
		require.NoError(t, billingTx.Commit())
		for range 2 {
			require.NoError(t, <-results)
		}
		updatedUser, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Zero(t, updatedUser.Balance)
		require.Equal(t, 7.0, updatedUser.FrozenBalance)
		ledger, err = client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.Equal(t, int64(2000000000), ledger.RefundTargetUsdUnits)
		require.Equal(t, int64(1000000000), ledger.RecoveredUsdUnits)
		require.Equal(t, int64(1000000000), ledger.DebtUsdUnits)
		require.Equal(t, ledger.RefundTargetUsdUnits, ledger.RecoveredUsdUnits+ledger.DebtUsdUnits)
		require.Equal(t, int64(1), f.users.deductionCalls.Load())
		count, err := client.PaymentExternalRefundJournal.Query().Where(paymentexternalrefundjournal.ExternalOrderLedgerIDEQ(ledger.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		count, err = client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.local.ID, 10)), paymentauditlog.ActionEQ("SQUARESPACE_EXTERNAL_REFUND")).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("post_deduction_audit_error_rolls_back_everything", func(t *testing.T) {
		f := squarespacePGFixture(t, ctx, client, deductSQL, sequence.Add(1), OrderStatusCompleted, 15)
		ledger := f.bind(t, ctx)
		client.PaymentAuditLog.Use(func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				return nil, errors.New("injected refund audit failure")
			})
		})
		err := f.bridge.applyObservedRefund(ctx, ledger, squarespacePGRefundProof(f.proof, 200), "REFUNDED")
		require.ErrorContains(t, err, "injected refund audit failure")
		require.Equal(t, int64(1), f.users.deductionCalls.Load(), "the failure must occur after a real database deduction")
		updatedUser, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 15.0, updatedUser.Balance)
		require.Equal(t, 7.0, updatedUser.FrozenBalance)
		local, err := client.PaymentOrder.Get(ctx, f.local.ID)
		require.NoError(t, err)
		require.Equal(t, OrderStatusCompleted, local.Status)
		require.Zero(t, local.RefundAmount)
		require.Nil(t, local.RefundAt)
		ledger, err = client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.Zero(t, ledger.RefundedMinor)
		require.Zero(t, ledger.RefundTargetUsdUnits)
		require.Zero(t, ledger.RecoveredUsdUnits)
		require.Zero(t, ledger.DebtUsdUnits)
		paid, err := client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID)).Only(ctx)
		require.NoError(t, err)
		require.Zero(t, paid.RefundedMinor)
		count, err := client.PaymentExternalRefundJournal.Query().Where(paymentexternalrefundjournal.ExternalOrderLedgerIDEQ(ledger.ID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}

// Importing repository from a same-package service test creates an import cycle.
// Extract the checked-in production SQL literal instead of maintaining a copied
// query, then execute it through the transaction context supplied by the bridge.
func squarespacePGSourceSQL(t *testing.T, file, function, marker string) string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "repository", file), nil, 0)
	require.NoError(t, err)
	var query string
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && strings.Contains(value, "UPDATE users") && strings.Contains(value, marker) {
				query = value
			}
			return true
		})
	}
	require.NotEmpty(t, query, "production SQL shape changed; revise the integration adapter")
	return query
}

type squarespacePGSettings struct{ SettingRepository }

func (squarespacePGSettings) GetValue(context.Context, string) (string, error) { return "true", nil }

type squarespacePGCache struct{}

func (squarespacePGCache) InvalidateUserBalance(context.Context, int64) error { return nil }

type squarespacePGUserRepo struct {
	UserRepository
	client         *dbent.Client
	deductSQL      string
	beforeDeduct   func()
	deductionCalls atomic.Int64
}

func squarespacePGClient(ctx context.Context, client *dbent.Client) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return client
}
func (r *squarespacePGUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	u, err := squarespacePGClient(ctx, r.client).User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &User{ID: u.ID, Email: u.Email, Username: u.Username, Balance: u.Balance, FrozenBalance: u.FrozenBalance, Status: u.Status, Role: u.Role}, nil
}
func (r *squarespacePGUserRepo) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	u := squarespacePGClient(ctx, r.client).User.Update().Where(user.IDEQ(id)).AddBalance(amount)
	if amount > 0 {
		u.AddTotalRecharged(amount)
	}
	count, err := u.Save(ctx)
	if err == nil && count != 1 {
		return ErrUserNotFound
	}
	return err
}
func (r *squarespacePGUserRepo) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	if dbent.TxFromContext(ctx) == nil {
		return 0, errors.New("refund deduction escaped the bridge transaction")
	}
	r.deductionCalls.Add(1)
	if r.beforeDeduct != nil {
		r.beforeDeduct()
	}
	rows, err := squarespacePGClient(ctx, r.client).QueryContext(ctx, r.deductSQL, amount, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, ErrUserNotFound
	}
	var deducted float64
	if err := rows.Scan(&deducted); err != nil {
		return 0, err
	}
	return deducted, rows.Err()
}

type squarespacePGRedeemRepo struct {
	RedeemCodeRepository
	client *dbent.Client
}

func squarespacePGCode(row *dbent.RedeemCode) *RedeemCode {
	return &RedeemCode{ID: row.ID, Code: row.Code, Type: row.Type, Value: row.Value, Status: row.Status, UsedBy: row.UsedBy, UsedAt: row.UsedAt, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt}
}
func (r *squarespacePGRedeemRepo) Create(ctx context.Context, code *RedeemCode) error {
	row, err := squarespacePGClient(ctx, r.client).RedeemCode.Create().SetCode(code.Code).SetType(code.Type).SetValue(code.Value).SetStatus(code.Status).Save(ctx)
	if err == nil {
		code.ID = row.ID
		code.CreatedAt = row.CreatedAt
	}
	return err
}
func (r *squarespacePGRedeemRepo) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	row, err := squarespacePGClient(ctx, r.client).RedeemCode.Query().Where(redeemcode.CodeEQ(code)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, ErrRedeemCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return squarespacePGCode(row), nil
}
func (r *squarespacePGRedeemRepo) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	row, err := squarespacePGClient(ctx, r.client).RedeemCode.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return squarespacePGCode(row), nil
}
func (r *squarespacePGRedeemRepo) Use(ctx context.Context, id, uid int64) error {
	count, err := squarespacePGClient(ctx, r.client).RedeemCode.Update().Where(redeemcode.IDEQ(id), redeemcode.StatusEQ(StatusUnused)).SetStatus(StatusUsed).SetUsedBy(uid).SetUsedAt(time.Now()).Save(ctx)
	if err == nil && count != 1 {
		return ErrRedeemCodeUsed
	}
	return err
}

type squarespacePGFixtureData struct {
	bridge   *SquarespacePaymentBridge
	local    *dbent.PaymentOrder
	instance *dbent.PaymentProviderInstance
	remote   *provider.SquarespaceOrder
	docs     []provider.SquarespaceTransactionDocument
	proof    *verifiedSquarespaceProof
	users    *squarespacePGUserRepo
}

func squarespacePGFixture(t *testing.T, ctx context.Context, client *dbent.Client, deductSQL string, sequence int64, status string, balance float64) *squarespacePGFixtureData {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	u, err := client.User.Create().SetEmail(fmt.Sprintf("sq-pg-%d@example.test", sequence)).SetPasswordHash("test-only-hash").SetUsername("integration").SetBalance(balance).SetFrozenBalance(7).Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey("squarespace").SetConfig(`{"websiteId":"site_1","productId":"6abc2d02ac3ba7447cdc0752","referenceFieldLabel":"RynexAI top-up reference","payLinkUrl":"https://test.squarespace.com/pay","currency":"GBP"}`).SetSupportedTypes("squarespace").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	no := false
	money := func(value string) provider.SquarespaceMoney {
		return provider.SquarespaceMoney{Currency: "GBP", Value: value}
	}
	remote := &provider.SquarespaceOrder{ID: fmt.Sprintf("pg-external-%d", sequence), OrderNumber: strconv.FormatInt(sequence, 10), PaymentState: "PAID", TestMode: &no, GrandTotal: money("10.00"), RefundedTotal: money("0.00"), TopUpReference: fmt.Sprintf("sub2_%032x", sequence), ReferenceStatus: "valid", LineItems: []provider.SquarespaceLineItem{{ID: "line_1", ProductID: "6abc2d02ac3ba7447cdc0752"}}}
	docs := []provider.SquarespaceTransactionDocument{{ID: fmt.Sprintf("pg-document-%d", sequence), SalesOrderID: remote.ID, Voided: &no, Total: money("10.00"), TotalNetPayment: money("9.55"), Payments: []provider.SquarespacePayment{{ID: fmt.Sprintf("pg-payment-%d", sequence), ExternalTransactionID: fmt.Sprintf("pg-charge-%d", sequence), Provider: "SQSP_PAYMENTS", PaidOn: now.Format(time.RFC3339Nano), Amount: money("10.00"), NetAmount: money("9.55"), RefundedAmount: money("0.00"), ProcessingFees: []provider.SquarespaceProcessingFee{{Amount: money("0.45"), NetAmount: money("0.45"), RefundedAmount: money("0.00")}}}}}}
	quote := RetailQuote{ProductID: "6abc2d02ac3ba7447cdc0752", CreditedAmountUSD: 20, BaseAmountGBP: 9.5, IncludedCostGBP: .5, TotalAmountGBP: 10, PayAmount: 10, Currency: "GBP", FX: map[string]float64{"GBP": 1, "USD": 2, "CNY": 10}, FXSource: "integration", FXAsOf: now, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CheckoutReference: remote.TopUpReference}
	raw, err := json.Marshal(quote)
	require.NoError(t, err)
	var quoteMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &quoteMap))
	snapshot := map[string]any{"schema_version": 1, "provider_instance_id": strconv.FormatInt(inst.ID, 10), "provider_key": "squarespace", "merchant_id": "site_1", "product_id": "6abc2d02ac3ba7447cdc0752", "currency": "GBP", "retail_quote": quoteMap}
	local, err := client.PaymentOrder.Create().SetUserID(u.ID).SetUserEmail(u.Email).SetUserName(u.Username).SetAmount(20).SetPayAmount(10).SetRechargeCode(fmt.Sprintf("PG-RECHARGE-%d", sequence)).SetOutTradeNo(remote.TopUpReference).SetPaymentType("squarespace").SetProviderKey("squarespace").SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).SetProviderSnapshot(snapshot).SetPaymentTradeNo("").SetOrderType("balance").SetStatus(status).SetExpiresAt(quote.ExpiresAt).SetClientIP("127.0.0.1").SetSrcHost("integration.example.test").Save(ctx)
	require.NoError(t, err)
	users := &squarespacePGUserRepo{client: client, deductSQL: deductSQL}
	redeem := &squarespacePGRedeemRepo{client: client}
	if status == OrderStatusCompleted {
		_, err = client.RedeemCode.Create().SetCode(local.RechargeCode).SetType(RedeemTypeBalance).SetValue(20).SetStatus(StatusUsed).SetUsedBy(u.ID).SetUsedAt(now).Save(ctx)
		require.NoError(t, err)
	}
	cfg := NewPaymentConfigService(client, squarespacePGSettings{}, []byte("0123456789abcdef0123456789abcdef"))
	redeemSvc := NewRedeemService(redeem, users, nil, nil, nil, client, nil, nil)
	paymentSvc := &PaymentService{entClient: client, configService: cfg, userRepo: users, redeemService: redeemSvc}
	bridge := NewSquarespacePaymentBridge(paymentSvc, nil, nil)
	bridge.SetRefundCacheInvalidators(squarespacePGCache{}, nil)
	bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
		return &squarespacePGSource{order: remote, documents: docs}, nil
	}
	_, err = provider.EvaluateSquarespaceOrder(remote, docs)
	require.NoError(t, err)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	return &squarespacePGFixtureData{bridge: bridge, local: local, instance: inst, remote: remote, docs: docs, proof: proof, users: users}
}

type squarespacePGSource struct {
	order     *provider.SquarespaceOrder
	documents []provider.SquarespaceTransactionDocument
}

func (*squarespacePGSource) VerifyWebsite(context.Context) error { return nil }
func (s *squarespacePGSource) ListOrders(context.Context, string, string, string) (*provider.SquarespaceOrderPage, error) {
	return &provider.SquarespaceOrderPage{Orders: []provider.SquarespaceOrder{*s.order}}, nil
}
func (s *squarespacePGSource) GetOrder(context.Context, string) (*provider.SquarespaceOrder, error) {
	return s.order, nil
}
func (s *squarespacePGSource) ListAllTransactionsForOrder(context.Context, string, int) ([]provider.SquarespaceTransactionDocument, error) {
	return s.documents, nil
}
func (f *squarespacePGFixtureData) bind(t *testing.T, ctx context.Context) *dbent.PaymentExternalOrder {
	t.Helper()
	ledger, err := f.bridge.observeOrder(ctx, "site_1", f.remote)
	require.NoError(t, err)
	ledger, err = f.bridge.bindExternalOrder(ctx, ledger, f.local, f.remote, f.proof, "reference")
	require.NoError(t, err)
	return ledger
}
func squarespacePGRefundProof(proof *verifiedSquarespaceProof, cumulative int64) *verifiedSquarespaceProof {
	copyProof := *proof
	copyProof.RefundedMinor = cumulative
	copyProof.Payments = append([]verifiedSquarespacePaymentProof(nil), proof.Payments...)
	copyProof.Payments[0].RefundedMinor = cumulative
	return &copyProof
}
