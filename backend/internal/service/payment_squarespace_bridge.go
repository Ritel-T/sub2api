package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalpayment"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/paymentsyncstate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/google/uuid"
)

const squarespaceSyncInterval = 45 * time.Second
const squarespaceSyncTimeout = 40 * time.Second
const squarespaceSyncLease = 90 * time.Second
const squarespaceSyncOverlap = 5 * time.Minute
const squarespaceMaxPagesPerCycle = 3

type squarespaceBridgeSource interface {
	VerifyWebsite(context.Context) error
	ListOrders(context.Context, string, string, string) (*provider.SquarespaceOrderPage, error)
	GetOrder(context.Context, string) (*provider.SquarespaceOrder, error)
	ListAllTransactionsForOrder(context.Context, string, int) ([]provider.SquarespaceTransactionDocument, error)
}

type squarespaceBalanceCacheInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}

// SquarespacePaymentBridge reconciles a website once per cycle, rather than
// issuing an upstream query for every pending local order or every app instance.
type SquarespacePaymentBridge struct {
	payment         *PaymentService
	config          *PaymentConfigService
	client          *dbent.Client
	lockCache       LeaderLockCache
	db              *sql.DB
	owner           string
	now             func() time.Time
	sourceFactory   func(context.Context, map[string]string) (squarespaceBridgeSource, error)
	balanceCache    squarespaceBalanceCacheInvalidator
	authCache       APIKeyAuthCacheInvalidator
	stop            chan struct{}
	stopOnce        sync.Once
	startOnce       sync.Once
	wg              sync.WaitGroup
	activeRunMu     sync.Mutex
	activeRunCancel context.CancelFunc
}

func NewSquarespacePaymentBridge(paymentSvc *PaymentService, lockCache LeaderLockCache, db *sql.DB) *SquarespacePaymentBridge {
	bridge := &SquarespacePaymentBridge{payment: paymentSvc, lockCache: lockCache, db: db, owner: uuid.NewString(), now: time.Now, stop: make(chan struct{})}
	if paymentSvc != nil {
		bridge.config = paymentSvc.configService
		bridge.client = paymentSvc.entClient
	}
	bridge.sourceFactory = func(ctx context.Context, cfg map[string]string) (squarespaceBridgeSource, error) {
		if bridge.config == nil {
			return nil, errors.New("payment configuration unavailable")
		}
		tokens, err := bridge.config.squarespaceTokenSource(ctx, cfg["websiteId"])
		if err != nil {
			return nil, err
		}
		return provider.NewSquarespaceClient(cfg["websiteId"], cfg["referenceFieldLabel"], tokens)
	}
	return bridge
}

func (s *SquarespacePaymentBridge) SetRefundCacheInvalidators(balance squarespaceBalanceCacheInvalidator, auth APIKeyAuthCacheInvalidator) {
	s.balanceCache = balance
	s.authCache = auth
}
func (s *SquarespacePaymentBridge) Start() {
	if s == nil || s.payment == nil || s.client == nil || s.config == nil {
		return
	}
	s.startOnce.Do(func() {
		s.activeRunMu.Lock()
		select {
		case <-s.stop:
			s.activeRunMu.Unlock()
			return
		default:
		}
		s.wg.Add(1)
		s.activeRunMu.Unlock()
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(squarespaceSyncInterval)
			defer ticker.Stop()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), squarespaceSyncTimeout)
				s.activeRunMu.Lock()
				select {
				case <-s.stop:
					s.activeRunMu.Unlock()
					cancel()
					return
				default:
				}
				s.activeRunCancel = cancel
				s.activeRunMu.Unlock()
				err := s.RunOnce(ctx)
				cancel()
				s.activeRunMu.Lock()
				s.activeRunCancel = nil
				s.activeRunMu.Unlock()
				if err != nil {
					slog.Warn("Squarespace payment synchronization deferred", "code", "SYNC_ERROR")
				}
				select {
				case <-ticker.C:
				case <-s.stop:
					return
				}
			}
		}()
	})
}
func (s *SquarespacePaymentBridge) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stop != nil {
			close(s.stop)
		}
	})
	s.activeRunMu.Lock()
	if s.activeRunCancel != nil {
		s.activeRunCancel()
	}
	s.activeRunMu.Unlock()
	s.wg.Wait()
}

func (s *SquarespacePaymentBridge) RunOnce(ctx context.Context) error {
	if s == nil || s.client == nil || s.config == nil {
		return nil
	}
	instances, err := s.client.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ("squarespace")).Order(dbent.Desc(paymentproviderinstance.FieldEnabled), dbent.Asc(paymentproviderinstance.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	visited := make(map[string]bool)
	var firstErr error
	for _, inst := range instances {
		cfg, err := s.config.decryptConfig(inst.Config)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		website := strings.TrimSpace(cfg["websiteId"])
		if website == "" || visited[website] {
			continue
		}
		visited[website] = true
		if !inst.Enabled {
			hasBound, checkErr := s.client.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(website), paymentexternalorder.LocalOrderIDNotNil()).Exist(ctx)
			if checkErr != nil {
				return checkErr
			}
			if !hasBound {
				continue
			}
		}
		if err = s.syncWebsite(ctx, inst, cfg); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *SquarespacePaymentBridge) syncWebsite(ctx context.Context, instance *dbent.PaymentProviderInstance, cfg map[string]string) error {
	website := cfg["websiteId"]
	state, err := s.config.squarespaceSyncState(ctx, website)
	if err != nil {
		return err
	}
	if state.RetryAt != nil && state.RetryAt.After(s.now()) {
		return nil
	}
	// The durable CAS lease remains authoritative when Redis is absent or failing.
	claimed, err := s.client.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(state.ID), paymentsyncstate.Or(paymentsyncstate.LeaseOwnerEQ(""), paymentsyncstate.LeaseUntilIsNil(), paymentsyncstate.LeaseUntilLT(s.now()))).SetLeaseOwner(s.owner).SetLeaseUntil(s.now().Add(squarespaceSyncLease)).Save(ctx)
	if err != nil || claimed != 1 {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.client.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(state.ID), paymentsyncstate.LeaseOwnerEQ(s.owner)).SetLeaseOwner("").ClearLeaseUntil().Save(releaseCtx)
	}()
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, "payment:squarespace:sync:"+website, s.owner, squarespaceSyncLease)
	if !ok {
		return nil
	}
	defer release()
	source, err := s.sourceFactory(ctx, cfg)
	if err != nil {
		return s.deferSync(ctx, state.ID, err)
	}
	if err = source.VerifyWebsite(ctx); err != nil {
		return s.deferSync(ctx, state.ID, err)
	}
	if state.WindowStart == nil || state.WindowEnd == nil {
		start := time.Unix(0, 0).UTC()
		if state.LastSyncedAt != nil {
			start = state.LastSyncedAt.Add(-squarespaceSyncOverlap)
		}
		end := s.now().UTC()
		state, err = s.client.PaymentSyncState.UpdateOneID(state.ID).SetWindowStart(start).SetWindowEnd(end).SetNextCursor("").Save(ctx)
		if err != nil {
			return err
		}
	}
	cursor := state.NextCursor
	seen := make(map[string]bool)
	for pages := 0; pages < squarespaceMaxPagesPerCycle; pages++ {
		after, before := "", ""
		if cursor == "" {
			after = state.WindowStart.Format(time.RFC3339Nano)
			before = state.WindowEnd.Format(time.RFC3339Nano)
		}
		page, fetchErr := source.ListOrders(ctx, cursor, after, before)
		if fetchErr != nil {
			return s.deferSync(ctx, state.ID, fetchErr)
		}
		if page == nil {
			return s.deferSync(ctx, state.ID, errors.New("missing Squarespace order page"))
		}
		for i := range page.Orders {
			remote := &page.Orders[i]
			settled, settledErr := s.settledUnchangedOrder(ctx, website, remote)
			if settledErr != nil {
				return settledErr
			}
			if settled {
				continue
			}
			ledger, observeErr := s.observeOrder(ctx, website, remote)
			if observeErr != nil {
				return observeErr
			}
			if ledger.LocalOrderID == nil && remote.ReferenceStatus != "valid" {
				if err = s.anomaly(ctx, ledger.ID, "CHECKOUT_REFERENCE_"+strings.ToUpper(remote.ReferenceStatus)); err != nil {
					return err
				}
				continue
			}
			docs, fetchErr := source.ListAllTransactionsForOrder(ctx, remote.ID, 4)
			if fetchErr != nil {
				return s.deferSync(ctx, state.ID, fetchErr)
			}
			if err = s.processExternalOrder(ctx, instance, website, remote, docs, ledger); err != nil {
				return err
			}
		}
		if !page.Pagination.HasNextPage {
			_, err = s.client.PaymentSyncState.UpdateOneID(state.ID).SetLastSyncedAt(*state.WindowEnd).ClearWindowStart().ClearWindowEnd().SetNextCursor("").ClearRetryAt().SetLastErrorCode("").Save(ctx)
			if err != nil {
				return err
			}
			break
		}
		next := page.Pagination.NextPageCursor
		if next == "" || next == cursor || seen[next] {
			return s.deferSync(ctx, state.ID, errors.New("invalid Squarespace pagination"))
		}
		seen[next] = true
		cursor = next
		if _, err = s.client.PaymentSyncState.UpdateOneID(state.ID).SetNextCursor(cursor).Save(ctx); err != nil {
			return err
		}
	}
	// Recover a crash between binding, redeem and the completed marker, and keep
	// refund/cache exceptions observable even if provider data is now unchanged.
	return s.recoverBoundOrders(ctx, instance, website, source)
}

func (s *SquarespacePaymentBridge) deferSync(ctx context.Context, stateID int64, cause error) error {
	if ctx.Err() != nil {
		saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ctx = saveCtx
	}
	delay := squarespaceSyncInterval
	code := "SYNC_SOURCE_ERROR"
	var apiErr *provider.SquarespaceAPIError
	if errors.As(cause, &apiErr) && apiErr.HTTPStatus == 429 {
		code = "UPSTREAM_RATE_LIMIT"
		delay = apiErr.RetryAfter
		if delay < 30*time.Second {
			delay = 30 * time.Second
		}
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
	}
	if errors.Is(cause, ErrSquarespaceReauthorizationRequired) {
		code = "OAUTH_REAUTHORIZATION_REQUIRED"
	}
	if errors.Is(cause, ErrSquarespaceTokenRotationBusy) {
		code = "OAUTH_ROTATION_BUSY"
	}
	_, saveErr := s.client.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(stateID), paymentsyncstate.RotationPhaseEQ("idle")).SetRetryAt(s.now().Add(delay)).SetLastErrorCode(code).Save(ctx)
	if saveErr == nil {
		_, saveErr = s.client.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(stateID), paymentsyncstate.RotationPhaseNEQ("idle")).SetRetryAt(s.now().Add(delay)).Save(ctx)
	}
	if saveErr != nil {
		return saveErr
	}
	return errors.New(code)
}

// The five-minute overlap intentionally replays modified orders. Reuse a fully
// settled unchanged proof instead of re-fetching transactions for every replay.
func (s *SquarespacePaymentBridge) settledUnchangedOrder(ctx context.Context, website string, remote *provider.SquarespaceOrder) (bool, error) {
	if remote == nil {
		return false, nil
	}
	modified, parseErr := time.Parse(time.RFC3339Nano, remote.ModifiedOn)
	if parseErr != nil {
		return false, nil
	}
	ledger, err := s.client.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(website), paymentexternalorder.ExternalOrderIDEQ(remote.ID)).Only(ctx)
	if dbent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ledger.Status == "CREDITED" && ledger.AnomalyCode == "" && ledger.ProviderModifiedAt != nil && ledger.ProviderModifiedAt.Equal(modified), nil
}

func (s *SquarespacePaymentBridge) observeOrder(ctx context.Context, website string, remote *provider.SquarespaceOrder) (*dbent.PaymentExternalOrder, error) {
	if remote == nil || remote.ID == "" || len(remote.ID) > 128 {
		return nil, errors.New("invalid external order identifier")
	}
	row, err := s.client.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(website), paymentexternalorder.ExternalOrderIDEQ(remote.ID)).Only(ctx)
	if dbent.IsNotFound(err) {
		row, err = s.client.PaymentExternalOrder.Create().SetProviderKey("squarespace").SetWebsiteID(website).SetExternalOrderID(remote.ID).Save(ctx)
		if dbent.IsConstraintError(err) {
			row, err = s.client.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(website), paymentexternalorder.ExternalOrderIDEQ(remote.ID)).Only(ctx)
		}
	}
	if err != nil {
		return nil, err
	}
	state := "UNKNOWN"
	switch remote.PaymentState {
	case "PAID", "NOT_CHARGED", "AUTHORIZED", "PENDING", "FAILED", "REFUNDED", "REFUND_PENDING", "REFUND_FAILED", "PARTIALLY_PAID":
		state = remote.PaymentState
	}
	update := s.client.PaymentExternalOrder.UpdateOneID(row.ID).SetPaymentState(state).SetLastSeenAt(s.now())
	if receipt, receiptErr := normalizeSquarespaceReceiptNumber(remote.OrderNumber); receiptErr == nil {
		update.SetOrderNumber(receipt)
	}
	if modified, parseErr := time.Parse(time.RFC3339Nano, remote.ModifiedOn); parseErr == nil {
		update.SetProviderModifiedAt(modified)
	}
	return update.Save(ctx)
}

func (s *SquarespacePaymentBridge) anomaly(ctx context.Context, ledgerID int64, code string) error {
	if len(code) > 64 {
		code = "INVALID_PAYMENT_PROOF"
	}
	return s.client.PaymentExternalOrder.UpdateOneID(ledgerID).SetAnomalyCode(code).SetStatus("EXCEPTION").Exec(ctx)
}

func (s *SquarespacePaymentBridge) processExternalOrder(ctx context.Context, instance *dbent.PaymentProviderInstance, website string, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument, ledger *dbent.PaymentExternalOrder) error {
	proof, code := verifySquarespaceProof(remote, docs)
	if code != "" {
		return s.anomaly(ctx, ledger.ID, code)
	}
	if proof.PaidAt.After(s.now().Add(time.Minute)) {
		return s.anomaly(ctx, ledger.ID, "PAYMENT_TIME_IN_FUTURE")
	}
	if ledger.LocalOrderID == nil {
		if !instance.Enabled {
			return s.anomaly(ctx, ledger.ID, "AUTOMATIC_CREDIT_DISABLED")
		}
		if proof.RefundedMinor != 0 || remote.PaymentState != "PAID" {
			return s.anomaly(ctx, ledger.ID, "REFUND_BEFORE_BINDING")
		}
		local, err := s.client.PaymentOrder.Query().Where(paymentorder.OutTradeNoEQ(remote.TopUpReference)).Only(ctx)
		if dbent.IsNotFound(err) {
			return s.anomaly(ctx, ledger.ID, "LOCAL_ORDER_NOT_FOUND")
		}
		if err != nil {
			return err
		}
		if PaymentOrderClaimMode(local) != "reference" {
			return s.anomaly(ctx, ledger.ID, "PAYER_OTP_REQUIRED")
		}
		if err := s.payment.validateSquarespaceOrderAccess(ctx, local.UserID, local); err != nil {
			return s.anomaly(ctx, ledger.ID, "AUTOMATIC_CREDIT_DISABLED")
		}
		if code = validateSquarespaceQuoteProof(local, remote, proof, website); code != "" {
			return s.anomaly(ctx, ledger.ID, code)
		}
		ledger, err = s.bindExternalOrder(ctx, ledger, local, remote, proof, "reference")
		if err != nil {
			if dbent.IsConstraintError(err) {
				return s.anomaly(ctx, ledger.ID, "EXTERNAL_PAYMENT_ALREADY_BOUND")
			}
			return err
		}
	}
	currentLocal, loadErr := s.client.PaymentOrder.Get(ctx, *ledger.LocalOrderID)
	if loadErr != nil {
		return loadErr
	}
	grant, scopeErr := s.payment.squarespaceReceiptScope(ctx, currentLocal, remote, ledger)
	if scopeErr != nil {
		code := "ORDER_SCOPE_UNTRUSTED"
		if ledger.BindingMethod != squarespaceLegacyScopeBinding {
			if fixedCode := validateSquarespaceFrozenTopupProduct(currentLocal, remote); fixedCode != "" {
				code = fixedCode
			}
		}
		return s.anomaly(ctx, ledger.ID, code)
	}
	currentHash, hashErr := SquarespaceReceiptQuoteHash(currentLocal)
	if hashErr != nil || currentHash != ledger.QuoteHash {
		return s.anomaly(ctx, ledger.ID, "BOUND_QUOTE_CHANGED")
	}
	if ledger.TotalMinor != proof.TotalMinor || ledger.Currency != proof.Currency || ledger.PaidAt == nil || !ledger.PaidAt.Equal(proof.PaidAt) {
		return s.anomaly(ctx, ledger.ID, "BOUND_PAYMENT_PROOF_CHANGED")
	}
	fixedPayments, err := s.client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID)).All(ctx)
	if err != nil {
		return err
	}
	if len(fixedPayments) != len(proof.Payments) {
		return s.anomaly(ctx, ledger.ID, "BOUND_PAYMENT_ID_CHANGED")
	}
	fixedByID := make(map[string]*dbent.PaymentExternalPayment, len(fixedPayments))
	for _, paid := range fixedPayments {
		fixedByID[paid.PaymentID] = paid
	}
	for _, paid := range proof.Payments {
		fixed := fixedByID[paid.ID]
		if fixed == nil || fixed.AmountMinor != paid.AmountMinor || !fixed.PaidAt.Equal(paid.PaidAt) {
			return s.anomaly(ctx, ledger.ID, "BOUND_PAYMENT_PROOF_CHANGED")
		}
	}

	if proof.RefundedMinor > 0 || remote.PaymentState == "REFUND_PENDING" || remote.PaymentState == "REFUND_FAILED" {
		return s.applyObservedRefund(ctx, ledger, proof, remote.PaymentState)
	}
	if ledger.RefundedMinor > 0 {
		return s.anomaly(ctx, ledger.ID, "REFUND_TOTAL_REGRESSED")
	}
	local, err := s.client.PaymentOrder.Get(ctx, *ledger.LocalOrderID)
	if err != nil {
		return err
	}
	if psIsRefundStatus(local.Status) {
		return s.anomaly(ctx, ledger.ID, "LOCAL_REFUND_STATE")
	}
	if local.Status == OrderStatusCompleted {
		return s.client.PaymentExternalOrder.UpdateOneID(ledger.ID).SetStatus("CREDITED").SetAnomalyCode("").Exec(ctx)
	}
	if !instance.Enabled {
		return s.anomaly(ctx, ledger.ID, "AUTOMATIC_CREDIT_DISABLED")
	}
	if err := s.payment.validateSquarespaceOrderAccess(ctx, local.UserID, local); err != nil {
		return s.anomaly(ctx, ledger.ID, "AUTOMATIC_CREDIT_DISABLED")
	}
	if ledger.BindingMethod == "receipt_otp" || ledger.BindingMethod == squarespaceLegacyScopeBinding {
		code = validateSquarespaceQuoteFinancialProof(local, proof, website)
	} else {
		code = validateSquarespaceQuoteProof(local, remote, proof, website)
	}
	if code != "" {
		return s.anomaly(ctx, ledger.ID, code)
	}

	if grant.Config["orderScopeMode"] == provider.SquarespaceScopeDedicatedSiteService && !grant.AssistedLegacy {
		grant.ActualProductID = strings.ToLower(remote.LineItems[0].ProductID)
	}
	allPaidTimes := make([]string, 0, len(proof.Payments))
	for _, paid := range proof.Payments {
		allPaidTimes = append(allPaidTimes, paid.PaidAt.Format(time.RFC3339Nano))
	}
	paidTimesJSON, _ := json.Marshal(allPaidTimes)
	notification := &payment.PaymentNotification{OrderID: local.OutTradeNo, TradeNo: remote.ID, Amount: float64(proof.TotalMinor) / 100, Status: payment.NotificationStatusSuccess, Metadata: map[string]string{"website_id": website, "currency": "GBP", "checkout_reference": local.OutTradeNo, "squarespace_paid_on": proof.PaidAt.Format(time.RFC3339Nano), "upstream_paid_on": string(paidTimesJSON)}}
	for key, value := range squarespaceScopeMetadata(grant) {
		notification.Metadata[key] = value
	}
	if err = s.payment.HandlePaymentNotification(ctx, notification, "squarespace"); err != nil {
		_ = s.anomaly(ctx, ledger.ID, "FULFILLMENT_RETRY")
		return err
	}
	refreshed, err := s.client.PaymentOrder.Get(ctx, local.ID)
	if err != nil {
		return err
	}
	status := "FULFILLMENT_PENDING"
	if refreshed.Status == OrderStatusCompleted {
		status = "CREDITED"
	}
	return s.client.PaymentExternalOrder.UpdateOneID(ledger.ID).SetStatus(status).SetAnomalyCode("").Exec(ctx)
}

func (s *SquarespacePaymentBridge) bindExternalOrder(ctx context.Context, ledger *dbent.PaymentExternalOrder, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, proof *verifiedSquarespaceProof, bindingMethod string) (result *dbent.PaymentExternalOrder, err error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	query := tx.PaymentExternalOrder.Query().Where(paymentexternalorder.IDEQ(ledger.ID))
	localQuery := tx.PaymentOrder.Query().Where(paymentorder.IDEQ(local.ID))
	if s.client.Driver().Dialect() == dialect.Postgres {
		query.ForUpdate()
		localQuery.ForUpdate()
	}
	lockedLocal, err := localQuery.Only(ctx)
	if err != nil {
		return nil, err
	}
	current, err := query.Only(ctx)
	if err != nil {
		return nil, err
	}
	if current.LocalOrderID != nil {
		if *current.LocalOrderID != local.ID {
			return nil, errors.New("external order already bound")
		}
		_ = tx.Rollback()
		return current, nil
	}
	if lockedLocal.Status != OrderStatusPending && lockedLocal.Status != OrderStatusExpired {
		return nil, errors.New("local order cannot accept a new payment binding")
	}
	if bindingMethod == "reference" && PaymentOrderClaimMode(lockedLocal) != "reference" {
		return nil, errors.New("receipt payment requires payer OTP authority")
	}
	if bindingMethod != "reference" && bindingMethod != "receipt_otp" && bindingMethod != squarespaceLegacyScopeBinding {
		return nil, errors.New("unsupported payment binding method")
	}
	if bindingMethod == "receipt_otp" || bindingMethod == squarespaceLegacyScopeBinding {
		if err := validateSquarespaceReceiptAuthority(ctx, lockedLocal, remote); err != nil {
			return nil, err
		}
	}

	transactionalCfg := *s.config
	transactionalCfg.entClient = tx.Client()
	// Scope verification only needs these transaction-bound readers. Do not
	// copy the shared PaymentService, which owns the provider registry mutex.
	transactionalPayment := PaymentService{
		entClient: tx.Client(), configService: &transactionalCfg,
	}
	grant, scopeErr := transactionalPayment.squarespaceReceiptScope(ctx, lockedLocal, remote, nil)
	if scopeErr != nil {
		return nil, scopeErr
	}
	if grant.AssistedLegacy {
		if bindingMethod != squarespaceLegacyScopeBinding {
			return nil, errors.New("reviewed legacy scope requires explicit receipt binding")
		}
	} else if bindingMethod == squarespaceLegacyScopeBinding {
		return nil, errors.New("unexpected legacy scope marker")
	}
	code := ""
	if bindingMethod == "receipt_otp" || bindingMethod == squarespaceLegacyScopeBinding {
		code = validateSquarespaceQuoteFinancialProof(lockedLocal, proof, ledger.WebsiteID)
	} else {
		code = validateSquarespaceQuoteProof(lockedLocal, remote, proof, ledger.WebsiteID)
	}
	if code != "" {
		return nil, fmt.Errorf("quote changed during binding: %s", code)
	}
	expectedHash, hashErr := SquarespaceReceiptQuoteHash(local)
	if hashErr != nil {
		return nil, hashErr
	}
	quoteHash, err := SquarespaceReceiptQuoteHash(lockedLocal)
	if err != nil {
		return nil, err
	}
	if quoteHash != expectedHash {
		return nil, errors.New("quote changed during external payment binding")
	}
	credited, err := squarespaceFloatUnits(lockedLocal.Amount, squarespaceUSDScale)
	if err != nil {
		return nil, err
	}
	result, err = tx.PaymentExternalOrder.UpdateOneID(ledger.ID).SetLocalOrderID(local.ID).SetBindingMethod(bindingMethod).SetQuoteHash(quoteHash).SetCheckoutReference(local.OutTradeNo).SetCurrency("GBP").SetTotalMinor(proof.TotalMinor).SetCreditedUsdUnits(credited).SetPaidAt(proof.PaidAt).SetStatus("BOUND").SetAnomalyCode("").Save(ctx)
	if err != nil {
		return nil, err
	}
	if grant.AssistedLegacy {
		if err = transactionalPayment.storeSquarespaceLegacyScopeAudit(ctx, tx.Client(), lockedLocal, remote, grant, quoteHash); err != nil {
			return nil, err
		}
	}
	for _, paid := range proof.Payments {
		_, err = tx.PaymentExternalPayment.Create().SetProviderKey("squarespace").SetWebsiteID(ledger.WebsiteID).SetPaymentID(paid.ID).SetExternalOrderLedgerID(ledger.ID).SetCurrency("GBP").SetAmountMinor(paid.AmountMinor).SetPaidAt(paid.PaidAt).Save(ctx)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SquarespacePaymentBridge) recoverBoundOrders(ctx context.Context, instance *dbent.PaymentProviderInstance, website string, source squarespaceBridgeSource) error {
	rows, err := s.client.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(website), paymentexternalorder.LocalOrderIDNotNil(), paymentexternalorder.Or(paymentexternalorder.StatusIn("BOUND", "FULFILLMENT_PENDING", "REFUND_WAITING"), paymentexternalorder.AnomalyCodeIn("FULFILLMENT_RETRY", "REFUND_FULFILLMENT_IN_FLIGHT", "CACHE_INVALIDATION_PENDING", "REFUND_OBSERVED_ON_RECHECK"))).Limit(20).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.AnomalyCode == "CACHE_INVALIDATION_PENDING" {
			if err = s.invalidateRefundCaches(ctx, row); err != nil {
				continue
			}
		}
		remote, fetchErr := source.GetOrder(ctx, row.ExternalOrderID)
		if fetchErr != nil {
			return fetchErr
		}
		docs, fetchErr := source.ListAllTransactionsForOrder(ctx, row.ExternalOrderID, 4)
		if fetchErr != nil {
			return fetchErr
		}
		if err = s.processExternalOrder(ctx, instance, website, remote, docs, row); err != nil {
			return err
		}
	}
	return nil
}

// Exported safe status DTOs deliberately omit ciphertext, tokens and credentials.
type SquarespaceOAuthStatus struct {
	Configured       bool       `json:"configured"`
	TokenVersion     int64      `json:"token_version"`
	RotationPhase    string     `json:"rotation_phase"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	AccessExpiresAt  *time.Time `json:"access_expires_at,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
}

func (s *PaymentConfigService) SquarespaceOAuthStatus(ctx context.Context, websiteID string) (*SquarespaceOAuthStatus, error) {
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return nil, err
	}
	result := &SquarespaceOAuthStatus{Configured: row.EncryptedOauthTokens != "" && row.EncryptedOauthCredentials != "", TokenVersion: row.TokenVersion, RotationPhase: row.RotationPhase, LastErrorCode: row.LastErrorCode, LastSyncedAt: row.LastSyncedAt}
	if pair, _, loadErr := s.LoadSquarespaceOAuthTokens(ctx, websiteID); loadErr == nil {
		if expires, parseErr := squarespaceOAuthExpiry(pair.AccessTokenExpiresAt); parseErr == nil {
			result.AccessExpiresAt = &expires
		}
		if expires, parseErr := squarespaceOAuthExpiry(pair.RefreshTokenExpiresAt); parseErr == nil {
			result.RefreshExpiresAt = &expires
		}
	}
	return result, nil
}

// ListSquarespacePaymentExceptions is the operator queue of unassigned,
// rejected, cache-pending or debt-bearing observations. Rows contain identifiers
// and integer monetary proof only, never customer emails or checkout form data.
func (s *PaymentConfigService) ListSquarespacePaymentExceptions(ctx context.Context, websiteID string, limit int) ([]*dbent.PaymentExternalOrder, error) {
	if websiteID == "" {
		return nil, errors.New("website identifier required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.entClient.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(websiteID), paymentexternalorder.AnomalyCodeNEQ("")).Order(dbent.Desc(paymentexternalorder.FieldUpdatedAt)).Limit(limit).All(ctx)
}
