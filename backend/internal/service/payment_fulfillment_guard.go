package service

import (
	"context"
	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

type paymentRedeemGuardKey struct{}
type paymentRedeemGuard struct {
	OrderID      int64
	LeaseVersion time.Time
}

// Guard payment redemption in the same transaction that consumes its code and
// credits balance. Refund reconciliation locks this same payment row.
func lockPaymentRedemptionGuard(ctx context.Context, tx *dbent.Tx, client *dbent.Client, userID int64, code string) error {
	guard, ok := ctx.Value(paymentRedeemGuardKey{}).(paymentRedeemGuard)
	if !ok {
		return nil
	}
	query := tx.PaymentOrder.Query().Where(paymentorder.IDEQ(guard.OrderID), paymentorder.UserIDEQ(userID), paymentorder.RechargeCodeEQ(code))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	order, err := query.Only(ctx)
	if err != nil {
		return infraerrors.Conflict("PAYMENT_REDEMPTION_CHANGED", "payment order cannot be locked for redemption")
	}
	if order.Status != OrderStatusRecharging || !order.UpdatedAt.Equal(guard.LeaseVersion) {
		return infraerrors.Conflict("PAYMENT_REDEMPTION_CHANGED", "payment order was changed or refunded before redemption")
	}
	return nil
}
