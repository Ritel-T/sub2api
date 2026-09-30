package service

import (
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ProvideSquarespacePaymentBridge is wired into the same lifecycle as other
// payment background services; the runtime gate and durable website lease keep
// disabled/unconfigured deployments inert and multiple app instances safe.
func ProvideSquarespacePaymentBridge(paymentSvc *PaymentService, lockCache LeaderLockCache, db *sql.DB, billingCache *BillingCacheService, authCache APIKeyAuthCacheInvalidator, cfg *config.Config) *SquarespacePaymentBridge {
	bridge := NewSquarespacePaymentBridge(paymentSvc, lockCache, db)
	bridge.SetRefundCacheInvalidators(billingCache, authCache)
	startBackgroundService(cfg, bridge)
	return bridge
}
