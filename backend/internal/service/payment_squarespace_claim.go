package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net/mail"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	squarespaceClaimPurpose      = "squarespace_receipt_claim_v1"
	squarespaceClaimPrefix       = "payment:squarespace:claim:v1:"
	squarespaceClaimTTL          = 10 * time.Minute
	squarespaceClaimCooldown     = time.Minute
	squarespaceClaimMaxAttempts  = 5
	squarespaceClaimRequestLimit = 10
	squarespaceClaimSubmitLimit  = 30
)

var (
	ErrSquarespaceClaimCacheMiss          = errors.New("Squarespace claim cache entry missing")
	ErrSquarespaceClaimUnavailable        = infraerrors.BadRequest("SQUARESPACE_CLAIM_UNAVAILABLE", "This payment cannot currently be claimed")
	ErrSquarespaceClaimCodeInvalid        = infraerrors.BadRequest("SQUARESPACE_CLAIM_CODE_INVALID", "Invalid or expired payment claim code")
	ErrSquarespaceClaimRateLimited        = infraerrors.TooManyRequests("SQUARESPACE_CLAIM_RATE_LIMITED", "Please wait before requesting or submitting another payment claim")
	ErrSquarespaceClaimServiceUnavailable = infraerrors.ServiceUnavailable("SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE", "Payment claim verification is temporarily unavailable")
)

// SquarespaceClaimCache keeps the service layer independent of Redis libraries.
// Get must distinguish a missing entry (ErrSquarespaceClaimCacheMiss, or an
// empty value with nil error) from a backend outage. Eval must run atomically.
type SquarespaceClaimCache interface {
	Eval(context.Context, string, []string, ...any) (any, error)
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Del(context.Context, string) error
}

type SquarespaceClaimChallengeResponse struct {
	ChallengeToken     string    `json:"challenge_token"`
	ExpiresAt          time.Time `json:"expires_at"`
	ResendAfterSeconds int       `json:"resend_after_seconds"`
}

// No raw mailbox, token or OTP is persisted. Their purpose-separated HMACs
// bind the one-time proof to the signed-in user and the immutable retail quote.
type squarespaceClaimChallenge struct {
	Purpose         string `json:"purpose"`
	UserID          int64  `json:"user_id"`
	LocalOrderID    int64  `json:"local_order_id"`
	ExternalOrderID string `json:"external_order_id"`
	WebsiteID       string `json:"website_id"`
	ReceiptNumber   string `json:"receipt_number"`
	QuoteHash       string `json:"quote_hash"`
	RecipientHash   string `json:"recipient_hash"` // Current Auth.Email fingerprint; never a delivery address.
	PayerEmailHash  string `json:"payer_email_hash"`
	CodeMAC         string `json:"code_mac"`
	IssuedAtMS      int64  `json:"issued_at_ms"`
	ExpiresAtMS     int64  `json:"expires_at_ms"`
	Attempts        int    `json:"attempts"`
	Ready           bool   `json:"ready"`
}

type SquarespaceClaimService struct {
	payments       *PaymentService
	cache          SquarespaceClaimCache
	now            func() time.Time
	signingKey     func() ([]byte, error)
	loadUser       func(context.Context, int64) (*User, error)
	loadOrder      func(context.Context, int64, int64) (*dbent.PaymentOrder, error)
	validateAccess func(context.Context, int64, *dbent.PaymentOrder) error
	lookupReceipt  func(context.Context, *dbent.PaymentOrder, string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error)
	validateProof  func(context.Context, *dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument) error
	ensureUnbound  func(context.Context, *dbent.PaymentOrder, *provider.SquarespaceOrder) error
	bind           func(context.Context, *dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument) error
	sendEmail      func(context.Context, string, string, string) error
	generateCode   func() (string, error)
}

func NewSquarespaceClaimService(payments *PaymentService, email *EmailService, cache SquarespaceClaimCache) *SquarespaceClaimService {
	s := &SquarespaceClaimService{payments: payments, cache: cache, now: time.Now}
	s.signingKey = func() ([]byte, error) {
		if payments == nil {
			return nil, ErrSquarespaceClaimServiceUnavailable
		}
		return payments.retailQuoteSigningKey()
	}
	s.loadUser = func(ctx context.Context, uid int64) (*User, error) {
		if payments == nil || payments.userRepo == nil {
			return nil, ErrSquarespaceClaimServiceUnavailable
		}
		return payments.userRepo.GetByID(ctx, uid)
	}
	s.loadOrder = func(ctx context.Context, localID, uid int64) (*dbent.PaymentOrder, error) {
		if payments == nil || payments.entClient == nil {
			return nil, ErrSquarespaceClaimServiceUnavailable
		}
		return payments.GetOrder(ctx, localID, uid)
	}
	s.validateAccess = func(ctx context.Context, uid int64, local *dbent.PaymentOrder) error {
		if payments == nil {
			return ErrSquarespaceClaimServiceUnavailable
		}
		return payments.validateSquarespaceOrderAccess(ctx, uid, local)
	}
	s.lookupReceipt = func(ctx context.Context, local *dbent.PaymentOrder, number string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error) {
		if payments == nil || payments.entClient == nil || payments.configService == nil {
			return nil, nil, ErrSquarespaceClaimServiceUnavailable
		}
		return payments.FindSquarespaceReceiptOrder(ctx, local, number)
	}
	s.validateProof = func(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) error {
		if payments == nil {
			return ErrSquarespaceClaimServiceUnavailable
		}
		return payments.validateSquarespaceReceiptClaim(ctx, local, remote, docs)
	}
	s.ensureUnbound = s.checkUnbound
	s.bind = func(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) error {
		if payments == nil {
			return ErrSquarespaceClaimServiceUnavailable
		}
		return payments.BindVerifiedReceiptClaim(ctx, local, remote, docs)
	}
	s.sendEmail = func(ctx context.Context, recipient, subject, body string) error {
		if email == nil {
			return ErrSquarespaceClaimServiceUnavailable
		}
		return email.SendEmail(ctx, recipient, subject, body)
	}
	s.generateCode = func() (string, error) {
		code := make([]byte, 6)
		for i := range code {
			digit, err := rand.Int(rand.Reader, big.NewInt(10))
			if err != nil {
				return "", err
			}
			code[i] = '0' + byte(digit.Int64())
		}
		return string(code), nil
	}
	return s
}

func (s *SquarespaceClaimService) RequestChallenge(ctx context.Context, uid, localID int64, receiptNumber, payerEmail string) (*SquarespaceClaimChallengeResponse, error) {
	if !s.ready(ctx) {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	if uid <= 0 || localID <= 0 {
		return nil, ErrSquarespaceClaimUnavailable
	}
	number, err := normalizeSquarespaceReceiptNumber(receiptNumber)
	if err != nil {
		return nil, ErrSquarespaceClaimUnavailable
	}
	if err := s.limit(ctx, "request:"+strconv.FormatInt(uid, 10), squarespaceClaimRequestLimit, time.Hour); err != nil {
		return nil, err
	}
	key, err := s.signingKey()
	if err != nil || len(key) < 16 {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	local, remote, _, recipient, authEmailHash, hash, err := s.candidate(ctx, uid, localID, number, key)
	if err != nil {
		return nil, err
	}
	// The caller supplies a receipt hint, never an arbitrary delivery address.
	// Only the fresh official customerEmail below may receive a claim code.
	payerHint, err := squarespaceClaimBareEmail(payerEmail)
	if err != nil || subtle.ConstantTimeCompare([]byte(payerHint), []byte(recipient)) != 1 {
		return nil, ErrSquarespaceClaimUnavailable
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	code, err := s.generateCode()
	if err != nil || !squarespaceClaimValidCode(code) {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	now := s.now().UTC()
	data := &squarespaceClaimChallenge{Purpose: squarespaceClaimPurpose, UserID: uid, LocalOrderID: localID, ExternalOrderID: remote.ID, WebsiteID: psOrderProviderSnapshot(local).MerchantID, ReceiptNumber: number, QuoteHash: hash, RecipientHash: authEmailHash, PayerEmailHash: squarespaceClaimMAC(key, "payer-recipient", recipient), IssuedAtMS: now.UnixMilli(), ExpiresAtMS: now.Add(squarespaceClaimTTL).UnixMilli()}
	challengeKey := squarespaceClaimTokenKey(key, token)
	data.CodeMAC = squarespaceClaimCodeMAC(key, challengeKey, data, code)
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	activeKey := squarespaceClaimActiveKey(uid, localID)
	keys := []string{challengeKey, squarespaceClaimPrefix + "cooldown:" + strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(localID, 10), squarespaceClaimPrefix + "receipt-cooldown:" + squarespaceClaimMAC(key, "receipt-cooldown", data.WebsiteID, number), activeKey}
	status, err := s.eval(ctx, squarespaceClaimIssueScript, keys, string(encoded), squarespaceClaimTTL.Milliseconds(), squarespaceClaimCooldown.Milliseconds())
	if err != nil {
		return nil, err
	}
	if status == 0 {
		return nil, ErrSquarespaceClaimRateLimited
	}
	if status != 1 {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	quote := PaymentOrderRetailQuote(local)
	body := fmt.Sprintf("<p>付款认领验证码 / Payment claim verification code: <strong>%s</strong></p><p>Squarespace receipt #%s · GBP %.2f · USD %.2f credit.</p><p>此验证码只用于将这笔付款充值到您的本站账户，10 分钟内有效。Only enter it in your signed-in payment page.</p>", html.EscapeString(code), html.EscapeString(number), quote.TotalAmountGBP, quote.CreditedAmountUSD)
	if err := s.sendEmail(ctx, recipient, "付款认领验证码 / Payment claim code", body); err != nil {
		s.discard(ctx, challengeKey, activeKey)
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	status, err = s.eval(ctx, squarespaceClaimActivateScript, []string{challengeKey, activeKey}, s.now().UTC().UnixMilli())
	if err != nil || status != 1 {
		s.discard(ctx, challengeKey, activeKey)
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	return &SquarespaceClaimChallengeResponse{ChallengeToken: token, ExpiresAt: time.UnixMilli(data.ExpiresAtMS).UTC(), ResendAfterSeconds: 60}, nil
}

func (s *SquarespaceClaimService) Claim(ctx context.Context, uid int64, token, code string) (*dbent.PaymentOrder, error) {
	if !s.ready(ctx) {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	if uid <= 0 {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	if err := s.limit(ctx, "submit:"+strconv.FormatInt(uid, 10), squarespaceClaimSubmitLimit, squarespaceClaimTTL); err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	key, err := s.signingKey()
	if err != nil || len(key) < 16 {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	challengeKey := squarespaceClaimTokenKey(key, token)
	cacheCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	value, err := s.cache.Get(cacheCtx, challengeKey)
	cancel()
	if errors.Is(err, ErrSquarespaceClaimCacheMiss) || err == nil && value == "" {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	if err != nil {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	var data squarespaceClaimChallenge
	if len(value) > 4096 || json.Unmarshal([]byte(value), &data) != nil {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	if data.UserID != uid || data.LocalOrderID <= 0 || data.Purpose != squarespaceClaimPurpose || !data.Ready || data.ExpiresAtMS <= s.now().UTC().UnixMilli() || data.ExpiresAtMS-data.IssuedAtMS != squarespaceClaimTTL.Milliseconds() {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	if len(code) > 32 {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	code = strings.TrimSpace(code)
	providedMAC := squarespaceClaimCodeMAC(key, challengeKey, &data, code)
	activeKey := squarespaceClaimActiveKey(uid, data.LocalOrderID)
	args := []any{uid, providedMAC, s.now().UTC().UnixMilli(), "check", squarespaceClaimMaxAttempts}
	status, err := s.eval(ctx, squarespaceClaimVerifyScript, []string{challengeKey, activeKey}, args...)
	if err != nil {
		return nil, err
	}
	if status == 0 {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	if status != 1 {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	local, remote, docs, recipient, authEmailHash, hash, err := s.candidate(ctx, uid, data.LocalOrderID, data.ReceiptNumber, key)
	if err != nil {
		return nil, err
	}
	if remote.ID != data.ExternalOrderID || psOrderProviderSnapshot(local).MerchantID != data.WebsiteID || subtle.ConstantTimeCompare([]byte(hash), []byte(data.QuoteHash)) != 1 || subtle.ConstantTimeCompare([]byte(authEmailHash), []byte(data.RecipientHash)) != 1 || subtle.ConstantTimeCompare([]byte(squarespaceClaimMAC(key, "payer-recipient", recipient)), []byte(data.PayerEmailHash)) != 1 {
		return nil, ErrSquarespaceClaimUnavailable
	}
	args[2], args[3] = s.now().UTC().UnixMilli(), "consume"
	status, err = s.eval(ctx, squarespaceClaimVerifyScript, []string{challengeKey, activeKey}, args...)
	if err != nil {
		return nil, err
	}
	if status == 0 {
		return nil, ErrSquarespaceClaimCodeInvalid
	}
	if status != 1 {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	// Authority is created only after fresh official proof and atomic OTP consume.
	// The binder's durable unique constraints remain authoritative across users,
	// different challenges and crashes. A failure requires a new email challenge.
	claimCtx := withVerifiedSquarespacePayerReceiptClaim(ctx, uid, local.ID, remote.ID, hash, data.PayerEmailHash, data.RecipientHash)
	if err := s.bind(claimCtx, local, remote, docs); err != nil {
		return nil, ErrSquarespaceClaimUnavailable
	}
	updated, err := s.loadOrder(ctx, local.ID, uid)
	if err != nil || updated == nil || updated.UserID != uid {
		return nil, ErrSquarespaceClaimServiceUnavailable
	}
	return updated, nil
}

func (s *SquarespaceClaimService) ready(ctx context.Context) bool {
	return s != nil && ctx != nil && ctx.Err() == nil && s.cache != nil && s.now != nil && s.signingKey != nil && s.loadUser != nil && s.loadOrder != nil && s.validateAccess != nil && s.lookupReceipt != nil && s.validateProof != nil && s.ensureUnbound != nil && s.bind != nil && s.sendEmail != nil && s.generateCode != nil
}

func (s *SquarespaceClaimService) candidate(ctx context.Context, uid, localID int64, number string, key []byte) (*dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, string, string, string, error) {
	fail := func(err error) (*dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, string, string, string, error) {
		return nil, nil, nil, "", "", "", err
	}
	user, err := s.loadUser(ctx, uid)
	if err != nil || user == nil || user.ID != uid || !user.IsActive() {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	authEmailHash := squarespaceClaimAuthEmailHash(key, user.Email)
	local, lookupQuoteHash, err := s.eligibleOrder(ctx, uid, localID)
	if err != nil {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	lookupWebsite := psOrderProviderSnapshot(local).MerchantID
	remote, docs, err := s.lookupReceipt(ctx, local, number)
	if err != nil || remote == nil {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	canonical, err := normalizeSquarespaceReceiptNumber(remote.OrderNumber)
	if err != nil || canonical != number {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	// Recheck the actual stored invoice and current channel policy after slow
	// official I/O. Never issue or consume an OTP from a canceled/repriced invoice
	// or use an old merchant's proof after its frozen website binding changes.
	currentOrder, hash, err := s.eligibleOrder(ctx, uid, localID)
	if err != nil || psOrderProviderSnapshot(currentOrder).MerchantID != lookupWebsite || subtle.ConstantTimeCompare([]byte(hash), []byte(lookupQuoteHash)) != 1 {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	local = currentOrder
	if err := s.validateProof(ctx, local, remote, docs); err != nil {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	recipient, err := squarespaceClaimBareEmail(remote.CustomerEmail)
	if err != nil {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	// Official lookup may paginate or wait on token rotation. Re-read the account
	// after it finishes so an email/status change during that wait cannot use the
	// earlier account snapshot as the authorization predicate.
	currentUser, err := s.loadUser(ctx, uid)
	if err != nil || currentUser == nil || currentUser.ID != uid || !currentUser.IsActive() {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	currentAuthEmailHash := squarespaceClaimAuthEmailHash(key, currentUser.Email)
	if subtle.ConstantTimeCompare([]byte(authEmailHash), []byte(currentAuthEmailHash)) != 1 {
		return fail(ErrSquarespaceClaimUnavailable)
	}
	if err := s.ensureUnbound(ctx, local, remote); err != nil {
		if errors.Is(err, ErrSquarespaceClaimUnavailable) {
			return fail(ErrSquarespaceClaimUnavailable)
		}
		return fail(ErrSquarespaceClaimServiceUnavailable)
	}
	return local, remote, docs, recipient, currentAuthEmailHash, hash, nil
}

func (s *SquarespaceClaimService) eligibleOrder(ctx context.Context, uid, localID int64) (*dbent.PaymentOrder, string, error) {
	local, err := s.loadOrder(ctx, localID, uid)
	if err != nil || local == nil || local.ID != localID || local.UserID != uid || local.OrderType != "balance" || (local.Status != OrderStatusPending && local.Status != OrderStatusExpired) {
		return nil, "", ErrSquarespaceClaimUnavailable
	}
	quote, snapshot := PaymentOrderRetailQuote(local), psOrderProviderSnapshot(local)
	if quote == nil || quote.PaymentClaimMode != "receipt_otp" || snapshot == nil || snapshot.ProviderKey != "squarespace" || snapshot.MerchantID == "" {
		return nil, "", ErrSquarespaceClaimUnavailable
	}
	if err := s.validateAccess(ctx, uid, local); err != nil {
		return nil, "", ErrSquarespaceClaimUnavailable
	}
	hash, err := SquarespaceReceiptQuoteHash(local)
	if err != nil {
		return nil, "", ErrSquarespaceClaimUnavailable
	}
	return local, hash, nil
}

func (s *SquarespaceClaimService) checkUnbound(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder) error {
	if s.payments == nil || s.payments.entClient == nil {
		return ErrSquarespaceClaimServiceUnavailable
	}
	snapshot := psOrderProviderSnapshot(local)
	if snapshot == nil {
		return ErrSquarespaceClaimUnavailable
	}
	exists, err := s.payments.entClient.PaymentExternalOrder.Query().Where(paymentexternalorder.Or(paymentexternalorder.LocalOrderIDEQ(local.ID), paymentexternalorder.And(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(snapshot.MerchantID), paymentexternalorder.ExternalOrderIDEQ(remote.ID), paymentexternalorder.LocalOrderIDNotNil()))).Exist(ctx)
	if err != nil {
		return ErrSquarespaceClaimServiceUnavailable
	}
	if exists {
		return ErrSquarespaceClaimUnavailable
	}
	return nil
}

// Auth.Email remains an account-state binding even for legacy placeholder
// mailboxes. It is not required to be deliverable and never selects an OTP target.
func squarespaceClaimAuthEmailHash(key []byte, raw string) string {
	return squarespaceClaimMAC(key, "recipient", strings.ToLower(strings.TrimSpace(raw)))
}

func squarespaceClaimBareEmail(raw string) (string, error) {
	if len(raw) > 255 || strings.ContainsAny(raw, "\r\n") {
		return "", ErrSquarespaceClaimUnavailable
	}
	normalized := strings.ToLower(strings.TrimSpace(raw))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Name != "" || address.Address != normalized || isReservedEmail(normalized) {
		return "", ErrSquarespaceClaimUnavailable
	}
	at := strings.LastIndexByte(normalized, '@')
	if at < 1 || strings.HasSuffix(normalized[at+1:], ".invalid") {
		return "", ErrSquarespaceClaimUnavailable
	}
	return normalized, nil
}

func squarespaceClaimValidCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := range code {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

func squarespaceClaimMAC(key []byte, purpose string, parts ...string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(squarespaceClaimPurpose + ":" + purpose + ":"))
	encoded, _ := json.Marshal(parts)
	_, _ = mac.Write(encoded)
	return hex.EncodeToString(mac.Sum(nil))
}

func squarespaceClaimTokenKey(key []byte, token string) string {
	return squarespaceClaimPrefix + "challenge:" + squarespaceClaimMAC(key, "token-index", token)
}
func squarespaceClaimActiveKey(uid, localID int64) string {
	return squarespaceClaimPrefix + "active:" + strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(localID, 10)
}
func squarespaceClaimCodeMAC(key []byte, challengeKey string, data *squarespaceClaimChallenge, code string) string {
	return squarespaceClaimMAC(key, "otp", challengeKey, data.Purpose, strconv.FormatInt(data.UserID, 10), strconv.FormatInt(data.LocalOrderID, 10), data.ExternalOrderID, data.WebsiteID, data.ReceiptNumber, data.QuoteHash, data.RecipientHash, data.PayerEmailHash, strconv.FormatInt(data.IssuedAtMS, 10), strconv.FormatInt(data.ExpiresAtMS, 10), code)
}

func (s *SquarespaceClaimService) limit(ctx context.Context, suffix string, maximum int, window time.Duration) error {
	count, err := s.eval(ctx, squarespaceClaimRateScript, []string{squarespaceClaimPrefix + "rate:" + suffix}, window.Milliseconds())
	if err != nil {
		return err
	}
	if count < 1 {
		return ErrSquarespaceClaimServiceUnavailable
	}
	if count > int64(maximum) {
		return ErrSquarespaceClaimRateLimited
	}
	return nil
}

func (s *SquarespaceClaimService) eval(ctx context.Context, script string, keys []string, args ...any) (int64, error) {
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := s.cache.Eval(bounded, script, keys, args...)
	if err != nil {
		return 0, ErrSquarespaceClaimServiceUnavailable
	}
	switch value := result.(type) {
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, ErrSquarespaceClaimServiceUnavailable
}

func (s *SquarespaceClaimService) discard(ctx context.Context, challengeKey, activeKey string) {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = s.cache.Eval(bounded, squarespaceClaimDiscardScript, []string{challengeKey, activeKey})
}

const squarespaceClaimRateScript = `
local n = redis.call('INCR', KEYS[1])
if n == 1 or redis.call('PTTL', KEYS[1]) == -1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`

const squarespaceClaimIssueScript = `
if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('EXISTS', KEYS[3]) == 1 then return 0 end
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
redis.call('SET', KEYS[2], '1', 'PX', ARGV[3])
redis.call('SET', KEYS[3], '1', 'PX', ARGV[3])
redis.call('SET', KEYS[4], KEYS[1], 'PX', ARGV[2])
return 1
`

const squarespaceClaimActivateScript = `
if redis.call('GET', KEYS[2]) ~= KEYS[1] then return 0 end
local value = redis.call('GET', KEYS[1])
if not value then return 0 end
local ok, data = pcall(cjson.decode, value)
if not ok or tonumber(data.expires_at_ms) <= tonumber(ARGV[1]) then return 0 end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then return 0 end
data.ready = true
redis.call('SET', KEYS[1], cjson.encode(data), 'PX', ttl)
return 1
`

const squarespaceClaimVerifyScript = `
if redis.call('GET', KEYS[2]) ~= KEYS[1] then return 0 end
local value = redis.call('GET', KEYS[1])
if not value then return 0 end
local ok, data = pcall(cjson.decode, value)
if not ok then return -2 end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 or not data.ready or data.purpose ~= 'squarespace_receipt_claim_v1' or tonumber(data.user_id) ~= tonumber(ARGV[1]) or tonumber(data.expires_at_ms) <= tonumber(ARGV[3]) then return 0 end
if tonumber(data.attempts) >= tonumber(ARGV[5]) then return 0 end
if data.code_mac ~= ARGV[2] then
  data.attempts = tonumber(data.attempts) + 1
  redis.call('SET', KEYS[1], cjson.encode(data), 'PX', ttl)
  return 0
end
if ARGV[4] == 'consume' then redis.call('DEL', KEYS[1]); redis.call('DEL', KEYS[2]) end
return 1
`

const squarespaceClaimDiscardScript = `
redis.call('DEL', KEYS[1])
if redis.call('GET', KEYS[2]) == KEYS[1] then redis.call('DEL', KEYS[2]) end
return 1
`
