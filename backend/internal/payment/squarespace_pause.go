package payment

import (
	"os"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SquarespacePaymentsPaused is an operator-controlled, process-level stop gate.
// Unset/empty preserves compatibility; invalid nonempty values fail closed.
// Production changes require recreating the application with the reviewed env.
func SquarespacePaymentsPaused() bool {
	value := strings.TrimSpace(os.Getenv("SQUARESPACE_PAYMENTS_PAUSED"))
	if value == "" {
		return false
	}
	paused, err := strconv.ParseBool(value)
	return err != nil || paused
}

var ErrSquarespacePaymentsPaused = infraerrors.ServiceUnavailable("SQUARESPACE_PAYMENTS_PAUSED", "Squarespace payments are paused")

func RequireSquarespacePaymentsActive() error {
	if SquarespacePaymentsPaused() {
		return ErrSquarespacePaymentsPaused
	}
	return nil
}
